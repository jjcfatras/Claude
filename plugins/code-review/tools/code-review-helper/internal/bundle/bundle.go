// Package bundle assembles $REVIEW_TMPDIR/spawn-context.md deterministically.
//
// Earlier revisions had the lead model concatenate per-PR scalars, on-disk
// JSON artifacts, and the verbatim rubric into the bundle file as a Write
// call — observed cost was ~4 minutes of pure model-output streaming on every
// run (transcript b5a8dd9d, May 2026). The pipeline is mechanical: read
// changed-files.json / roster.json / prior-issues.json / claude-md-files.json
// (and optional migration-history.json), read the rubric, optionally read each
// small-enough changed file at HEAD via `git show HEAD_SHA:path`, and emit
// markdown. This package owns that assembly so the lead's serial output stream
// stays small.
package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Input is what the `bundle-context` subcommand collects from flags +
// $REVIEW_TMPDIR.
type Input struct {
	ReviewTmpDir        string
	HeadSHA             string
	PRNumber            int
	Owner               string
	Repo                string
	RepoRoot            string // repo working-tree root for specialist `git -C <REPO_ROOT> ...` calls; emitted in Per-PR header so specialists never synthesize paths from cwd (which may be a worktree not checked out to HEAD)
	SummaryParagraph    string // contents of the file the prep agent wrote
	RubricPath          string // path to references/code-review-rubrics.md
	RubricExternal      string // when non-empty, helper writes rubric to this path and emits `RUBRIC_PATH: <path>` in the bundle header instead of inlining the rubric body — keeps spawn-context.md under the 256 KB Read byte cap
	MaxSourceBytes      int    // per-file embedding cap; 0 disables
	MaxTotalSourceBytes int    // aggregate cap across all embedded files; 0 disables (only per-file cap applies). Once running embedded bytes + next file size > cap, the next file and all remaining files are marked _omitted_ with the aggregate-cap reason.
	GitWorkdir          string // cwd for `git show` calls (repo root)

	// GitShowFn allows tests to inject a deterministic substitute for
	// `git show HEAD_SHA:path`. Returns (content, size, error). When nil, the
	// production gitShowAtHead is used.
	GitShowFn func(workdir, sha, path string) (string, int, error)
}

