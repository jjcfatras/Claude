package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// spawnArgs returns a complete valid flag set for runSpawnManifest pointing at
// rosterPath, writing the manifest to dir/out.json.
func spawnArgs(dir, rosterPath string) []string {
	return []string{
		"--roster", rosterPath,
		"--review-tmpdir", "/tmp/x",
		"--head-sha", "abc",
		"--pr-number", "1",
		"--owner", "o",
		"--repo", "r",
		"--repo-root", "/r",
		"--out", filepath.Join(dir, "out.json"),
	}
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
	err := runSpawnManifest(spawnArgs(dir, filepath.Join(dir, "missing.json")))
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
	err := runSpawnManifest(spawnArgs(dir, p))
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
	err := runSpawnManifest(spawnArgs(dir, p))
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
	err := runSpawnManifest(spawnArgs(dir, p))
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
	if e.SubagentType != "review-pr:claude-md" {
		t.Errorf("subagent_type=%q, want review-pr:claude-md", e.SubagentType)
	}
	if e.Description != "claude-md specialist scan" {
		t.Errorf("description=%q, want claude-md specialist scan", e.Description)
	}
	// Stated once, then referenced as $REVIEW_TMPDIR: transcript 70510fd5 spelled
	// it out four times per prompt, and the orchestrator re-types every prompt
	// before any specialist starts.
	if n := strings.Count(e.Prompt, "/tmp/pr-review-99-1"); n != 1 {
		t.Errorf("prompt spells out the tmpdir %d times, want 1; got: %s", n, e.Prompt)
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
	// The manifest is emitted longest-running-role first rather than in roster
	// order. On a harness that starts each agent as its block streams, the role
	// that defines the run's critical path must not be the last to start (on
	// transcript db83dbda, `infra` spawned 5th and began 26s after the first
	// specialist, then ran 73s past every peer). See spawnRank.
	wantOrder := []string{"infra", "security", "typescript", "quality", "errors", "react", "perf", "claude-md"}
	for i, role := range wantOrder {
		e := entries[i]
		if e.SubagentType != "review-pr:"+role {
			t.Errorf("[%d] subagent_type=%q, want review-pr:%s", i, e.SubagentType, role)
		}
		if e.Description != role+" specialist scan" {
			t.Errorf("[%d] description=%q, want %q", i, e.Description, role+" specialist scan")
		}
	}

	// Reordering must never drop or duplicate a roster entry.
	seen := map[string]int{}
	for _, e := range entries {
		seen[strings.TrimPrefix(e.SubagentType, "review-pr:")]++
	}
	for _, role := range roles {
		if seen[role] != 1 {
			t.Errorf("role %s appears %d times in the manifest, want exactly 1", role, seen[role])
		}
	}
}

// TestRunSpawnManifest_UnrankedRolesKeepRosterOrderLast guards the sort's
// fallback: a role added to the roster but not to spawnRank must still be
// emitted exactly once, after the ranked roles, in its original relative order.
func TestRunSpawnManifest_UnrankedRolesKeepRosterOrderLast(t *testing.T) {
	dir := t.TempDir()
	rp := writeRoster(t, dir, "zeta-new", "perf", "alpha-new", "infra")
	out := runSpawnManifestOK(t, dir, rp)

	var entries []spawnEntry
	readJSON(t, out, &entries)
	var got []string
	for _, e := range entries {
		got = append(got, strings.TrimPrefix(e.SubagentType, "review-pr:"))
	}
	want := []string{"infra", "perf", "zeta-new", "alpha-new"}
	if !slices.Equal(got, want) {
		t.Errorf("spawn order = %v, want %v", got, want)
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
		"Your $REVIEW_TMPDIR is /tmp/pr-review-99-1.",
		"$REVIEW_TMPDIR/spawn-context.md",
		"$REVIEW_TMPDIR/rubric.md",
		"$REVIEW_TMPDIR/pr-99.diff",
		// SOURCE_START, not BUNDLE_LINES: the prompt's paging anchor is the end
		// of the orientation payload, not the end of the file. A specialist
		// reads through SOURCE_START and then jumps via the Source index, so it
		// never needs the total line count to know when to stop.
		"SOURCE_START",
		// The one value a specialist may need before its first Read lands.
		"HEAD_SHA: deadbeefcafef00d",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(p, s) {
			t.Errorf("prompt missing %q; full prompt:\n%s", s, p)
		}
	}

	// The orchestrator pays for this prompt once per roster entry, so anything
	// identical across roles and runs belongs in agents/*.md (a system prompt,
	// which is free) or in the bundle the specialist Reads seconds later. These
	// substrings all moved out; if one reappears here, the per-block streaming
	// cost that transcript db83dbda measured at ~6.4s comes back with it.
	forbidden := map[string]string{
		"REPO_ROOT: ":     "already in the bundle's `## Per-PR` header",
		"REVIEW_TMPDIR: ": "already in the bundle's `## Per-PR` header",
		"PR: #":           "already in the bundle's `## Per-PR` header",
		"ugrep":           "moved to agents/*.md `## Environment`",
		"suggested_fix":   "defined in the rubric the specialist Reads",
		"startLine":       "defined in the rubric the specialist Reads",
		"/findings/":      "each agents/*.md `## Output` names its own findings path",
	}
	for s, why := range forbidden {
		if strings.Contains(p, s) {
			t.Errorf("prompt re-introduced %q — %s; full prompt:\n%s", s, why, p)
		}
	}
}

// TestAgentFilesCarryEnvironmentNotes is the other half of the prompt trim: the
// guardrail notes are only free if they actually reached every specialist's
// system prompt. Without this, deleting them from the prompt template would
// silently leave every role unwarned — which is how a specialist lost a turn to
// the since-retired `no-cd-chaining` hook in transcript db83dbda.
func TestAgentFilesCarryEnvironmentNotes(t *testing.T) {
	agentsDir := filepath.Join("..", "..", "..", "..", "agents")
	roles := []string{"security", "quality", "errors", "perf", "typescript", "react", "infra", "claude-md"}
	wantNotes := []string{
		"may not be at `HEAD_SHA`",
		"ugrep in BRE mode",
	}
	for _, role := range roles {
		path := filepath.Join(agentsDir, role+".md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(raw)
		if want := "$REVIEW_TMPDIR/findings/" + role + ".json"; !strings.Contains(body, want) {
			t.Errorf("%s.md never names %s, and the spawn prompt no longer carries it", role, want)
		}
		if !strings.Contains(body, "\n## Environment\n") {
			t.Errorf("%s.md has no `## Environment` section", role)
			continue
		}
		for _, note := range wantNotes {
			if !strings.Contains(body, note) {
				t.Errorf("%s.md `## Environment` is missing the %q guardrail note", role, note)
			}
		}
	}
}

// TestOrientationReads pins the page plan that replaced "Read up to
// SOURCE_START": every planned Read stays under maxReadBytes, and together they
// cover lines [0, SOURCE_START) exactly once, in order.
func TestOrientationReads(t *testing.T) {
	line := strings.Repeat("x", 999) + "\n" // 1,000 bytes
	big := "- SOURCE_START: 120 — first line\n" + strings.Repeat(line, 200)
	cases := []struct {
		name   string
		bundle string
		end    int
		reads  int
	}{
		{"small PR is one read", "# h\n- SOURCE_START: 3 — x\nindex\nsrc\nsrc\n", 3, 1},
		{"large orientation is split by bytes", big, 120, (120*1000 + maxReadBytes - 1) / maxReadBytes},
		{"no source section covers the whole file", "a\nb\nc\n", 3, 1},
		{"oversized single line still advances", strings.Repeat("y", maxReadBytes*2) + "\n- SOURCE_START: 2 — x\nsrc\n", 2, 2},
	}
	re := regexp.MustCompile(`offset:(\d+) limit:(\d+)`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orientationReads(tc.bundle)
			lines := strings.SplitAfter(tc.bundle, "\n")
			next := 0
			ms := re.FindAllStringSubmatch(got, -1)
			for _, m := range ms {
				off, _ := strconv.Atoi(m[1])
				lim, _ := strconv.Atoi(m[2])
				if off != next {
					t.Fatalf("read starts at %d, want %d: %s", off, next, got)
				}
				size := len(strings.Join(lines[off:off+lim], ""))
				if lim > 1 && size > maxReadBytes {
					t.Errorf("read offset:%d limit:%d is %d bytes > %d", off, lim, size, maxReadBytes)
				}
				next = off + lim
			}
			if next != tc.end || len(ms) != tc.reads {
				t.Errorf("covered [0,%d) in %d reads, want [0,%d) in %d: %s", next, len(ms), tc.end, tc.reads, got)
			}
		})
	}
}

