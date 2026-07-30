package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/dedup"
	diffpkg "github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/gates"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/lines"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/payload"
)

type consolidatedFile struct {
	InlineEligible     []findings.Finding `json:"inline_eligible"`
	SummaryOnly        []findings.Finding `json:"summary_only"`
	DroppedPriorReview []findings.Finding `json:"dropped_prior_review"`
	// DroppedByGate is the audit trail for the confidence/severity gate — the
	// specialist's rationale supplies the reasoning for each dismissal. Shown in
	// the terminal summary only; never posted to GitHub.
	DroppedByGate   []droppedGateEntry        `json:"dropped_by_gate"`
	SpecialistsUsed []string                  `json:"specialists_used"`
	TimedOutRoles   []string                  `json:"timed_out_roles"`
	MissingRoles    []string                  `json:"missing_roles"`
	UnreadableRoles []string                  `json:"unreadable_roles"`
	InvalidFindings []findings.InvalidFinding `json:"invalid_findings"`
	LastReviewDate  *string                   `json:"last_review_date"`
	// Deduped is the audit trail for the two dedup passes — the buckets above
	// only account for what the *gate*, the *prior-review* filter and the schema
	// validator dropped, so without this a finding folded by dedup appeared in no
	// bucket at all and the summary looked internally consistent while
	// under-counting. Shown in the terminal summary; never posted to GitHub.
	Deduped []dedupedEntry `json:"deduped"`
	// Accounting is the conservation check over everything above. Every loaded
	// finding must land in exactly one bucket (or in Deduped); when it doesn't,
	// Accounting.OK is false and the summary prints a MISMATCH line. This is what
	// makes the rendered summary's "complete account" claim verifiable instead of
	// assumed.
	Accounting accounting `json:"accounting"`
	// PostingFilter is the audit trail for the include/exclude subset; the
	// consolidated counts above reflect the PRE-filter classification while
	// payload.json reflects the POST-filter subset. omitempty keeps existing
	// goldens byte-identical when no filter is active.
	PostingFilter *postingFilter `json:"posting_filter,omitempty"`
}

type postingFilter struct {
	IncludeIDs []string `json:"include_ids,omitempty"`
	ExcludeIDs []string `json:"exclude_ids,omitempty"`
}

// droppedGateEntry is a slim projection of a gate-dropped finding — enough for
// the terminal "Reconciled (below gate)" block without the full finding noise.
type droppedGateEntry struct {
	ID         string            `json:"id"`
	Severity   findings.Severity `json:"severity"`
	Confidence int               `json:"confidence"`
	File       string            `json:"file"`
	Line       int               `json:"line"`
	Rationale  string            `json:"rationale"`
}

// dedupedEntry names a finding that a dedup pass folded into another, and which
// one it folded into. Reconstructed from the survivors' CrossRefs, so it needs
// no extra plumbing through the pipeline.
type dedupedEntry struct {
	ID         string            `json:"id"`
	Specialist string            `json:"specialist"`
	Severity   findings.Severity `json:"severity"`
	Confidence int               `json:"confidence"`
	File       string            `json:"file"`
	Line       int               `json:"line"`
	Rationale  string            `json:"rationale"`
	MergedInto string            `json:"merged_into"`
	MergedBy   string            `json:"merged_by"`
}

// accounting is the conservation ledger: Loaded must equal Accounted, and every
// loaded ID must appear exactly once across the buckets. Orphans lists any ID
// that vanished — a bug in the pipeline, surfaced rather than swallowed.
type accounting struct {
	OK        bool           `json:"ok"`
	Loaded    int            `json:"loaded"`
	Accounted int            `json:"accounted"`
	ByBucket  map[string]int `json:"by_bucket"`
	Orphans   []string       `json:"orphans"`
}