// Build reads the on-disk artifacts in ReviewTmpDir and emits the spawn-context
// bundle as a string. The shape matches the template in
// commands/code-review.md step 2b verbatim — keep them in sync.
func Build(in Input) (string, error) {
	if in.ReviewTmpDir == "" {
		return "", fmt.Errorf("ReviewTmpDir is required")
	}
	if in.RubricPath == "" {
		return "", fmt.Errorf("RubricPath is required")
	}

	changedFilesRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "changed-files.json"))
	if err != nil {
		return "", fmt.Errorf("read changed-files.json: %w", err)
	}
	rosterRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "roster.json"))
	if err != nil {
		return "", fmt.Errorf("read roster.json: %w", err)
	}
	priorIssuesRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "prior-issues.json"))
	if err != nil {
		return "", fmt.Errorf("read prior-issues.json: %w", err)
	}
	claudeMDRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "claude-md-files.json"))
	if err != nil {
		return "", fmt.Errorf("read claude-md-files.json: %w", err)
	}

	migrationRaw, err := os.ReadFile(filepath.Join(in.ReviewTmpDir, "migration-history.json"))
	hasMigration := err == nil
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read migration-history.json: %w", err)
	}

	rubricRaw, err := os.ReadFile(in.RubricPath)
	if err != nil {
		return "", fmt.Errorf("read rubric %s: %w", in.RubricPath, err)
	}

	// When the caller requested the rubric be externalized, copy it verbatim
	// to that path before assembling the bundle. Specialists Read the bundle
	// once, then Read the rubric path once — keeping each file under the
	// Read tool's 256 KB byte cap (the binding limit; the 25k-token cap is
	// roomier on most diffs).
	if in.RubricExternal != "" {
		if err := os.WriteFile(in.RubricExternal, rubricRaw, 0o644); err != nil {
			return "", fmt.Errorf("write rubric to %s: %w", in.RubricExternal, err)
		}
	}

	var changedFiles []string
	if err := json.Unmarshal(changedFilesRaw, &changedFiles); err != nil {
		return "", fmt.Errorf("parse changed-files.json: %w", err)
	}

	var b bytes.Buffer

	fmt.Fprintf(&b, "# Code review spawn context (PR #%d, %s/%s)\n\n", in.PRNumber, in.Owner, in.Repo)

	fmt.Fprint(&b, "## Per-PR\n")
	fmt.Fprintf(&b, "- HEAD_SHA: %s\n", in.HeadSHA)
	fmt.Fprintf(&b, "- PR_NUMBER: %d\n", in.PRNumber)
	fmt.Fprintf(&b, "- REVIEW_TMPDIR: %s\n", in.ReviewTmpDir)
	fmt.Fprintf(&b, "- DIFF: %s/pr-%d.diff\n", in.ReviewTmpDir, in.PRNumber)
	if in.RepoRoot != "" {
		fmt.Fprintf(&b, "- REPO_ROOT: %s\n", in.RepoRoot)
	}
	if in.RubricExternal != "" {
		fmt.Fprintf(&b, "- RUBRIC_PATH: %s (Read this once after the bundle — moved out of the bundle body to keep spawn-context.md under the Read tool's 256 KB byte cap)\n", in.RubricExternal)
	}
	fmt.Fprintf(&b, "- The findings/ subdirectory is pre-created by the orchestrator — do not mkdir or pre-test it before your Write.\n\n")

	fmt.Fprint(&b, "## Summary\n")
	summary := strings.TrimSpace(in.SummaryParagraph)
	if summary == "" {
		summary = "(no summary paragraph supplied)"
	}
	fmt.Fprintf(&b, "%s\n\n", summary)

	fmt.Fprint(&b, "## Changed files\n")
	b.Write(bytes.TrimRight(changedFilesRaw, "\n"))
	b.WriteString("\n\n")

	fmt.Fprint(&b, "## Roster (active specialists)\n")
	b.Write(bytes.TrimRight(rosterRaw, "\n"))
	b.WriteString("\n\n")

	fmt.Fprint(&b, "## Prior issues (most recent prior Claude Code review on this PR; may be empty)\n")
	b.Write(bytes.TrimRight(priorIssuesRaw, "\n"))
	b.WriteString("\n\n")

	fmt.Fprint(&b, "## CLAUDE.md content (paths + contents from step 1b; may be empty `{}`)\n")
	b.Write(bytes.TrimRight(claudeMDRaw, "\n"))
	b.WriteString("\n\n")

	if hasMigration {
		fmt.Fprint(&b, "## Migration history\n")
		b.Write(bytes.TrimRight(migrationRaw, "\n"))
		b.WriteString("\n\n")
	}

	if in.MaxSourceBytes > 0 && len(changedFiles) > 0 {
		decisions := planSourceSection(in, changedFiles)
		b.WriteString(renderSourceIndex(decisions))
		b.WriteString(renderSourceContent(decisions, in))
	}

	if in.RubricExternal == "" {
		fmt.Fprint(&b, "## Rubric\n")
		b.Write(rubricRaw)
		if !bytes.HasSuffix(rubricRaw, []byte("\n")) {
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}

// sourceStatus enumerates the two possible source-section dispositions.
type sourceStatus string

const (
	sourceEmbedded sourceStatus = "embedded"
	sourceOmitted  sourceStatus = "omitted"
)

// sourceDecision is one row of the per-file embed-or-omit plan. `planSourceSection`
// produces these up front so the Source index and Source content sections agree.
type sourceDecision struct {
	Path    string
	Size    int          // size returned by git show (0 when not measured or err)
	Content string       // populated only when Status == sourceEmbedded
	Status  sourceStatus // sourceEmbedded | sourceOmitted
	Reason  string       // omission rationale (per-file cap, aggregate cap, git show error)
}

// planSourceSection iterates the changed paths in lexicographic order and
// decides per-file whether to embed the content, mark it omitted under the
// per-file cap, or mark it omitted under the aggregate-bytes cap. Lexicographic
// ordering is deterministic, greppable, and golden-test friendly. Per-file cap
// is checked BEFORE the aggregate cap so a single large file is reported with
// the more useful per-file reason ("47 KB > 32 KB cap") rather than being
// blamed on cumulative usage.
//
// Once the aggregate cap is reached, remaining files skip the `git show`
// subprocess entirely — no future file can embed, and their size is not
// load-bearing for the index (the row still appears with size 0 and a
// distinct reason directing the specialist to fetch on demand). On a 176-file
// PR with default caps this avoids ~100+ subprocesses.
func planSourceSection(in Input, paths []string) []sourceDecision {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)

	showFn := in.GitShowFn
	if showFn == nil {
		showFn = gitShowAtHead
	}

	out := make([]sourceDecision, 0, len(sorted))
	running := 0
	exhausted := false
	for _, p := range sorted {
		if exhausted {
			out = append(out, sourceDecision{
				Path:   p,
				Status: sourceOmitted,
				Reason: fmt.Sprintf("aggregate bundle cap already exhausted (running=%d ≥ %d) — use `git show %s:%s`", running, in.MaxTotalSourceBytes, in.HeadSHA, p),
			})
			continue
		}
		content, size, err := showFn(in.GitWorkdir, in.HeadSHA, p)
		d := sourceDecision{Path: p, Size: size}
		switch {
		case err != nil:
			d.Status = sourceOmitted
			d.Reason = fmt.Sprintf("git show: %s", err)
		case size > in.MaxSourceBytes:
			d.Status = sourceOmitted
			d.Reason = fmt.Sprintf("%d bytes > %d per-file cap — use `git show %s:%s`", size, in.MaxSourceBytes, in.HeadSHA, p)
		case in.MaxTotalSourceBytes > 0 && running+size > in.MaxTotalSourceBytes:
			d.Status = sourceOmitted
			d.Reason = fmt.Sprintf("would exceed %d-byte aggregate bundle cap (running=%d, file=%d) — use `git show %s:%s`", in.MaxTotalSourceBytes, running, size, in.HeadSHA, p)
			// First aggregate-cap trip flips the latch: subsequent files would
			// almost always also overflow, and we save the subprocess by
			// short-circuiting. The rare case of a smaller-than-headroom file
			// later in the alphabet not embedding is an acceptable trade for
			// ~100+ saved subprocesses on a 176-file PR.
			exhausted = true
		default:
			d.Status = sourceEmbedded
			d.Content = content
			running += size
		}
		out = append(out, d)
	}
	return out
}