func TestBuildSpawnEntries_UsesBundlePlan(t *testing.T) {
	entries, err := buildSpawnEntries([]string{"security"}, spawnSubs{reviewTmpDir: "/t", headSHA: "abc", prNumber: 1, bundle: "a\n- SOURCE_START: 2 — x\nsrc\n"})
	if err != nil {
		t.Fatal(err)
	}
	p := entries[0].Prompt
	if !strings.Contains(p, "(offset:0 limit:2)") || strings.Contains(p, "limit:400") || strings.Contains(p, "{{") {
		t.Errorf("prompt did not use the bundle's page plan: %s", p)
	}
}

func TestDiffGuidance(t *testing.T) {
	tests := []struct {
		name           string
		bytes, lines   int
		wantContains   string
		wantNotContain string
	}{
		{
			// The PR #1817 shape: all five specialists correctly ignored the
			// old absolute "never bare-Read the diff" rule at this size.
			name: "small diff says read it whole", bytes: 13456, lines: 224,
			wantContains: "13456 bytes / 224 lines, so Read it in ONE call",
			// The map is the wrong advice here and must not be mentioned.
			wantNotContain: "Diff map",
		},
		{
			name: "byte-oversize diff says use the map", bytes: 420_000, lines: 900,
			wantContains: "too big for one Read, so use the `## Diff map`",
		},
		{
			name: "line-oversize diff says use the map", bytes: 100_000, lines: 9_000,
			wantContains: "too big for one Read, so use the `## Diff map`",
		},
		{
			// Unmeasured (0) must fall back to the conservative branch rather
			// than claiming a 0-byte diff is safe to read whole.
			name: "unmeasured falls back to the map", bytes: 0, lines: 0,
			wantContains:   "use the `## Diff map`",
			wantNotContain: "ONE call",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffGuidance(tt.bytes, tt.lines)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("diffGuidance(%d, %d) = %q, want it to contain %q", tt.bytes, tt.lines, got, tt.wantContains)
			}
			if tt.wantNotContain != "" && strings.Contains(got, tt.wantNotContain) {
				t.Errorf("diffGuidance(%d, %d) = %q, should not contain %q", tt.bytes, tt.lines, got, tt.wantNotContain)
			}
		})
	}
}

// The rendered prompt must carry the measured guidance, not the placeholder.
func TestBuildSpawnEntries_SubstitutesDiffGuidance(t *testing.T) {
	entries, err := buildSpawnEntries([]string{"security"}, spawnSubs{
		reviewTmpDir: "/tmp/r", headSHA: "deadbeef", prNumber: 7,
		owner: "o", repo: "r", repoRoot: "/repo",
		diffBytes: 13456, diffLines: 224,
	})
	if err != nil {
		t.Fatalf("buildSpawnEntries: %v", err)
	}
	if strings.Contains(entries[0].Prompt, "{{") {
		t.Errorf("unsubstituted placeholder left in prompt: %s", entries[0].Prompt)
	}
	if !strings.Contains(entries[0].Prompt, "Read it in ONE call") {
		t.Errorf("prompt missing measured diff guidance: %s", entries[0].Prompt)
	}
}
