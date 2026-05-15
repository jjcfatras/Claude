package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestBuild_EndToEnd seeds a $REVIEW_TMPDIR with the JSON artifacts the lead
// would write before invoking bundle-context, runs Build, and asserts the
// emitted markdown contains every required section in the documented order.
func TestBuild_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.json"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `["security","quality"]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{"last_review_date":null,"last_review_commit":null,"issues":[]}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)

	rubric := filepath.Join(dir, "rubric.md")
	mustWrite(t, rubric, "# Rubric\n\nRubric body.\n")

	out, err := Build(Input{
		ReviewTmpDir:     dir,
		HeadSHA:          "abcdef0123456789",
		PRNumber:         42,
		Owner:            "Test-Owner",
		Repo:             "test-repo",
		SummaryParagraph: "This PR does a thing.",
		RubricPath:       rubric,
		MaxSourceBytes:   0,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	wantSections := []string{
		"# Code review spawn context (PR #42, Test-Owner/test-repo)",
		"## Per-PR",
		"- HEAD_SHA: abcdef0123456789",
		"- PR_NUMBER: 42",
		"## Summary\nThis PR does a thing.",
		"## Changed files\n[\"a.ts\",\"b.json\"]",
		"## Roster",
		"\"security\"",
		"## Prior issues",
		"## CLAUDE.md content",
		"## Rubric\n# Rubric\n\nRubric body.\n",
	}
	for _, want := range wantSections {
		if !strings.Contains(out, want) {
			t.Errorf("missing section %q in bundle output", want)
		}
	}

	// Sections appear in the documented order.
	idx := func(s string) int { return strings.Index(out, s) }
	order := []string{"## Per-PR", "## Summary", "## Changed files", "## Roster", "## Prior issues", "## CLAUDE.md content", "## Rubric"}
	for i := 1; i < len(order); i++ {
		if idx(order[i-1]) > idx(order[i]) {
			t.Errorf("section %q appears before %q (out-of-order)", order[i], order[i-1])
		}
	}

	// findings/ pre-creation guarantee is part of the bundle's contract with specialists.
	if !strings.Contains(out, "findings/ subdirectory is pre-created") {
		t.Errorf("missing findings/ pre-creation guarantee")
	}
}

// TestBuild_MigrationHistoryGated verifies migration-history.json is only
// rendered when present, matching the gated section semantics in
// commands/code-review.md step 2b.
func TestBuild_MigrationHistoryGated(t *testing.T) {
	dir := t.TempDir()
	seedMinimalArtifacts(t, dir)
	rubric := seedRubric(t, dir)

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 0,
	})
	if err != nil {
		t.Fatalf("Build (no migration): %v", err)
	}
	if strings.Contains(out, "## Migration history") {
		t.Errorf("Migration history section appeared without migration-history.json on disk")
	}

	mustWrite(t, filepath.Join(dir, "migration-history.json"), `{"migrations/x":[{"path":"migrations/x/2026-01.ts","first_line":"// migration"}]}`)
	out, err = Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 0,
	})
	if err != nil {
		t.Fatalf("Build (with migration): %v", err)
	}
	if !strings.Contains(out, "## Migration history") {
		t.Errorf("Migration history section missing despite file present")
	}
	if !strings.Contains(out, "migrations/x/2026-01.ts") {
		t.Errorf("Migration history section did not include file content")
	}
}

