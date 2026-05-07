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
	fmt.Fprint(os.Stderr, `code-review-helper — deterministic backend for the /code-review skill

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

	// Outputs.
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
	}
	if err := writeJSON(*outConsolidated, cf); err != nil {
		return err
	}

	buildIn := payload.BuildInput{
		HeadSHA:        *headSHA,
		Owner:          *owner,
		Repo:           *repo,
		InlineEligible: classified.InlineEligible,
		SummaryOnly:    classified.SummaryOnly,
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
				out = append(out, s[start:i])
			}
			start = i + 1
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
	rubricOut := fs.String("rubric-out", "", "when set, copy the rubric verbatim to this path and emit RUBRIC_PATH in the bundle header instead of inlining the rubric body — keeps spawn-context.md under the 25k-token Read cap")
	maxSourceBytes := fs.Int("max-source-bytes", 12288, "embed each changed file <= this many bytes from HEAD; 0 disables source embedding")
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
		ReviewTmpDir:     *reviewTmpDir,
		HeadSHA:          *headSHA,
		PRNumber:         *prNumber,
		Owner:            *owner,
		Repo:             *repo,
		RepoRoot:         *repoRoot,
		SummaryParagraph: summary,
		RubricPath:       *rubricPath,
		RubricExternal:   *rubricOut,
		MaxSourceBytes:   *maxSourceBytes,
		GitWorkdir:       *gitWorkdir,
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
