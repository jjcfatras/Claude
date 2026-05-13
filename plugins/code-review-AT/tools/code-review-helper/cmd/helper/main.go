// code-review-helper is the deterministic backend for the /code-review skill.
// It owns diff parsing, the dedup + gate + snap pipeline, and final payload
// assembly. The skill invokes it via two subcommands; see the package
// documentation for each subcommand for the exact contract.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/bundle"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/dedup"
	diffpkg "github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/gates"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/lines"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/payload"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/spawnbatch"
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
	case "spawn-batch":
		if err := runSpawnBatch(args); err != nil {
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
	fmt.Fprint(os.Stderr, `code-review-helper — deterministic backend for the /code-review skill

Usage:
  code-review-helper diff            [flags]
  code-review-helper finalize        [flags]
  code-review-helper bundle-context  [flags]
  code-review-helper spawn-batch     [flags]

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

// runFinalize: dedup, gate, snap, render — produce consolidated.json,
// payload.json, fallback.md.
func runFinalize(argv []string) error {
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	diffPath := fs.String("diff", "", "path to unified diff file")
	findingsDir := fs.String("findings-dir", "", "directory containing findings/<role>.json files")
	priorPath := fs.String("prior-issues", "", "path to prior-issues.json")
	headSHA := fs.String("head-sha", "", "full HEAD SHA")
	owner := fs.String("owner", "", "GitHub owner")
	repo := fs.String("repo", "", "GitHub repo")
	prNumber := fs.Int("pr-number", 0, "pull request number")
	outConsolidated := fs.String("out-consolidated", "", "output path for consolidated.json")
	outPayload := fs.String("out-payload", "", "output path for payload.json")
	outPendingPayload := fs.String("out-pending-payload", "", "output path for payload-pending.json (no `event` field, used for two-step fallback)")
	outBody := fs.String("out-body", "", "output path for payload-body.json (just `{\"body\": ...}`, used for the submit step of the two-step fallback)")
	outFallback := fs.String("out-fallback", "", "output path for fallback.md")
	expected := fs.String("expected-roles", "", "comma-separated list of expected specialist roles (optional)")
	includeIDs := fs.String("include-finding-ids", "", "comma-separated specialist finding IDs to keep for posting (e.g. \"sec-1,err-2\"); the user's step-4 subset choice routes through this flag instead of editing payload.json by hand. Default empty = keep all classified findings. Unknown IDs are a hard error.")
	excludeIDs := fs.String("exclude-finding-ids", "", "comma-separated specialist finding IDs to drop from posting (e.g. \"qual-3\"); ignored if --include-finding-ids is also set. Unknown IDs are a hard error.")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *diffPath == "" || *findingsDir == "" || *priorPath == "" || *headSHA == "" ||
		*owner == "" || *repo == "" || *outConsolidated == "" || *outPayload == "" ||
		*outPendingPayload == "" || *outBody == "" || *outFallback == "" {
		return fmt.Errorf("finalize: missing required flag (run with -h)")
	}
	_ = prNumber // currently logged-only; reserved for future use

	df, err := os.Open(*diffPath)
	if err != nil {
		return fmt.Errorf("open diff: %w", err)
	}
	parsed, err := diffpkg.Parse(df)
	df.Close()
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	expectedRoles := splitCSV(*expected)
	loaded, err := findings.LoadDir(*findingsDir, expectedRoles)
	if err != nil {
		return fmt.Errorf("load findings: %w", err)
	}

	// Surface schema-rejected findings to the operator. The lead agent reads
	// consolidated.json at step 4 and is expected to mention these to the user
	// before posting; the stderr line is so they're visible in the bash output
	// even if the lead skips the consolidated.json check.
	for _, inv := range loaded.InvalidFindings {
		fmt.Fprintf(os.Stderr, "code-review-helper: warning: dropped %s finding %q: %s\n",
			inv.Role, inv.ID, inv.Reason)
	}

	prior, err := loadPriorIssues(*priorPath)
	if err != nil {
		return fmt.Errorf("load prior issues: %w", err)
	}

	// Pipeline.
	step1 := dedup.Positional(loaded.Findings)
	inDiff := func(path string) bool { _, ok := parsed.ValidLines[path]; return ok }
	step2 := dedup.Semantic(step1, inDiff)
	step3, dropped := dedup.PriorReview(step2, prior, parsed.IsAddedLine)
	step4 := gates.Filter(step3)
	classified := lines.Classify(step4, parsed)

	// Subset-post filter (--include-finding-ids / --exclude-finding-ids).
	// Applied AFTER classification so the IDs match what the user saw in
	// consolidated.json at step 4, and AFTER the consolidated.json emit so the
	// consolidated file remains a true pre-filter audit log. Unknown IDs are a
	// hard error to surface user mistakes (typo, stale ID) at the failing call
	// site rather than silently skipping.
	includeList := splitCSV(*includeIDs)
	excludeList := splitCSV(*excludeIDs)
	var pf *postingFilter
	if len(includeList) > 0 || len(excludeList) > 0 {
		if len(includeList) > 0 && len(excludeList) > 0 {
			fmt.Fprintln(os.Stderr, "code-review-helper: warning: both --include-finding-ids and --exclude-finding-ids supplied; --include-finding-ids wins, --exclude-finding-ids ignored")
			excludeList = nil
		}
		knownIDs := collectIDs(classified.InlineEligible, classified.SummaryOnly)
		if unknown := missingIDs(includeList, knownIDs); len(unknown) > 0 {
			return fmt.Errorf("finalize: --include-finding-ids referenced unknown id(s) %v (available: %v)", unknown, knownIDs)
		}
		if unknown := missingIDs(excludeList, knownIDs); len(unknown) > 0 {
			return fmt.Errorf("finalize: --exclude-finding-ids referenced unknown id(s) %v (available: %v)", unknown, knownIDs)
		}
		pf = &postingFilter{IncludeIDs: includeList, ExcludeIDs: excludeList}
	}

	// Outputs. Consolidated.json reflects the pre-filter classification — it is
	// the audit log of what dedup + gates + classify produced.
	cf := consolidatedFile{
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
	if err := writeJSON(*outConsolidated, cf); err != nil {
		return err
	}

	// Payload-side findings reflect the POST-filter subset. When pf is nil this
	// is a no-op (filterFindings returns the input unchanged).
	postInline := filterFindings(classified.InlineEligible, includeList, excludeList)
	postSummary := filterFindings(classified.SummaryOnly, includeList, excludeList)

	buildIn := payload.BuildInput{
		HeadSHA:        *headSHA,
		Owner:          *owner,
		Repo:           *repo,
		InlineEligible: postInline,
		SummaryOnly:    postSummary,
		Specialists:    loaded.Specialists,
	}

	rev := payload.Build(buildIn)
	payloadJSON, err := payload.MarshalJSON(rev)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := os.WriteFile(*outPayload, payloadJSON, 0o644); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	pending := payload.BuildPending(buildIn)
	pendingJSON, err := payload.MarshalJSON(pending)
	if err != nil {
		return fmt.Errorf("marshal pending payload: %w", err)
	}
	if err := os.WriteFile(*outPendingPayload, pendingJSON, 0o644); err != nil {
		return fmt.Errorf("write pending payload: %w", err)
	}

	bodyJSON, err := json.MarshalIndent(payload.BodyOnly{Body: rev.Body}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	if err := os.WriteFile(*outBody, bodyJSON, 0o644); err != nil {
		return fmt.Errorf("write body: %w", err)
	}

	fallback := payload.Fallback(buildIn, "")
	if err := os.WriteFile(*outFallback, []byte(fallback), 0o644); err != nil {
		return fmt.Errorf("write fallback: %w", err)
	}

	return nil
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
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				// Trim whitespace so callers can write "sec-1, err-2" without
				// the space surviving into the validator (which would then
				// reject " err-2" as unknown).
				entry := strings.TrimSpace(s[start:i])
				if entry != "" {
					out = append(out, entry)
				}
			}
			start = i + 1
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
	sort.Strings(out)
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

// runSpawnBatch: render a roster-driven tool-call batch as verbatim markdown
// the /code-review skill's lead echoes as a single assistant message. See
// internal/spawnbatch for the contract.
func runSpawnBatch(argv []string) error {
	fs := flag.NewFlagSet("spawn-batch", flag.ContinueOnError)
	kindStr := fs.String("kind", "", "one of: tasks, agents, finalize, shutdown")
	rosterPath := fs.String("roster", "", "path to roster.json (required)")
	assignmentsPath := fs.String("assignments-file", "", "path to assignments.json (required for --kind agents)")
	reviewTmpDir := fs.String("review-tmpdir", "", "path to $REVIEW_TMPDIR (required for --kind agents)")
	owner := fs.String("owner", "", "GitHub owner (required for --kind agents)")
	repo := fs.String("repo", "", "GitHub repo (required for --kind agents)")
	prNumber := fs.Int("pr-number", 0, "PR number (required for --kind agents)")
	out := fs.String("out", "", "output markdown path (required; '-' for stdout)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *kindStr == "" || *rosterPath == "" || *out == "" {
		return fmt.Errorf("spawn-batch: --kind, --roster, --out are all required")
	}
	kind, err := spawnbatch.ParseKind(*kindStr)
	if err != nil {
		return fmt.Errorf("spawn-batch: %w", err)
	}

	roster, err := spawnbatch.LoadRoster(*rosterPath)
	if err != nil {
		return fmt.Errorf("spawn-batch: %w", err)
	}

	in := spawnbatch.Input{
		Kind:         kind,
		Roster:       roster,
		ReviewTmpDir: *reviewTmpDir,
		Owner:        *owner,
		Repo:         *repo,
		PRNumber:     *prNumber,
	}
	if kind == spawnbatch.KindAgents {
		if *assignmentsPath == "" {
			return fmt.Errorf("spawn-batch: --kind agents requires --assignments-file")
		}
		assigns, err := spawnbatch.LoadAssignments(*assignmentsPath)
		if err != nil {
			return fmt.Errorf("spawn-batch: %w", err)
		}
		in.Assignments = assigns
	}

	body, err := spawnbatch.Build(in)
	if err != nil {
		return fmt.Errorf("spawn-batch: %w", err)
	}

	if *out == "-" {
		_, err := io.WriteString(os.Stdout, body)
		return err
	}
	return os.WriteFile(*out, []byte(body), 0o644)
}
