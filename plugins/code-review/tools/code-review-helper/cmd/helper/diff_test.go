package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inlineDiff is a 6-line synthetic unified diff: one file, one hunk, two
// additions on top of one context line and one removal. Shaped to match the
// idiom in internal/diff/parse_test.go.
const inlineDiff = `diff --git a/src/foo.ts b/src/foo.ts
index abc..def 100644
--- a/src/foo.ts
+++ b/src/foo.ts
@@ -10,3 +10,4 @@ ctx
 ctx
-removed
+added
+added
 ctx
`

func TestRunDiff_MissingRequiredFlags(t *testing.T) {
	err := runDiff([]string{"--in", "-"})
	if err == nil {
		t.Fatal("expected error for missing --out-changed-files / --out-valid-lines")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required flags; got %v", err)
	}
}

func TestRunDiff_ReadsFromFile(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.diff")
	if err := os.WriteFile(inPath, []byte(inlineDiff), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	changedPath := filepath.Join(dir, "changed.json")
	validPath := filepath.Join(dir, "valid.json")

	if err := runDiff([]string{
		"--in", inPath,
		"--out-changed-files", changedPath,
		"--out-valid-lines", validPath,
	}); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	var changed []string
	readJSON(t, changedPath, &changed)
	if len(changed) != 1 || changed[0] != "src/foo.ts" {
		t.Errorf("changed files = %v, want [src/foo.ts]", changed)
	}

	var valid map[string]json.RawMessage
	readJSON(t, validPath, &valid)
	if _, ok := valid["src/foo.ts"]; !ok {
		t.Errorf("valid-lines missing src/foo.ts: %v", valid)
	}
}

func TestRunDiff_ReadsFromStdin(t *testing.T) {
	dir := t.TempDir()
	changedPath := filepath.Join(dir, "changed.json")
	validPath := filepath.Join(dir, "valid.json")

	withStdin(t, inlineDiff)

	if err := runDiff([]string{
		"--in", "-",
		"--out-changed-files", changedPath,
		"--out-valid-lines", validPath,
	}); err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	var changed []string
	readJSON(t, changedPath, &changed)
	if len(changed) != 1 || changed[0] != "src/foo.ts" {
		t.Errorf("changed files = %v, want [src/foo.ts]", changed)
	}
}

func TestRunDiff_MissingInputFile(t *testing.T) {
	dir := t.TempDir()
	err := runDiff([]string{
		"--in", filepath.Join(dir, "does-not-exist.diff"),
		"--out-changed-files", filepath.Join(dir, "changed.json"),
		"--out-valid-lines", filepath.Join(dir, "valid.json"),
	})
	if err == nil {
		t.Fatal("expected error for missing input file")
	}
	if !strings.Contains(err.Error(), "open --in") {
		t.Errorf("error should be wrapped 'open --in'; got %v", err)
	}
}

// withStdin is shared with bundle_test.go (same package).
func withStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := io.WriteString(w, data); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	w.Close()

	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = saved
		r.Close()
	})
}