// collectDeduped walks every surviving finding's CrossRefs to reconstruct the
// list of findings the dedup passes folded away. Buckets must include every
// slice that can hold a survivor — a gate-dropped or prior-review-dropped
// finding carries CrossRefs just as an inline-eligible one does, and missing one
// would report its folded peers as orphans.
func collectDeduped(buckets ...[]findings.Finding) []dedupedEntry {
	var out []dedupedEntry
	for _, bucket := range buckets {
		for _, survivor := range bucket {
			for _, ref := range survivor.CrossRefs {
				out = append(out, dedupedEntry{
					ID:         ref.ID,
					Specialist: ref.Specialist,
					Severity:   ref.Severity,
					Confidence: ref.Confidence,
					File:       ref.File,
					Line:       ref.Line,
					Rationale:  ref.Rationale,
					MergedInto: survivor.ID,
					MergedBy:   ref.MergedBy,
				})
			}
		}
	}
	slices.SortFunc(out, func(a, b dedupedEntry) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// reconcile checks that every loaded finding is accounted for exactly once.
// `loaded` is the post-validation set (invalid findings never reach the
// pipeline, so they are counted separately via their own bucket).
func reconcile(loaded []findings.Finding, invalid []findings.InvalidFinding, cf consolidatedFile) accounting {
	seen := make(map[string]struct{}, len(loaded))
	byBucket := map[string]int{
		"inline_eligible":      len(cf.InlineEligible),
		"summary_only":         len(cf.SummaryOnly),
		"dropped_prior_review": len(cf.DroppedPriorReview),
		"dropped_by_gate":      len(cf.DroppedByGate),
		"deduped":              len(cf.Deduped),
		"invalid_findings":     len(invalid),
	}
	for _, bucket := range [][]findings.Finding{cf.InlineEligible, cf.SummaryOnly, cf.DroppedPriorReview} {
		for _, f := range bucket {
			seen[f.ID] = struct{}{}
		}
	}
	for _, e := range cf.DroppedByGate {
		seen[e.ID] = struct{}{}
	}
	for _, e := range cf.Deduped {
		seen[e.ID] = struct{}{}
	}

	var orphans []string
	for _, f := range loaded {
		if _, ok := seen[f.ID]; !ok {
			orphans = append(orphans, f.ID)
		}
	}
	slices.Sort(orphans)

	accountedFindings := len(seen)
	return accounting{
		OK:        len(orphans) == 0 && accountedFindings == len(loaded),
		Loaded:    len(loaded) + len(invalid),
		Accounted: accountedFindings + len(invalid),
		ByBucket:  byBucket,
		Orphans:   coalesce(orphans),
	}
}

func toDroppedGateEntries(in []findings.Finding) []droppedGateEntry {
	out := make([]droppedGateEntry, 0, len(in))
	for _, f := range in {
		out = append(out, droppedGateEntry{
			ID:         f.ID,
			Severity:   f.Severity,
			Confidence: f.Confidence,
			File:       f.File,
			Line:       f.Line,
			Rationale:  f.Rationale,
		})
	}
	return out
}

type finalizeOpts struct {
	diffPath          string
	findingsDir       string
	priorPath         string
	headSHA           string
	owner             string
	repo              string
	prNumber          int
	outConsolidated   string
	outPayload        string
	outPendingPayload string
	outBody           string
	outFallback       string
	expectedRoles     string
	includeIDs        string
	excludeIDs        string
}

func parseFinalizeArgs(argv []string) (finalizeOpts, error) {
	var opts finalizeOpts
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	fs.StringVar(&opts.diffPath, "diff", "", "path to unified diff file")
	fs.StringVar(&opts.findingsDir, "findings-dir", "", "directory containing findings/<role>.json files")
	fs.StringVar(&opts.priorPath, "prior-issues", "", "path to prior-issues.json")
	fs.StringVar(&opts.headSHA, "head-sha", "", "full HEAD SHA")
	fs.StringVar(&opts.owner, "owner", "", "GitHub owner")
	fs.StringVar(&opts.repo, "repo", "", "GitHub repo")
	fs.IntVar(&opts.prNumber, "pr-number", 0, "pull request number")
	fs.StringVar(&opts.outConsolidated, "out-consolidated", "", "output path for consolidated.json")
	fs.StringVar(&opts.outPayload, "out-payload", "", "output path for payload.json")
	fs.StringVar(&opts.outPendingPayload, "out-pending-payload", "", "output path for payload-pending.json (no `event` field, used for two-step fallback)")
	fs.StringVar(&opts.outBody, "out-body", "", "output path for payload-body.json (just `{\"body\": ...}`, used for the submit step of the two-step fallback)")
	fs.StringVar(&opts.outFallback, "out-fallback", "", "output path for fallback.md")
	fs.StringVar(&opts.expectedRoles, "expected-roles", "", "comma-separated list of expected specialist roles (optional)")
	fs.StringVar(&opts.includeIDs, "include-finding-ids", "", "comma-separated specialist finding IDs to keep for posting (e.g. \"sec-1,err-2\"); the user's step-4 subset choice routes through this flag instead of editing payload.json by hand. Default empty = keep all classified findings. Unknown IDs are a hard error.")
	fs.StringVar(&opts.excludeIDs, "exclude-finding-ids", "", "comma-separated specialist finding IDs to drop from posting (e.g. \"qual-3\"); ignored if --include-finding-ids is also set. Unknown IDs are a hard error.")
	if err := fs.Parse(argv); err != nil {
		return finalizeOpts{}, err
	}
	return opts, nil
}

func (o finalizeOpts) validate() error {
	if o.diffPath == "" || o.findingsDir == "" || o.priorPath == "" || o.headSHA == "" ||
		o.owner == "" || o.repo == "" || o.outConsolidated == "" || o.outPayload == "" ||
		o.outPendingPayload == "" || o.outBody == "" || o.outFallback == "" {
		return fmt.Errorf("finalize: missing required flag (run with -h)")
	}
	return nil
}

func loadFinalizeInputs(opts finalizeOpts) (*diffpkg.Parsed, *findings.LoadResult, dedup.PriorIssuesFile, error) {
	df, err := os.Open(opts.diffPath)
	if err != nil {
		return nil, nil, dedup.PriorIssuesFile{}, fmt.Errorf("open diff: %w", err)
	}
	parsed, err := diffpkg.Parse(df)
	df.Close()
	if err != nil {
		return nil, nil, dedup.PriorIssuesFile{}, fmt.Errorf("parse diff: %w", err)
	}

	loaded, err := findings.LoadDir(opts.findingsDir, splitCSV(opts.expectedRoles))
	if err != nil {
		return nil, nil, dedup.PriorIssuesFile{}, fmt.Errorf("load findings: %w", err)
	}

	prior, err := loadPriorIssues(opts.priorPath)
	if err != nil {
		return nil, nil, dedup.PriorIssuesFile{}, fmt.Errorf("load prior issues: %w", err)
	}
	return parsed, loaded, prior, nil
}

// warnInvalid mirrors schema-rejected findings to stderr so they're visible in
// the bash output even when the lead agent skips reading consolidated.json.
func warnInvalid(invalid []findings.InvalidFinding) {
	for _, inv := range invalid {
		fmt.Fprintf(os.Stderr, "code-review-helper: warning: dropped %s finding %q: %s\n",
			inv.Role, inv.ID, inv.Reason)
	}
}

func runPipeline(parsed *diffpkg.Parsed, loaded *findings.LoadResult, prior dedup.PriorIssuesFile) (lines.Result, []findings.Finding, []findings.Finding) {
	step1 := dedup.Positional(loaded.Findings)
	inDiff := func(path string) bool { _, ok := parsed.ValidLines[path]; return ok }
	step2 := dedup.Semantic(step1, inDiff)
	step3, droppedPrior := dedup.PriorReview(step2, prior, parsed.IsAddedLine)
	step4, droppedGate := gates.Filter(step3)
	return lines.Classify(step4, parsed), droppedPrior, droppedGate
}

// buildPostingFilter runs AFTER classification so the IDs in --include/--exclude
// match what the user saw in consolidated.json. Unknown IDs are a hard error to
// surface typos at the failing call site rather than silently skipping.
func buildPostingFilter(includeIDs, excludeIDs string, classified lines.Result) (*postingFilter, error) {
	includeList := splitCSV(includeIDs)
	excludeList := splitCSV(excludeIDs)
	if len(includeList) == 0 && len(excludeList) == 0 {
		return nil, nil
	}
	if len(includeList) > 0 && len(excludeList) > 0 {
		fmt.Fprintln(os.Stderr, "code-review-helper: warning: both --include-finding-ids and --exclude-finding-ids supplied; --include-finding-ids wins, --exclude-finding-ids ignored")
		excludeList = nil
	}
	knownIDs := collectIDs(classified.InlineEligible, classified.SummaryOnly)
	if unknown := missingIDs(includeList, knownIDs); len(unknown) > 0 {
		return nil, fmt.Errorf("finalize: --include-finding-ids referenced unknown id(s) %v (available: %v)", unknown, knownIDs)
	}
	if len(excludeList) > 0 {
		if unknown := missingIDs(excludeList, knownIDs); len(unknown) > 0 {
			return nil, fmt.Errorf("finalize: --exclude-finding-ids referenced unknown id(s) %v (available: %v)", unknown, knownIDs)
		}
	}
	return &postingFilter{IncludeIDs: includeList, ExcludeIDs: excludeList}, nil
}

func assembleConsolidated(classified lines.Result, droppedPrior, droppedGate []findings.Finding, loaded *findings.LoadResult, prior dedup.PriorIssuesFile, pf *postingFilter) consolidatedFile {
	cf := consolidatedFile{
		InlineEligible:     coalesce(classified.InlineEligible),
		SummaryOnly:        coalesce(classified.SummaryOnly),
		DroppedPriorReview: coalesce(droppedPrior),
		DroppedByGate:      toDroppedGateEntries(droppedGate),
		SpecialistsUsed:    coalesce(loaded.Specialists),
		TimedOutRoles:      coalesce(loaded.TimedOutRoles),
		MissingRoles:       coalesce(loaded.MissingRoles),
		UnreadableRoles:    coalesce(loaded.UnreadableRoles),
		InvalidFindings:    coalesce(loaded.InvalidFindings),
		LastReviewDate:     prior.LastReviewDate,
		PostingFilter:      pf,
	}
	cf.Deduped = coalesce(collectDeduped(classified.InlineEligible, classified.SummaryOnly, droppedPrior, droppedGate))
	cf.Accounting = reconcile(loaded.Findings, loaded.InvalidFindings, cf)
	return cf
}

// warnAccounting mirrors a failed conservation check to stderr so it is visible
// in the bash output even before the summary jq runs. A mismatch means findings
// entered the pipeline and left no trace in any bucket — a helper bug, and the
// raw findings files are the only remaining copy.
func warnAccounting(a accounting) {
	if a.OK {
		return
	}
	fmt.Fprintf(os.Stderr,
		"code-review-helper: warning: accounting MISMATCH — %d finding(s) loaded, %d accounted for; orphans: %v\n",
		a.Loaded, a.Accounted, a.Orphans)
}

func assembleBuildInput(opts finalizeOpts, classified lines.Result, specialists []string, pf *postingFilter) payload.BuildInput {
	var include, exclude []string
	if pf != nil {
		include = pf.IncludeIDs
		exclude = pf.ExcludeIDs
	}
	return payload.BuildInput{
		HeadSHA:        opts.headSHA,
		Owner:          opts.owner,
		Repo:           opts.repo,
		InlineEligible: filterFindings(classified.InlineEligible, include, exclude),
		SummaryOnly:    filterFindings(classified.SummaryOnly, include, exclude),
		Specialists:    specialists,
	}
}

func writeFinalizeOutputs(opts finalizeOpts, cf consolidatedFile, buildIn payload.BuildInput) error {
	if err := writeJSON(opts.outConsolidated, cf); err != nil {
		return err
	}
	rev := payload.Build(buildIn)
	if err := writeJSON(opts.outPayload, rev); err != nil {
		return err
	}
	if err := writeJSON(opts.outPendingPayload, payload.BuildPending(buildIn)); err != nil {
		return err
	}
	if err := writeJSON(opts.outBody, payload.BodyOnly{Body: rev.Body}); err != nil {
		return err
	}
	fallback := payload.Fallback(buildIn, "")
	if err := os.WriteFile(opts.outFallback, []byte(fallback), 0o644); err != nil {
		return fmt.Errorf("write fallback: %w", err)
	}
	return nil
}

// runFinalize: dedup, gate, snap, render.
func runFinalize(argv []string) error {
	opts, err := parseFinalizeArgs(argv)
	if err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}

	parsed, loaded, prior, err := loadFinalizeInputs(opts)
	if err != nil {
		return err
	}
	warnInvalid(loaded.InvalidFindings)

	classified, droppedPrior, droppedGate := runPipeline(parsed, loaded, prior)
	pf, err := buildPostingFilter(opts.includeIDs, opts.excludeIDs, classified)
	if err != nil {
		return err
	}

	cf := assembleConsolidated(classified, droppedPrior, droppedGate, loaded, prior, pf)
	warnAccounting(cf.Accounting)
	buildIn := assembleBuildInput(opts, classified, loaded.Specialists, pf)
	return writeFinalizeOutputs(opts, cf, buildIn)
}

func loadPriorIssues(path string) (dedup.PriorIssuesFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return dedup.PriorIssuesFile{}, err
	}
	var p dedup.PriorIssuesFile
	if err := json.Unmarshal(b, &p); err != nil {
		return dedup.PriorIssuesFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empty
// entries so " sec-1, err-2 " doesn't survive into downstream validators.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// collectIDs returns sorted, deduplicated finding IDs across all buckets.
func collectIDs(buckets ...[]findings.Finding) []string {
	seen := map[string]struct{}{}
	for _, b := range buckets {
		for _, f := range b {
			if f.ID != "" {
				seen[f.ID] = struct{}{}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func missingIDs(want, known []string) []string {
	knownSet := map[string]struct{}{}
	for _, id := range known {
		knownSet[id] = struct{}{}
	}
	var missing []string
	for _, id := range want {
		if _, ok := knownSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// filterFindings honors include-wins-over-exclude precedence as a safety net
// in case a future caller forgets to clear exclude.
func filterFindings(in []findings.Finding, include, exclude []string) []findings.Finding {
	if len(include) == 0 && len(exclude) == 0 {
		return in
	}
	if len(include) > 0 {
		keep := map[string]struct{}{}
		for _, id := range include {
			keep[id] = struct{}{}
		}
		out := make([]findings.Finding, 0, len(in))
		for _, f := range in {
			if _, ok := keep[f.ID]; ok {
				out = append(out, f)
			}
		}
		return out
	}
	drop := map[string]struct{}{}
	for _, id := range exclude {
		drop[id] = struct{}{}
	}
	out := make([]findings.Finding, 0, len(in))
	for _, f := range in {
		if _, ok := drop[f.ID]; !ok {
			out = append(out, f)
		}
	}
	return out
}

// coalesce turns nil slices into empty slices so JSON output is `[]` not `null`.
func coalesce[T any](slice []T) []T {
	if slice == nil {
		return []T{}
	}
	return slice
}