// renderSourceIndex emits a markdown table listing every changed path with its
// status. Specialists check this index FIRST to decide whether a file is in the
// bundle (read inline / paginate via Read) or needs a `git show` fetch.
func renderSourceIndex(decisions []sourceDecision) string {
	var b bytes.Buffer
	fmt.Fprint(&b, "## Source index\n\n")
	fmt.Fprint(&b, "Specialists: search this index FIRST. `embedded` → read content under `## Source at HEAD`. `omitted` → fetch via `git show HEAD_SHA:path`. Do not `git show` files marked `embedded`.\n\n")
	fmt.Fprint(&b, "| File | Status | Bytes | Note |\n")
	fmt.Fprint(&b, "|------|--------|-------|------|\n")
	for _, d := range decisions {
		note := ""
		if d.Status == sourceOmitted {
			note = d.Reason
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", d.Path, d.Status, d.Size, note)
	}
	b.WriteString("\n")
	return b.String()
}

// renderSourceContent emits the per-file content section. Embedded files get a
// fenced code block; omitted files get a one-line placeholder so the section is
// still self-describing as a manifest.
func renderSourceContent(decisions []sourceDecision, in Input) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "## Source at HEAD (per-file cap: %d bytes; aggregate cap: %d bytes; omitted files listed in `## Source index` — use `git show %s:<path>` to fetch)\n\n", in.MaxSourceBytes, in.MaxTotalSourceBytes, in.HeadSHA)
	for _, d := range decisions {
		if d.Status == sourceOmitted {
			fmt.Fprintf(&b, "### %s\n_omitted: %s_\n\n", d.Path, d.Reason)
			continue
		}
		lang := languageHint(d.Path)
		fmt.Fprintf(&b, "### %s\n```%s\n%s", d.Path, lang, d.Content)
		if !strings.HasSuffix(d.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	}
	return b.String()
}

// gitShowAtHead returns the contents of `path` at HEAD_SHA. If the file is
// missing at HEAD (e.g., a deletion), git show exits 128 and the error is
// returned for the caller to render as an omission notice.
func gitShowAtHead(workdir, sha, path string) (string, int, error) {
	cmd := exec.Command("git", "show", sha+":"+path)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", 0, fmt.Errorf("git show: %s", msg)
	}
	out := stdout.String()
	return out, len(out), nil
}

// languageHint maps a file extension to a fenced-block language tag. Returns
// empty string for unknown extensions (renders as a plain fence, which is
// fine).
func languageHint(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js":
		return "js"
	case ".jsx":
		return "jsx"
	case ".json":
		return "json"
	case ".go":
		return "go"
	case ".py":
		return "py"
	case ".rb":
		return "rb"
	case ".rs":
		return "rs"
	case ".java":
		return "java"
	case ".kt":
		return "kt"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".tf", ".hcl":
		return "hcl"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "md"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".html":
		return "html"
	case ".xml":
		return "xml"
	case ".dockerfile":
		return "dockerfile"
	}
	if strings.EqualFold(filepath.Base(path), "Dockerfile") {
		return "dockerfile"
	}
	return ""
}
