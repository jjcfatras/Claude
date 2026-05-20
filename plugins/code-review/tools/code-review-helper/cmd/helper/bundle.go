package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/bundle"
)

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
	maxTotalSourceBytes := fs.Int("max-total-source-bytes", 50000, "aggregate cap across all embedded files; once running embedded byte count + next file size > cap, the next file and all remaining files are marked _omitted_ with the aggregate-cap reason. Default sized so the assembled bundle stays under the Read tool's 25,000-token cap (dense source tokenizes at ~2.2 bytes/token; 50,000 bytes ≈ 22,000 tokens, leaving headroom for the non-source sections). 0 disables (per-file cap still applies).")
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
		DiffPath:            fmt.Sprintf("%s/pr-%d.diff", *reviewTmpDir, *prNumber),
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
