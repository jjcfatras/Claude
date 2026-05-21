package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRoster(t *testing.T, dir string, roles ...string) string {
	t.Helper()
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		parts = append(parts, "\""+r+"\"")
	}
	body := "[" + strings.Join(parts, ",") + "]"
	p := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("seed roster: %v", err)
	}
	return p
}

func runSpawnManifestOK(t *testing.T, dir, rosterPath string) string {
	t.Helper()
	out := filepath.Join(dir, "spawn-manifest.json")
	if err := runSpawnManifest([]string{
		"--roster", rosterPath,
		"--review-tmpdir", "/tmp/pr-review-99-1",
		"--head-sha", "deadbeefcafef00d",
		"--pr-number", "99",
		"--owner", "FS-Main",
		"--repo", "fairsquare",
		"--repo-root", "/work/repo",
		"--out", out,
	}); err != nil {
		t.Fatalf("runSpawnManifest: %v", err)
	}
	return out
}

func TestRunSpawnManifest_MissingRequiredFlags(t *testing.T) {
	err := runSpawnManifest([]string{"--roster", "x.json"})
	if err == nil {
		t.Fatal("expected error for missing required flags")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required flags; got %v", err)
	}
}

func TestRunSpawnManifest_BadRosterPath(t *testing.T) {
	dir := t.TempDir()
	err := runSpawnManifest([]string{
		"--roster", filepath.Join(dir, "missing.json"),
		"--review-tmpdir", "/tmp/x",
		"--head-sha", "abc",
		"--pr-number", "1",
		"--owner", "o",
		"--repo", "r",
		"--repo-root", "/r",
		"--out", filepath.Join(dir, "out.json"),
	})
	if err == nil {
		t.Fatal("expected error for missing roster file")
	}
	if !strings.Contains(err.Error(), "read --roster") {
		t.Errorf("error should be wrapped 'read --roster'; got %v", err)
	}
}

func TestRunSpawnManifest_MalformedRoster(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(p, []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := runSpawnManifest([]string{
		"--roster", p,
		"--review-tmpdir", "/tmp/x",
		"--head-sha", "abc",
		"--pr-number", "1",
		"--owner", "o",
		"--repo", "r",
		"--repo-root", "/r",
		"--out", filepath.Join(dir, "out.json"),
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse --roster") {
		t.Errorf("error should be wrapped 'parse --roster'; got %v", err)
	}
}

func TestRunSpawnManifest_EmptyRosterErrors(t *testing.T) {
	dir := t.TempDir()
	p := writeRoster(t, dir)
	err := runSpawnManifest([]string{
		"--roster", p,
		"--review-tmpdir", "/tmp/x",
		"--head-sha", "abc",
		"--pr-number", "1",
		"--owner", "o",
		"--repo", "r",
		"--repo-root", "/r",
		"--out", filepath.Join(dir, "out.json"),
	})
	if err == nil {
		t.Fatal("expected error for empty roster")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty; got %v", err)
	}
}

func TestRunSpawnManifest_EmptyRoleNameErrors(t *testing.T) {
	dir := t.TempDir()
	p := writeRoster(t, dir, "security", "")
	err := runSpawnManifest([]string{
		"--roster", p,
		"--review-tmpdir", "/tmp/x",
		"--head-sha", "abc",
		"--pr-number", "1",
		"--owner", "o",
		"--repo", "r",
		"--repo-root", "/r",
		"--out", filepath.Join(dir, "out.json"),
	})
	if err == nil {
		t.Fatal("expected error for empty role name")
	}
}

func TestRunSpawnManifest_SingleRoleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rp := writeRoster(t, dir, "claude-md")
	out := runSpawnManifestOK(t, dir, rp)

	var entries []spawnEntry
	readJSON(t, out, &entries)
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d, want 1", len(entries))
	}
	e := entries[0]
	if e.SubagentType != "code-review:claude-md" {
		t.Errorf("subagent_type=%q, want code-review:claude-md", e.SubagentType)
	}
	if e.Description != "claude-md specialist scan" {
		t.Errorf("description=%q, want claude-md specialist scan", e.Description)
	}
	if !strings.Contains(e.Prompt, "/tmp/pr-review-99-1/findings/claude-md.json") {
		t.Errorf("prompt missing per-role findings path; got: %s", e.Prompt)
	}
}

func TestRunSpawnManifest_AllRolesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	roles := []string{"security", "quality", "errors", "perf", "typescript", "react", "infra", "claude-md"}
	rp := writeRoster(t, dir, roles...)
	out := runSpawnManifestOK(t, dir, rp)

	var entries []spawnEntry
	readJSON(t, out, &entries)
	if len(entries) != len(roles) {
		t.Fatalf("len(entries)=%d, want %d", len(entries), len(roles))
	}
	for i, role := range roles {
		e := entries[i]
		if e.SubagentType != "code-review:"+role {
			t.Errorf("[%d] subagent_type=%q, want code-review:%s", i, e.SubagentType, role)
		}
		if e.Description != role+" specialist scan" {
			t.Errorf("[%d] description=%q, want %q", i, e.Description, role+" specialist scan")
		}
		if !strings.Contains(e.Prompt, "/findings/"+role+".json") {
			t.Errorf("[%d] prompt does not reference %s findings; got: %s", i, role, e.Prompt)
		}
	}
}

func TestRunSpawnManifest_AllPlaceholdersSubstituted(t *testing.T) {
	dir := t.TempDir()
	rp := writeRoster(t, dir, "security")
	out := runSpawnManifestOK(t, dir, rp)

	var entries []spawnEntry
	readJSON(t, out, &entries)
	p := entries[0].Prompt

	leftovers := []string{"{{ROLE}}", "{{TMP}}", "{{PR_NUMBER}}", "{{HEAD_SHA}}", "{{REPO_ROOT}}", "{{OWNER}}", "{{REPO}}"}
	for _, l := range leftovers {
		if strings.Contains(p, l) {
			t.Errorf("placeholder %s not substituted; prompt: %s", l, p)
		}
	}

	wantSubstrings := []string{
		"/tmp/pr-review-99-1/spawn-context.md",
		"/tmp/pr-review-99-1/rubric.md",
		"/tmp/pr-review-99-1/pr-99.diff",
		"/tmp/pr-review-99-1/findings/security.json",
		"HEAD_SHA: deadbeefcafef00d",
		"REPO_ROOT: /work/repo",
		"REVIEW_TMPDIR: /tmp/pr-review-99-1",
		"PR: #99 in FS-Main/fairsquare",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(p, s) {
			t.Errorf("prompt missing %q; full prompt:\n%s", s, p)
		}
	}
}
