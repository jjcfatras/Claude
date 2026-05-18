package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBundleContext_MissingRequiredFlags(t *testing.T) {
	err := runBundleContext([]string{"--review-tmpdir", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required flags; got %v", err)
	}
}

func TestRunBundleContext_SummaryFromFile(t *testing.T) {
	dir := seedBundleFixture(t)
	summaryPath := filepath.Join(dir, "summary.txt")
	if err := os.WriteFile(summaryPath, []byte("This PR does a thing."), 0o644); err != nil {
		t.Fatalf("seed summary: %v", err)
	}

	if err := runBundleContext(append(commonBundleArgs(dir),
		"--summary-paragraph", summaryPath,
	)); err != nil {
		t.Fatalf("runBundleContext: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "spawn-context.md"))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if !strings.Contains(string(got), "This PR does a thing.") {
		t.Errorf("bundle missing summary; got:\n%s", got)
	}
}

func TestRunBundleContext_SummaryFromStdin(t *testing.T) {
	dir := seedBundleFixture(t)
	withStdin(t, "Stdin summary line.")

	if err := runBundleContext(append(commonBundleArgs(dir),
		"--summary-paragraph", "-",
	)); err != nil {
		t.Fatalf("runBundleContext: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "spawn-context.md"))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if !strings.Contains(string(got), "Stdin summary line.") {
		t.Errorf("bundle missing stdin summary; got:\n%s", got)
	}
}

func TestRunBundleContext_SummaryEmptyPlaceholder(t *testing.T) {
	dir := seedBundleFixture(t)
	if err := runBundleContext(commonBundleArgs(dir)); err != nil {
		t.Fatalf("runBundleContext: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spawn-context.md")); err != nil {
		t.Errorf("bundle not written: %v", err)
	}
}

func TestRunBundleContext_OutputToStdout(t *testing.T) {
	dir := seedBundleFixture(t)
	out := captureStdout(t)

	if err := runBundleContext(append(commonBundleArgs(dir),
		"--out", "-",
	)); err != nil {
		t.Fatalf("runBundleContext: %v", err)
	}

	captured := out()
	if !strings.Contains(captured, "# Code review spawn context") {
		t.Errorf("stdout missing bundle header; got:\n%s", captured)
	}
	if _, err := os.Stat(filepath.Join(dir, "spawn-context.md")); !os.IsNotExist(err) {
		t.Errorf("expected no file on disk, got err=%v", err)
	}
}

// seedBundleFixture writes the minimal JSON artifacts + rubric.md that
// bundle.Build expects in $REVIEW_TMPDIR. Mirrors internal/bundle/bundle_test.go.
func seedBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"changed-files.json":   `["a.ts"]`,
		"roster.json":          `["security"]`,
		"prior-issues.json":    `{"last_review_date":null,"last_review_commit":null,"issues":[]}`,
		"claude-md-files.json": `{}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "rubric.md"), []byte("# Rubric\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("seed rubric: %v", err)
	}
	return dir
}

// commonBundleArgs returns the always-required flag set with
// --max-source-bytes=0 to disable `git show` so the test is hermetic
// (no git binary needed, no repo context required).
func commonBundleArgs(dir string) []string {
	return []string{
		"--review-tmpdir", dir,
		"--head-sha", "abcdef0123456789",
		"--pr-number", "42",
		"--owner", "test-owner",
		"--repo", "test-repo",
		"--rubric", filepath.Join(dir, "rubric.md"),
		"--max-source-bytes", "0",
	}
}

// captureStdout is shared with diff_test.go (same package).
func captureStdout(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = saved
	})
	return func() string {
		w.Close()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read pipe: %v", err)
		}
		return string(b)
	}
}
