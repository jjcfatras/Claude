// code-review-helper is the deterministic backend for the /code-review plugin.
// It owns diff parsing, the dedup + gate + snap pipeline, and final payload
// assembly. The command invokes it via three subcommands; see the package
// documentation for each subcommand for the exact contract.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/bundle"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/dedup"
	diffpkg "github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/gates"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/lines"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/payload"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "diff":
		if err := runDiff(args); err != nil {
			fail(err)
		}
	case "finalize":
		if err := runFinalize(args); err != nil {
			fail(err)
		}
	case "bundle-context":
		if err := runBundleContext(args); err != nil {
			fail(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "code-review-helper: unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `code-review-helper — deterministic backend for the /code-review plugin

Usage:
  code-review-helper diff            [flags]
  code-review-helper finalize        [flags]
  code-review-helper bundle-context  [flags]

Run "code-review-helper <subcommand> -h" for subcommand flags.
`)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "code-review-helper: %v\n", err)
	os.Exit(1)
}

// runDiff: parse a unified diff and emit the changed-files list and valid-line map.
func runDiff(argv []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	in := fs.String("in", "-", "path to unified diff (or '-' for stdin)")
	outChanged := fs.String("out-changed-files", "", "path to write changed-files JSON array")
	outValid := fs.String("out-valid-lines", "", "path to write valid-lines map")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *outChanged == "" || *outValid == "" {
		return fmt.Errorf("diff: --out-changed-files and --out-valid-lines are required")
	}

	var r io.Reader
	if *in == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(*in)
		if err != nil {
			return fmt.Errorf("open --in: %w", err)
		}
		defer f.Close()
		r = f
	}

	parsed, err := diffpkg.Parse(r)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	if err := writeJSON(*outChanged, parsed.ChangedFiles); err != nil {
		return err
	}
	if err := writeJSON(*outValid, parsed.ValidLines); err != nil {
		return err
	}
	return nil
}

// consolidatedFile is what the skill reads at step 4 to display to the user
// before asking permission to post.
type consolidatedFile struct {
	InlineEligible     []findings.Finding        `json:"inline_eligible"`
	SummaryOnly        []findings.Finding        `json:"summary_only"`
	DroppedPriorReview []findings.Finding        `json:"dropped_prior_review"`
	SpecialistsUsed    []string                  `json:"specialists_used"`
	TimedOutRoles      []string                  `json:"timed_out_roles"`
	MissingRoles       []string                  `json:"missing_roles"`
	UnreadableRoles    []string                  `json:"unreadable_roles"`
	InvalidFindings    []findings.InvalidFinding `json:"invalid_findings"`
	LastReviewDate     *string                   `json:"last_review_date"`
	// PostingFilter records the --include-finding-ids / --exclude-finding-ids
	// arguments (if any) applied between consolidated emission and payload
	// assembly. The consolidated counts above reflect the PRE-filter
	// classification — they're the audit log of what the pipeline produced.
	// The payload files (payload.json, payload-body.json, etc.) reflect the
	// POST-filter subset. nil when no filter was applied (omitempty keeps
	// existing goldens byte-identical).
	PostingFilter *postingFilter `json:"posting_filter,omitempty"`
}

// postingFilter is the on-disk audit trail for a subset-post run.
type postingFilter struct {
	IncludeIDs []string `json:"include_ids,omitempty"`
	ExcludeIDs []string `json:"exclude_ids,omitempty"`
}

// finalizeOpts holds parsed `finalize` flag values. Storage lives on the struct
// so parseFinalizeArgs returns one populated value instead of runFinalize
// juggling fourteen pointer locals.
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

// warnInvalid surfaces schema-rejected findings to the operator. The lead agent
// reads consolidated.json at step 4 and is expected to mention these to the
// user before posting; the stderr line is so they're visible in the bash output
// even if the lead skips the consolidated.json check.
func warnInvalid(invalid []findings.InvalidFinding) {
	for _, inv := range invalid {
		fmt.Fprintf(os.Stderr, "code-review-helper: warning: dropped %s finding %q: %s\n",
			inv.Role, inv.ID, inv.Reason)
	}
}

func runPipeline(parsed *diffpkg.Parsed, loaded *findings.LoadResult, prior dedup.PriorIssuesFile) (lines.Result, []findings.Finding) {
	step1 := dedup.Positional(loaded.Findings)
	inDiff := func(path string) bool { _, ok := parsed.ValidLines[path]; return ok }
	step2 := dedup.Semantic(step1, inDiff)
	step3, dropped := dedup.PriorReview(step2, prior, parsed.IsAddedLine)
	step4 := gates.Filter(step3)
	return lines.Classify(step4, parsed), dropped
}

// buildPostingFilter parses the --include-finding-ids / --exclude-finding-ids
// arguments and validates them against the IDs the pipeline actually produced.
// Returns nil when no subset filter is active.
//
// Applied AFTER classification so IDs match what the user saw in
// consolidated.json at step 4. Unknown IDs are a hard error to surface user
// mistakes (typo, stale ID) at the failing call site rather than silently
// skipping.
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
	if unknown := missingIDs(excludeList, knownIDs); len(unknown) > 0 {
		return nil, fmt.Errorf("finalize: --exclude-finding-ids referenced unknown id(s) %v (available: %v)", unknown, knownIDs)
	}
	return &postingFilter{IncludeIDs: includeList, ExcludeIDs: excludeList}, nil
}

func assembleConsolidated(classified lines.Result, dropped []findings.Finding, loaded *findings.LoadResult, prior dedup.PriorIssuesFile, pf *postingFilter) consolidatedFile {
	return consolidatedFile{
		InlineEligible:     coalesce(classified.InlineEligible),
		SummaryOnly:        coalesce(classified.SummaryOnly),
		DroppedPriorReview: coalesce(dropped),
		SpecialistsUsed:    coalesce(loaded.Specialists),
		TimedOutRoles:      coalesce(loaded.TimedOutRoles),
		MissingRoles:       coalesce(loaded.MissingRoles),
		UnreadableRoles:    coalesce(loaded.UnreadableRoles),
		InvalidFindings:    coalesce(loaded.InvalidFindings),
		LastReviewDate:     prior.LastReviewDate,
		PostingFilter:      pf,
	}
}

// assembleBuildInput builds the payload BuildInput from POST-filter findings.
// When pf is nil, filterFindings returns its input unchanged.
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

// writeFinalizeOutputs writes all five finalize outputs: consolidated.json
// (pre-filter audit log), payload.json, payload-pending.json, payload-body.json,
// and fallback.md.
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

// runFinalize: dedup, gate, snap, render — produce consolidated.json,
// payload.json, fallback.md.
func runFinalize(argv []string) error {
	opts, err := parseFinalizeArgs(argv)
	if err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}
	_ = opts.prNumber // currently logged-only; reserved for future use

	parsed, loaded, prior, err := loadFinalizeInputs(opts)
	if err != nil {
		return err
	}
	warnInvalid(loaded.InvalidFindings)

	classified, dropped := runPipeline(parsed, loaded, prior)
	pf, err := buildPostingFilter(opts.includeIDs, opts.excludeIDs, classified)
	if err != nil {
		return err
	}

	cf := assembleConsolidated(classified, dropped, loaded, prior, pf)
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

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// splitCSV splits a comma-separated list and trims whitespace from each entry
// so callers can write "sec-1, err-2" without the space surviving into the
// validator (which would then reject " err-2" as unknown). Empty entries are
// dropped.
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

// collectIDs returns the union of finding IDs across the inline_eligible and
// summary_only buckets, deduplicated and lexicographically sorted. Used to
// validate --include-finding-ids / --exclude-finding-ids inputs.
func collectIDs(buckets ...[]findings.Finding) []string {
	seen := map[string]struct{}{}
	for _, b := range buckets {
		for _, f := range b {
			if f.ID != "" {
				seen[f.ID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// missingIDs returns the subset of `want` that does not appear in `known`.
// Used to surface unknown filter inputs as a hard error.
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

// filterFindings returns the subset of `in` that survives the include/exclude
// filter. When include is non-empty, exclude is ignored (caller enforces this
// in the warning, so the same precedence holds here for safety). When both are
// empty, returns `in` unchanged.
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

// coalesce turns nil slices into empty slices so consumers see `[]` in the
// JSON output rather than `null`. Cosmetic but makes downstream `jq` cleaner.
func coalesce[T any](slice []T) []T {
	if slice == nil {
		return []T{}
	}
	return slice
}

// runBundleContext: assemble $REVIEW_TMPDIR/spawn-context.md deterministically.
// See internal/bundle for the contract.
func runBundleContext(argv []string) error {
	fs := flag.NewFlagSet("bundle-context", flag.ContinueOnError)
	reviewTmpDir := fs.String("review-tmpdir", "", "path to $REVIEW_TMPDIR (must contain changed-files.json, roster.json, prior-issues.json, claude-md-files.json)")
	headSHA := fs.String("head-sha", "", "full HEAD SHA")
	prNumber := fs.Int("pr-number", 0, "pull request number")
	owner := fs.String("owner", "", "GitHub owner")
	repo := fs.String("repo", "", "GitHub repo")
	repoRoot := fs.String("repo-root", "", "repo working-tree root; emitted as REPO_ROOT in the bundle so specialists never synthesize paths from cwd (which may be a worktree not checked out to HEAD)")
	summaryPath := fs.String("summary-paragraph", "", "path to a file containing the prep agent's summary paragraph; pass '-' to read from stdin (avoids a Write that may trip third-party PreToolUse:Write hooks on sensitive-API substrings)")
	rubricPath := fs.String("rubric", "", "path to references/code-review-rubrics.md")
	rubricOut := fs.String("rubric-out", "", "when set, copy the rubric verbatim to this path and emit RUBRIC_PATH in the bundle header instead of inlining the rubric body — keeps spawn-context.md under the Read tool's 256 KB byte cap")
	maxSourceBytes := fs.Int("max-source-bytes", 32768, "embed each changed file <= this many bytes from HEAD; 0 disables source embedding")
	maxTotalSourceBytes := fs.Int("max-total-source-bytes", 200000, "aggregate cap across all embedded files; once running embedded byte count + next file size > cap, the next file and all remaining files are marked _omitted_ with the aggregate-cap reason. Default leaves headroom inside the Read tool's 256 KB byte cap. 0 disables (per-file cap still applies).")
	gitWorkdir := fs.String("git-workdir", "", "cwd for `git show` calls; defaults to current process cwd")
	out := fs.String("out", "", "output path for spawn-context.md (defaults to <review-tmpdir>/spawn-context.md; use '-' for stdout)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *reviewTmpDir == "" || *headSHA == "" || *prNumber == 0 || *owner == "" || *repo == "" || *rubricPath == "" {
		return fmt.Errorf("bundle-context: --review-tmpdir, --head-sha, --pr-number, --owner, --repo, --rubric are all required")
	}

	var summary string
	switch *summaryPath {
	case "":
		// no summary supplied; bundle renders a placeholder
	case "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read summary paragraph from stdin: %w", err)
		}
		summary = string(b)
	default:
		b, err := os.ReadFile(*summaryPath)
		if err != nil {
			return fmt.Errorf("read summary paragraph: %w", err)
		}
		summary = string(b)
	}

	in := bundle.Input{
		ReviewTmpDir:        *reviewTmpDir,
		HeadSHA:             *headSHA,
		PRNumber:            *prNumber,
		Owner:               *owner,
		Repo:                *repo,
		RepoRoot:            *repoRoot,
		SummaryParagraph:    summary,
		RubricPath:          *rubricPath,
		RubricExternal:      *rubricOut,
		MaxSourceBytes:      *maxSourceBytes,
		MaxTotalSourceBytes: *maxTotalSourceBytes,
		GitWorkdir:          *gitWorkdir,
	}
	bundleStr, err := bundle.Build(in)
	if err != nil {
		return err
	}

	outPath := *out
	if outPath == "" {
		outPath = *reviewTmpDir + "/spawn-context.md"
	}
	if outPath == "-" {
		_, err := io.WriteString(os.Stdout, bundleStr)
		return err
	}
	return os.WriteFile(outPath, []byte(bundleStr), 0o644)
}
