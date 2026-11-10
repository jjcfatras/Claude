package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunRoster_MissingRequiredFlags(t *testing.T) {
	err := runRoster([]string{"--changed-files", "x.json"})
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required flags; got %v", err)
	}
}

func TestRunRoster_BadInputPath(t *testing.T) {
	dir := t.TempDir()
	err := runRoster([]string{
		"--changed-files", filepath.Join(dir, "does-not-exist.json"),
		"--repo-root", dir,
		"--out-claude-md-files", filepath.Join(dir, "cm.json"),
		"--out-roster", filepath.Join(dir, "roster.json"),
	})
	if err == nil {
		t.Fatal("expected error for missing input file")
	}
	if !strings.Contains(err.Error(), "read --changed-files") {
		t.Errorf("error should be wrapped 'read --changed-files'; got %v", err)
	}
}

func TestRunRoster_MalformedInputJSON(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := runRoster([]string{
		"--changed-files", inPath,
		"--repo-root", dir,
		"--out-claude-md-files", filepath.Join(dir, "cm.json"),
		"--out-roster", filepath.Join(dir, "roster.json"),
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse --changed-files") {
		t.Errorf("error should be wrapped 'parse --changed-files'; got %v", err)
	}
}

func TestRunRoster_HappyPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("root"), 0o644); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, []byte(`["src/app.tsx","db/migrations/0001.sql"]`), 0o644); err != nil {
		t.Fatalf("seed in: %v", err)
	}
	cmPath := filepath.Join(dir, "cm.json")
	rosterPath := filepath.Join(dir, "roster.json")

	if err := runRoster([]string{
		"--changed-files", inPath,
		"--repo-root", dir,
		"--out-claude-md-files", cmPath,
		"--out-roster", rosterPath,
	}); err != nil {
		t.Fatalf("runRoster: %v", err)
	}

	var cm []string
	readJSON(t, cmPath, &cm)
	if !reflect.DeepEqual(cm, []string{"CLAUDE.md"}) {
		t.Errorf("claude-md files = %v, want [CLAUDE.md]", cm)
	}

	var roster []string
	readJSON(t, rosterPath, &roster)
	want := []string{"security", "quality", "errors", "perf", "typescript", "react", "infra", "claude-md"}
	if !reflect.DeepEqual(roster, want) {
		t.Errorf("roster = %v, want %v", roster, want)
	}
}

func TestRunRoster_EmptyClaudeMdEmitsArrayNotNull(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, []byte(`["README.md"]`), 0o644); err != nil {
		t.Fatalf("seed in: %v", err)
	}
	cmPath := filepath.Join(dir, "cm.json")
	rosterPath := filepath.Join(dir, "roster.json")

	if err := runRoster([]string{
		"--changed-files", inPath,
		"--repo-root", dir,
		"--out-claude-md-files", cmPath,
		"--out-roster", rosterPath,
	}); err != nil {
		t.Fatalf("runRoster: %v", err)
	}

	raw, err := os.ReadFile(cmPath)
	if err != nil {
		t.Fatalf("read cm: %v", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "[]" {
		t.Errorf("claude-md output = %q, want \"[]\" (nil should coerce to empty array, not null)", trimmed)
	}
}
