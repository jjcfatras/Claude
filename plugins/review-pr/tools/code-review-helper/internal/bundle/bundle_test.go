package bundle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
		"## CLAUDE.md files",
		"## Rubric\n# Rubric\n\nRubric body.\n",
	}
	for _, want := range wantSections {
		if !strings.Contains(out, want) {
			t.Errorf("missing section %q in bundle output", want)
		}
	}

	// Sections appear in the documented order.
	idx := func(s string) int { return strings.Index(out, s) }
	order := []string{"## Per-PR", "## Summary", "## Changed files", "## Roster", "## Prior issues", "## CLAUDE.md files", "## Rubric"}
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

// TestBuild_StubSummary verifies that when SummaryParagraph is empty, Build
// synthesizes a deterministic stub from the changed-files list rather than
// emitting a "(no summary paragraph supplied)" placeholder. The stub keeps
// the `## Summary` section orienting without requiring a lead-model turn.
func TestBuild_StubSummary(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"),
		`["src/api/foo.ts","src/api/bar.ts","src/api/baz.ts","src/web/index.tsx","docs/readme.md"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `["security"]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 0,
		// SummaryParagraph deliberately left empty.
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(out, "no summary paragraph supplied") {
		t.Errorf("legacy placeholder still emitted; got:\n%s", out)
	}
	if !strings.Contains(out, "PR touches 5 changed files across 2 top-level directories") {
		t.Errorf("stub summary header missing or wrong; got:\n%s", out)
	}
	// Top dirs: src (4), docs (1).
	if !strings.Contains(out, "Top: src (4), docs (1)") {
		t.Errorf("stub summary top-dirs missing or wrong; got:\n%s", out)
	}
}

// TestBuild_StubSummary_EmptyAndSingle verifies the degenerate-case branches
// (zero or one changed file). These show up rarely in production but the
// stub must still produce a well-formed sentence.
func TestBuild_StubSummary_EmptyAndSingle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed string
		want    string
	}{
		{"empty", `[]`, "No changed files."},
		{"single", `["a.ts"]`, "1 changed file: a.ts."},
		{"single-dir", `["src/a.ts","src/b.ts"]`, "across 1 top-level directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "changed-files.json"), tc.changed)
			mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
			mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
			mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
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
				t.Fatalf("Build: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in stub output; got:\n%s", tc.want, out)
			}
		})
	}
}

// TestBuild_MigrationHistoryGated verifies migration-history.json is only
// rendered when present, matching the gated section semantics in
// skills/review-pr/SKILL.md step 2b.
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
	if !strings.Contains(out, "### small.ts\n```ts\n1| export const x = 1;") {
		t.Errorf("small.ts not embedded as expected (with HEAD line-number gutter)")
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
			return "", 0, errors.New("path not found at HEAD")
		}
		return "", 0, errors.New("unexpected path " + path)
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
	if !strings.Contains(out, "| File | Status | Bundle line | Bytes | Note |") {
		t.Error("Source index table header missing")
	}
	if indexRow(out, "a.ts") != "| a.ts | embedded | * | 20 |  |" {
		t.Error("a.ts row missing or wrong in Source index")
	}
	if !strings.HasPrefix(indexRow(out, "b.ts"), "| b.ts | omitted | * | 1400 |") {
		t.Error("b.ts omitted row missing or wrong in Source index")
	}
	if !strings.HasPrefix(indexRow(out, "c.ts"), "| c.ts | omitted | * | 0 | git show:") {
		t.Errorf("c.ts (git-show error) row missing or wrong in Source index; got:\n%s", out)
	}

	// Source index appears BEFORE Source at HEAD.
	idxIdx := strings.Index(out, "## Source index")
	srcIdx := strings.Index(out, "## Source at HEAD")
	if idxIdx < 0 || srcIdx < 0 || idxIdx > srcIdx {
		t.Errorf("Source index must precede Source at HEAD; got positions %d, %d", idxIdx, srcIdx)
	}
}