// TestBuild_SourceEmbedding exercises the --max-source-bytes path: small
// files inline, oversize files render as a placeholder.
func TestBuild_SourceEmbedding(t *testing.T) {
	repo := t.TempDir()
	if err := runIn(repo, "git", "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := runIn(repo, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := runIn(repo, "git", "config", "user.name", "test"); err != nil {
		t.Fatalf("git config name: %v", err)
	}
	mustWrite(t, filepath.Join(repo, "small.ts"), "export const x = 1;\n")
	big := strings.Repeat("// pad\n", 2000)
	mustWrite(t, filepath.Join(repo, "big.ts"), big)
	if err := runIn(repo, "git", "add", "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := runIn(repo, "git", "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	sha, err := runOutIn(repo, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	sha = strings.TrimSpace(sha)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["small.ts","big.ts","missing.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        sha,
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 1024,
		GitWorkdir:     repo,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !strings.Contains(out, "## Source at HEAD") {
		t.Errorf("missing source section header")
	}
	if !strings.Contains(out, "### small.ts\n```ts\nexport const x = 1;") {
		t.Errorf("small.ts not embedded as expected")
	}
	if !strings.Contains(out, "### big.ts\n_omitted:") {
		t.Errorf("big.ts not rendered as omitted placeholder")
	}
	if !strings.Contains(out, "### missing.ts\n_omitted:") {
		t.Errorf("missing.ts not rendered as omitted placeholder for missing file")
	}
}

// TestBuild_RepoRoot verifies the bundle exposes REPO_ROOT in the per-PR
// header so specialists never have to synthesize repo-relative paths from cwd
// (which may be a worktree not checked out to HEAD).
func TestBuild_RepoRoot(t *testing.T) {
	dir := t.TempDir()
	seedMinimalArtifacts(t, dir)
	rubric := seedRubric(t, dir)

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RepoRoot:       "/path/to/repo-root",
		RubricPath:     rubric,
		MaxSourceBytes: 0,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(out, "- REPO_ROOT: /path/to/repo-root") {
		t.Errorf("missing REPO_ROOT line in Per-PR header")
	}

	// Verify omission when not provided (existing tests already cover this
	// implicitly, but assert explicitly here so future regressions surface).
	out, err = Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 0,
	})
	if err != nil {
		t.Fatalf("Build (no repo-root): %v", err)
	}
	if strings.Contains(out, "REPO_ROOT") {
		t.Errorf("REPO_ROOT appeared without RepoRoot input set")
	}
}

// TestBuild_RubricExternal verifies the rubric is copied to the external path
// and replaced by a RUBRIC_PATH pointer line when RubricExternal is set.
// Keeps spawn-context.md under the Read tool's 256 KB byte cap.
func TestBuild_RubricExternal(t *testing.T) {
	dir := t.TempDir()
	seedMinimalArtifacts(t, dir)

	rubricSrc := filepath.Join(dir, "rubric-src.md")
	rubricBody := "# Rubric\n\nRubric body content.\n"
	mustWrite(t, rubricSrc, rubricBody)

	rubricDst := filepath.Join(dir, "rubric.md")

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubricSrc,
		RubricExternal: rubricDst,
		MaxSourceBytes: 0,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(out, "## Rubric") {
		t.Errorf("rubric section appeared in bundle despite RubricExternal being set")
	}
	if strings.Contains(out, "Rubric body content.") {
		t.Errorf("rubric body inlined in bundle despite RubricExternal being set")
	}
	if !strings.Contains(out, "- RUBRIC_PATH: "+rubricDst) {
		t.Errorf("missing RUBRIC_PATH pointer line in Per-PR header")
	}

	// Rubric copied to the external path verbatim.
	got, err := os.ReadFile(rubricDst)
	if err != nil {
		t.Fatalf("read external rubric: %v", err)
	}
	if string(got) != rubricBody {
		t.Errorf("external rubric mismatch:\nwant: %q\ngot:  %q", rubricBody, string(got))
	}
}

// TestBuild_SourceIndexBlock verifies the `## Source index` section: every
// changed path is listed with its status so specialists don't have to scroll
// the source section to know whether a file is embedded.
func TestBuild_SourceIndexBlock(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","c.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// Synthetic git show: a.ts is small, b.ts is over the per-file cap,
	// c.ts errors.
	showFn := func(workdir, sha, path string) (string, int, error) {
		switch path {
		case "a.ts":
			content := "export const a = 1;\n"
			return content, len(content), nil
		case "b.ts":
			content := strings.Repeat("// pad\n", 200) // ~1.4 KB
			return content, len(content), nil
		case "c.ts":
			return "", 0, &fakeErr{"path not found at HEAD"}
		}
		return "", 0, &fakeErr{"unexpected path " + path}
	}

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 512, // forces b.ts to be omitted
		GitShowFn:      showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !strings.Contains(out, "## Source index") {
		t.Error("missing `## Source index` header")
	}
	if !strings.Contains(out, "| File | Status | Bytes | Note |") {
		t.Error("Source index table header missing")
	}
	if !strings.Contains(out, "| a.ts | embedded | 20 |  |") {
		t.Error("a.ts row missing or wrong in Source index")
	}
	if !strings.Contains(out, "| b.ts | omitted | 1400 |") {
		t.Error("b.ts omitted row missing or wrong in Source index")
	}
	if !strings.Contains(out, "| c.ts | omitted | 0 | git show:") {
		t.Errorf("c.ts (git-show error) row missing or wrong in Source index; got:\n%s", out)
	}

	// Source index appears BEFORE Source at HEAD.
	idxIdx := strings.Index(out, "## Source index")
	srcIdx := strings.Index(out, "## Source at HEAD")
	if idxIdx < 0 || srcIdx < 0 || idxIdx > srcIdx {
		t.Errorf("Source index must precede Source at HEAD; got positions %d, %d", idxIdx, srcIdx)
	}
}

// TestBuild_AggregateByteCap verifies the aggregate-byte cap: the first file
// to overflow records the "would exceed" reason, and subsequent files
// short-circuit with a distinct "already exhausted" reason and no git show
// subprocess.
func TestBuild_AggregateByteCap(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","c.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// Each file is 600 bytes; per-file cap 1024 lets all three pass that gate,
	// but aggregate cap 1000 only fits one.
	body := strings.Repeat("x", 600)
	var shownPaths []string
	showFn := func(workdir, sha, path string) (string, int, error) {
		shownPaths = append(shownPaths, path)
		return body, len(body), nil
	}

	out, err := Build(Input{
		ReviewTmpDir:        dir,
		HeadSHA:             "deadbeef",
		PRNumber:            1,
		Owner:               "o",
		Repo:                "r",
		RubricPath:          rubric,
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: 1000,
		GitShowFn:           showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// a.ts embeds (600 bytes fits both caps).
	if !strings.Contains(out, "| a.ts | embedded | 600 |  |") {
		t.Errorf("a.ts should be embedded; got:\n%s", out)
	}
	// b.ts is the first overflow — recorded with the "would exceed" reason.
	if !strings.Contains(out, "| b.ts | omitted | 600 | would exceed 1000-byte aggregate bundle cap") {
		t.Errorf("b.ts should be omitted with the would-exceed reason; got:\n%s", out)
	}
	// c.ts short-circuits — size not measured, distinct "already exhausted" reason.
	if !strings.Contains(out, "| c.ts | omitted | 0 | aggregate bundle cap already exhausted") {
		t.Errorf("c.ts should be omitted with the already-exhausted reason and size 0; got:\n%s", out)
	}
	// Per-file cap reason should NOT fire — the section heading mentions
	// "per-file cap:" but the omission-reason text is "bytes > N per-file cap".
	if strings.Contains(out, "per-file cap — use `git show") {
		t.Errorf("per-file cap reason leaked into output despite all files being under it; got:\n%s", out)
	}
	// showFn should have been called for a.ts and b.ts but not c.ts.
	if want := []string{"a.ts", "b.ts"}; !slices.Equal(shownPaths, want) {
		t.Errorf("showFn calls = %v, want %v (c.ts must be short-circuited)", shownPaths, want)
	}
}

// TestBuild_AggregateCapDisabledWhenZero verifies MaxTotalSourceBytes=0
// preserves the legacy per-file-only behavior.
func TestBuild_AggregateCapDisabledWhenZero(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	body := strings.Repeat("x", 600)
	showFn := func(workdir, sha, path string) (string, int, error) {
		return body, len(body), nil
	}

	out, err := Build(Input{
		ReviewTmpDir:        dir,
		HeadSHA:             "deadbeef",
		PRNumber:            1,
		Owner:               "o",
		Repo:                "r",
		RubricPath:          rubric,
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: 0, // disabled — per-file cap is the only gate
		GitShowFn:           showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Both files should be embedded.
	if !strings.Contains(out, "| a.ts | embedded | 600 |") || !strings.Contains(out, "| b.ts | embedded | 600 |  |") {
		t.Errorf("expected both files embedded when aggregate cap disabled; got:\n%s", out)
	}
	if strings.Contains(out, "aggregate bundle cap") {
		t.Error("aggregate-cap reason leaked into output despite MaxTotalSourceBytes=0")
	}
}

// fakeErr is a simple error type used by tests injecting GitShowFn failures.
type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func seedMinimalArtifacts(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
}

func seedRubric(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "rubric.md")
	mustWrite(t, p, "# Rubric\n")
	return p
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

func runOutIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