// TestBuild_AggregateByteCap verifies the aggregate-byte cap: a file whose size
// would overflow the cap records the "would exceed" reason, and — because there
// is still headroom left — later files are measured rather than short-circuited.
func TestBuild_AggregateByteCap(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","c.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// Each file is 600 bytes; per-file cap 1024 lets all three pass that gate,
	// but aggregate cap 1000 only fits one.
	var shownPaths []string
	showFn := func(workdir, sha, path string) (string, int, error) {
		shownPaths = append(shownPaths, path)
		return padTo(path, 600), 600, nil
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
	if indexRow(out, "a.ts") != "| a.ts | embedded | * | 600 |  |" {
		t.Errorf("a.ts should be embedded; got:\n%s", out)
	}
	// b.ts is the first overflow — recorded with the "would exceed" reason.
	if !strings.HasPrefix(indexRow(out, "b.ts"), "| b.ts | omitted | * | 600 | would exceed 1000-byte aggregate bundle cap") {
		t.Errorf("b.ts should be omitted with the would-exceed reason; got:\n%s", out)
	}
	// c.ts is measured too — 400 bytes of headroom remain after a.ts, so it must
	// get its chance at the budget instead of being latched off unmeasured.
	if !strings.HasPrefix(indexRow(out, "c.ts"), "| c.ts | omitted | * | 600 | would exceed 1000-byte aggregate bundle cap") {
		t.Errorf("c.ts should be measured and omitted with the would-exceed reason; got:\n%s", out)
	}
	// Per-file cap reason should NOT fire — the section heading mentions
	// "per-file cap:" but the omission-reason text is "bytes > N per-file cap".
	if strings.Contains(out, "per-file cap — use `git show") {
		t.Errorf("per-file cap reason leaked into output despite all files being under it; got:\n%s", out)
	}
	// Every file is measured while headroom remains.
	if want := []string{"a.ts", "b.ts", "c.ts"}; !slices.Equal(shownPaths, want) {
		t.Errorf("showFn calls = %v, want %v (headroom remains, so none may be skipped)", shownPaths, want)
	}
}

// TestBuild_AggregateCapSkipsOnlyWhenHeadroomGone verifies the one case that
// still short-circuits the `git show` subprocess: the running total has reached
// the cap outright, so no remaining file can possibly fit.
func TestBuild_AggregateCapSkipsOnlyWhenHeadroomGone(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","c.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// a.ts exactly fills the aggregate cap, leaving zero headroom.
	var shownPaths []string
	showFn := func(workdir, sha, path string) (string, int, error) {
		shownPaths = append(shownPaths, path)
		return padTo(path, 600), 600, nil
	}

	out, err := Build(Input{
		ReviewTmpDir:        dir,
		HeadSHA:             "deadbeef",
		PRNumber:            1,
		Owner:               "o",
		Repo:                "r",
		RubricPath:          rubric,
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: 600,
		GitShowFn:           showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if indexRow(out, "a.ts") != "| a.ts | embedded | * | 600 |  |" {
		t.Errorf("a.ts should be embedded; got:\n%s", out)
	}
	for _, p := range []string{"b.ts", "c.ts"} {
		want := "| " + p + " | omitted | * | 0 | aggregate bundle cap already exhausted"
		if !strings.HasPrefix(indexRow(out, p), want) {
			t.Errorf("%s should be omitted unmeasured once headroom is gone; got:\n%s", p, out)
		}
	}
	if want := []string{"a.ts"}; !slices.Equal(shownPaths, want) {
		t.Errorf("showFn calls = %v, want %v (b/c must be skipped once headroom is gone)", shownPaths, want)
	}
}

// TestBuild_SourceOrderedByDiffChurn verifies the source section spends its
// budget most-churned-file-first rather than alphabetically, and that the diff
// map is emitted ahead of the source sections so it lands on the bundle's first
// page (both regressions observed in transcript 8428d029).
func TestBuild_SourceOrderedByDiffChurn(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["aaa-catalog.yaml","zzz-hot.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// zzz-hot.ts has 2 hunks; aaa-catalog.yaml has 1. Alphabetically the catalog
	// wins and eats the whole budget; by churn the hot file goes first.
	diffPath := filepath.Join(dir, "pr-1.diff")
	mustWrite(t, diffPath, "diff --git a/aaa-catalog.yaml b/aaa-catalog.yaml\n@@ -1 +1 @@\n-x\n+y\n"+
		"diff --git a/zzz-hot.ts b/zzz-hot.ts\n@@ -1 +1 @@\n-a\n+b\n@@ -9 +9 @@\n-c\n+d\n")

	var shownPaths []string
	showFn := func(workdir, sha, path string) (string, int, error) {
		shownPaths = append(shownPaths, path)
		return padTo(path, 600), 600, nil
	}

	out, err := Build(Input{
		ReviewTmpDir:        dir,
		HeadSHA:             "deadbeef",
		PRNumber:            1,
		Owner:               "o",
		Repo:                "r",
		RubricPath:          rubric,
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: 1000, // fits exactly one of the two files
		GitShowFn:           showFn,
		DiffPath:            diffPath,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The churned file gets the budget; the alphabetically-first catalog does not.
	if indexRow(out, "zzz-hot.ts") != "| zzz-hot.ts | embedded | * | 600 |  |" {
		t.Errorf("zzz-hot.ts (2 hunks) should win the budget; got:\n%s", out)
	}
	if !strings.HasPrefix(indexRow(out, "aaa-catalog.yaml"), "| aaa-catalog.yaml | omitted | * | 600 | would exceed") {
		t.Errorf("aaa-catalog.yaml (1 hunk) should be omitted; got:\n%s", out)
	}
	if want := []string{"zzz-hot.ts", "aaa-catalog.yaml"}; !slices.Equal(shownPaths, want) {
		t.Errorf("showFn order = %v, want %v (most-churned first)", shownPaths, want)
	}

	// Diff map precedes the source sections. Anchor on the newline so the
	// `## Source index` mention inside the `## Summary` prose doesn't match.
	mapIdx := strings.Index(out, "\n## Diff map\n")
	idxIdx := strings.Index(out, "\n## Source index\n")
	if mapIdx < 0 || idxIdx < 0 || mapIdx > idxIdx {
		t.Errorf("Diff map must precede Source index; got positions %d, %d", mapIdx, idxIdx)
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

	showFn := func(workdir, sha, path string) (string, int, error) {
		return padTo(path, 600), 600, nil
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
	if indexRow(out, "a.ts") != "| a.ts | embedded | * | 600 |  |" || indexRow(out, "b.ts") != "| b.ts | embedded | * | 600 |  |" {
		t.Errorf("expected both files embedded when aggregate cap disabled; got:\n%s", out)
	}
	if strings.Contains(out, "aggregate bundle cap") {
		t.Error("aggregate-cap reason leaked into output despite MaxTotalSourceBytes=0")
	}
}

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

// TestBuild_SourceIndexBundleLineIsAccurate is the guard on the two-pass line
// arithmetic: the `Bundle line` column must land exactly on each file's
// `### <path>` heading. An off-by-one here is worse than no column at all — a
// specialist would Read a confidently wrong offset instead of grepping.
func TestBuild_SourceIndexBundleLineIsAccurate(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","big.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	showFn := func(workdir, sha, path string) (string, int, error) {
		if path == "big.ts" {
			body := strings.Repeat("// pad\n", 400) // over the per-file cap
			return body, len(body), nil
		}
		body := padTo(path, 300) + "\nsecond line\nthird line\n"
		return body, len(body), nil
	}

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 1024,
		GitShowFn:      showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	lines := strings.Split(out, "\n")
	// BUNDLE_LINES must equal the bundle's real height. A trailing newline
	// terminates the last line rather than starting a new one, so Split's
	// trailing empty element is not a line.
	wantTotal := len(lines)
	if strings.HasSuffix(out, "\n") {
		wantTotal--
	}
	if !strings.Contains(out, fmt.Sprintf("- BUNDLE_LINES: %d ", wantTotal)) {
		t.Errorf("BUNDLE_LINES does not match actual height %d; header line was:\n%s",
			wantTotal, firstLineContaining(out, "BUNDLE_LINES"))
	}
	if strings.Contains(out, bundleLinesPlaceholder) {
		t.Error("BUNDLE_LINES placeholder was not substituted")
	}

	// Every non-omitted row's Bundle line must point at its own `### <path>`.
	for _, path := range []string{"a.ts", "b.ts"} {
		n := bundleLineFor(t, out, path)
		if n < 1 || n > len(lines) {
			t.Fatalf("%s: Bundle line %d out of range (bundle has %d lines)", path, n, len(lines))
		}
		if got := lines[n-1]; got != "### "+path {
			t.Errorf("%s: Bundle line %d points at %q, want %q", path, n, got, "### "+path)
		}
	}
}

// TestBuild_ReplicationGroups verifies byte-identical changed files collapse to
// one embedded body, are charged to the aggregate budget once, and resolve to
// the representative's offset so a specialist that jumps there finds content.
func TestBuild_ReplicationGroups(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["app1/logs.tf","app2/logs.tf","app3/logs.tf","other.tf"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	shared := "resource \"aws_cloudwatch\" \"x\" {\n  name = \"y\"\n}\n"
	showFn := func(workdir, sha, path string) (string, int, error) {
		if path == "other.tf" {
			return "variable \"z\" {}\n", len("variable \"z\" {}\n"), nil
		}
		return shared, len(shared), nil
	}

	out, err := Build(Input{
		ReviewTmpDir: dir,
		HeadSHA:      "deadbeef",
		PRNumber:     1,
		Owner:        "o",
		Repo:         "r",
		RubricPath:   rubric,
		// Aggregate cap fits ONE copy of shared plus other.tf, but not three
		// copies — so if dedup regressed, the later apps would be omitted and
		// this test fails loudly rather than merely wasting budget.
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: len(shared) + 32,
		GitShowFn:           showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !strings.Contains(out, "## Replication groups") {
		t.Fatal("missing `## Replication groups` section")
	}
	if !strings.Contains(out, "| app1/logs.tf | "+strconv.Itoa(len(shared))+" | 2: app2/logs.tf, app3/logs.tf |") {
		t.Errorf("replication group row missing or wrong; got:\n%s", out)
	}
	if indexRow(out, "app1/logs.tf") != "| app1/logs.tf | embedded | * | "+strconv.Itoa(len(shared))+" |  |" {
		t.Errorf("representative should be embedded; got %q", indexRow(out, "app1/logs.tf"))
	}
	for _, p := range []string{"app2/logs.tf", "app3/logs.tf"} {
		want := "| " + p + " | duplicate | * | " + strconv.Itoa(len(shared)) + " | identical to `app1/logs.tf` |"
		if indexRow(out, p) != want {
			t.Errorf("%s: got %q, want %q", p, indexRow(out, p), want)
		}
		// A duplicate's Bundle line must resolve to the representative's body,
		// not to its own pointer stub — otherwise the jump lands on nothing.
		if bundleLineFor(t, out, p) != bundleLineFor(t, out, "app1/logs.tf") {
			t.Errorf("%s: Bundle line should point at the representative's body", p)
		}
	}
	// The body appears exactly once.
	if n := strings.Count(out, `resource "aws_cloudwatch"`); n != 1 {
		t.Errorf("shared body embedded %d times, want exactly 1", n)
	}
	// Distinct content is unaffected by grouping.
	if indexRow(out, "other.tf") == "" || strings.Contains(indexRow(out, "other.tf"), "duplicate") {
		t.Errorf("other.tf must not be grouped; got %q", indexRow(out, "other.tf"))
	}
}

// bundleLineFor extracts the `Bundle line` value from path's Source index row.
func bundleLineFor(t *testing.T, out, path string) int {
	t.Helper()
	start := strings.Index(out, "\n## Source index\n")
	if start < 0 {
		t.Fatal("no Source index section")
	}
	for _, line := range strings.Split(out[start:], "\n") {
		if !strings.HasPrefix(line, "| "+path+" | ") {
			continue
		}
		cols := strings.Split(line, " | ")
		if len(cols) < 3 {
			t.Fatalf("malformed index row for %s: %q", path, line)
		}
		n, err := strconv.Atoi(strings.TrimSpace(cols[2]))
		if err != nil {
			t.Fatalf("non-numeric Bundle line for %s: %q", path, cols[2])
		}
		return n
	}
	t.Fatalf("no Source index row for %s", path)
	return 0
}

func firstLineContaining(out, needle string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// padTo returns exactly n bytes of filler unique to path. The cap tests must use
// distinct content per file: byte-identical files now collapse into a
// replication group, which claims them before either cap is consulted, so
// shared filler would silently stop exercising the cap logic under test.
func padTo(path string, n int) string {
	return (path + ":" + strings.Repeat("x", n))[:n]
}

// indexRow returns path's `## Source index` row with the `Bundle line` column
// replaced by `*`. That column is a function of every preceding section's
// height, so pinning it in an assertion would make unrelated wording edits
// anywhere above the index fail these tests for no reason. Returns "" when the
// path has no row.
//
// The search starts at the `## Source index` heading because `## Diff map`
// renders earlier and its rows share the leading `| <path> |` shape — scanning
// from the top of the bundle silently returns the diff-map row instead.
func indexRow(out, path string) string {
	start := strings.Index(out, "\n## Source index\n")
	if start < 0 {
		return ""
	}
	for _, line := range strings.Split(out[start:], "\n") {
		if !strings.HasPrefix(line, "| "+path+" | ") {
			continue
		}
		cols := strings.Split(line, " | ")
		if len(cols) > 2 {
			cols[2] = "*"
		}
		return strings.Join(cols, " | ")
	}
	return ""
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

// TestBuild_SourceStartCoversIndex pins the invariant the SOURCE_START header
// exists to guarantee: a Read of lines 0..SOURCE_START holds the *entire*
// Source index, rows included, and every file the index points at lives at or
// below that line.
//
// Transcript 2eb9ac1c (Aug 2026) is why this is a test and not a comment. The
// spawn prompt hard-coded `limit: 200` while the index's first data row landed
// at 213, so specialists received the index heading and zero rows, then paged
// all 1,492 lines linearly instead of jumping. Asserting the relationship
// rather than the literal numbers keeps this honest as the header grows.
func TestBuild_SourceStartCoversIndex(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["a.ts","b.ts","c.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `["security"]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	// Distinct bodies per file: identical content would collapse them into a
	// replication group, whose table also emits `| <path> | ` rows and would
	// mask whether the Source index itself lands inside the first page.
	showFn := func(workdir, sha, path string) (string, int, error) {
		content := "export const " + strings.TrimSuffix(path, ".ts") + " = 1;\n" +
			strings.Repeat("// "+path+"\n", 40)
		return content, len(content), nil
	}

	out, err := Build(Input{
		ReviewTmpDir:   dir,
		HeadSHA:        "deadbeef",
		PRNumber:       1,
		Owner:          "o",
		Repo:           "r",
		RubricPath:     rubric,
		MaxSourceBytes: 64 * 1024,
		GitShowFn:      showFn,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	srcStart := headerInt(t, out, "- SOURCE_START: ")
	if srcStart <= 0 {
		t.Fatalf("SOURCE_START not emitted or non-positive: %d", srcStart)
	}
	if strings.Contains(out, sourceStartPlaceholder) {
		t.Error("SOURCE_START placeholder was never substituted")
	}
	// The spawn prompt's read plan stops at SOURCE_START and says nothing about
	// what follows, so this header line is the specialist's only instruction to
	// jump via the Source index instead of paging on (transcript 2eb9ac1c paged
	// three specialists through 1,492 lines, 78s on the critical path).
	if !strings.Contains(out, "via the Source index's `Bundle line` column rather than by paging") {
		t.Error("SOURCE_START header lost its don't-page instruction")
	}

	lines := strings.Split(out, "\n")
	lineOf := func(prefix string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return i + 1 // Read offsets are 1-indexed
			}
		}
		return -1
	}

	idxLine := lineOf("## Source index")
	if idxLine < 0 {
		t.Fatal("no `## Source index` section emitted")
	}
	if idxLine >= srcStart {
		t.Errorf("Source index starts at line %d, at or past SOURCE_START %d", idxLine, srcStart)
	}

	// Every index row must fall inside the first page, or the jump table is
	// truncated exactly the way it was in 2eb9ac1c. Rows are located *after*
	// the index heading so an identically-shaped row elsewhere in the bundle
	// (the replication-groups table) can't satisfy the assertion by accident.
	for _, path := range []string{"a.ts", "b.ts", "c.ts"} {
		row := -1
		for i := idxLine; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "| "+path+" | ") {
				row = i + 1
				break
			}
		}
		if row < 0 {
			t.Errorf("%s missing from Source index", path)
			continue
		}
		if row > srcStart {
			t.Errorf("%s row at line %d falls past SOURCE_START %d — first page would truncate the index", path, row, srcStart)
		}
	}

	// And the content SOURCE_START points at is genuinely the source section.
	srcHeading := lineOf("## Source at HEAD")
	if srcHeading < 0 {
		t.Fatal("no `## Source at HEAD` section emitted")
	}
	if srcStart > srcHeading {
		t.Errorf("SOURCE_START %d overshoots the source heading at line %d", srcStart, srcHeading)
	}
}

// headerInt pulls an integer-valued bundle header field out of the rendered
// bundle, e.g. "- SOURCE_START: 213 — first line of ...".
func headerInt(t *testing.T, out, prefix string) int {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		rest := strings.TrimPrefix(l, prefix)
		if sp := strings.IndexAny(rest, " \t"); sp > 0 {
			rest = rest[:sp]
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			t.Fatalf("header %q not an integer: %q (%v)", prefix, rest, err)
		}
		return n
	}
	return -1
}
