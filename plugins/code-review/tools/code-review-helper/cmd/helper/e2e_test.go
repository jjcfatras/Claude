package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGoldens lets developers refresh the byte-for-byte golden files when
// they intend to change behavior. Run with: `go test ./cmd/helper -update`.
var updateGoldens = flag.Bool("update", false, "rewrite golden files instead of comparing")

// fixtures table-drives the e2e cases. Each entry mirrors a real OSS PR. To
// add a new case: drop a unified diff in testdata/diffs/, populate per-role
// JSON in testdata/findings/<id>/, supply prior-issues/<id>.json, and add the
// row here. Run `go test -update` once to capture the initial golden.
var fixtures = []struct {
	id    string
	roles string
}{
	{id: "k8s-138754", roles: "security,errors,perf,quality"},
	{id: "nextjs-93491", roles: "security,typescript,react,errors,perf,quality"},
	{id: "prisma-29514", roles: "security,quality,errors,perf"},
}

// TestE2E_FinalizePipeline runs the helper's finalize entrypoint against each
// fixture and compares the three output files (consolidated.json, payload.json,
// fallback.md) byte-for-byte against the captured goldens. The goldens encode
// the full deterministic pipeline behavior (positional + semantic dedup, prior-
// review filter, gate, snap/classify, payload + fallback rendering); a single
// regression anywhere in that chain shows up as a textual diff here.
func TestE2E_FinalizePipeline(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	for _, fx := range fixtures {
		t.Run(fx.id, func(t *testing.T) {
			tdRoot := filepath.Join(repoRoot, "testdata")
			outDir := t.TempDir()

			argv := []string{
				"--diff", filepath.Join(tdRoot, "diffs", fx.id+".diff"),
				"--findings-dir", filepath.Join(tdRoot, "findings", fx.id),
				"--prior-issues", filepath.Join(tdRoot, "prior-issues", fx.id+".json"),
				"--head-sha", "0000000000000000000000000000000000000000",
				"--owner", "test-owner",
				"--repo", "test-repo",
				"--pr-number", "1",
				"--expected-roles", fx.roles,
				"--out-consolidated", filepath.Join(outDir, "consolidated.json"),
				"--out-payload", filepath.Join(outDir, "payload.json"),
				"--out-pending-payload", filepath.Join(outDir, "payload-pending.json"),
				"--out-body", filepath.Join(outDir, "payload-body.json"),
				"--out-fallback", filepath.Join(outDir, "fallback.md"),
			}
			if err := runFinalize(argv); err != nil {
				t.Fatalf("runFinalize: %v", err)
			}

			goldenDir := filepath.Join(tdRoot, "golden", fx.id)
			for _, name := range []string{"consolidated.json", "payload.json", "payload-pending.json", "payload-body.json", "fallback.md"} {
				gotPath := filepath.Join(outDir, name)
				wantPath := filepath.Join(goldenDir, name)
				got, err := os.ReadFile(gotPath)
				if err != nil {
					t.Fatalf("read produced %s: %v", name, err)
				}
				if *updateGoldens {
					if err := os.WriteFile(wantPath, got, 0o644); err != nil {
						t.Fatalf("update golden %s: %v", name, err)
					}
					continue
				}
				want, err := os.ReadFile(wantPath)
				if err != nil {
					t.Fatalf("read golden %s: %v (run `go test -update` to seed)", name, err)
				}
				if string(got) != string(want) {
					t.Errorf("%s differs from golden — first diverging line:\n%s",
						name, firstDivergence(string(want), string(got)))
				}
			}
		})
	}
}

// TestE2E_SpawnBatch covers all four --kind values against a single 5-member
// roster fixture. Goldens encode the byte-for-byte markdown the lead echoes;
// any rendering regression shows as a textual diff. The fixture's assignment
// IDs are deliberately non-sequential (7,8,9,10,11) to defend against
// accidental role-by-position substitution. The fixture's changed-files.json
// has ≤ 50 entries so the safety-monitor budget stays at the 240 s floor.
func TestE2E_SpawnBatch(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	tdDir := filepath.Join(repoRoot, "testdata", "spawn-batch", "sample-roster")
	rosterPath := filepath.Join(tdDir, "roster.json")
	assignmentsPath := filepath.Join(tdDir, "assignments.json")
	goldenDir := filepath.Join(tdDir, "golden")

	kinds := []string{"tasks", "agents", "finalize", "shutdown"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			outDir := t.TempDir()
			outPath := filepath.Join(outDir, kind+".md")

			argv := []string{
				"--kind", kind,
				"--roster", rosterPath,
				"--out", outPath,
			}
			if kind == "agents" {
				argv = append(argv,
					"--assignments-file", assignmentsPath,
					"--review-tmpdir", tdDir, // points at fixture changed-files.json
					"--owner", "test-owner",
					"--repo", "test-repo",
					"--pr-number", "1337",
				)
			}
			if err := runSpawnBatch(argv); err != nil {
				t.Fatalf("runSpawnBatch: %v", err)
			}

			gotPath := outPath
			wantPath := filepath.Join(goldenDir, kind+".md")
			got, err := os.ReadFile(gotPath)
			if err != nil {
				t.Fatalf("read produced %s: %v", kind, err)
			}
			if *updateGoldens {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatalf("mkdir golden: %v", err)
				}
				if err := os.WriteFile(wantPath, got, 0o644); err != nil {
					t.Fatalf("update golden %s: %v", kind, err)
				}
				return
			}
			want, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run `go test -update` to seed)", kind, err)
			}
			if string(got) != string(want) {
				t.Errorf("%s.md differs from golden — first diverging line:\n%s",
					kind, firstDivergence(string(want), string(got)))
			}
		})
	}
}

// TestE2E_SpawnBatchAgentsScanBudget pins the workload-scaled Monitor sleep
// budget against two boundary fixtures: 176 files (above shoulder, below cap
// → 492 s) and 300 files (clamped to the 540 s cap).
func TestE2E_SpawnBatchAgentsScanBudget(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	cases := []struct {
		fixture          string
		wantSleepSeconds string // substring assertion against Monitor line
	}{
		{fixture: "sample-roster-large", wantSleepSeconds: "sleep 492;"},
		{fixture: "sample-roster-huge", wantSleepSeconds: "sleep 540;"},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			tdDir := filepath.Join(repoRoot, "testdata", "spawn-batch", c.fixture)
			outDir := t.TempDir()
			outPath := filepath.Join(outDir, "agents.md")

			argv := []string{
				"--kind", "agents",
				"--roster", filepath.Join(tdDir, "roster.json"),
				"--assignments-file", filepath.Join(tdDir, "assignments.json"),
				"--review-tmpdir", tdDir,
				"--owner", "test-owner",
				"--repo", "test-repo",
				"--pr-number", "1337",
				"--out", outPath,
			}
			if err := runSpawnBatch(argv); err != nil {
				t.Fatalf("runSpawnBatch: %v", err)
			}
			body, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read produced agents.md: %v", err)
			}
			if !strings.Contains(string(body), c.wantSleepSeconds) {
				t.Errorf("expected Monitor line to contain %q for fixture %s; got:\n%s",
					c.wantSleepSeconds, c.fixture, string(body))
			}
		})
	}
}

// TestE2E_FinalizeSubsetFilter pins the --include-finding-ids behavior:
// payload reflects the post-filter subset while consolidated.json remains the
// pre-filter audit log. Uses the k8s-138754 fixture whose IDs are sec-1,
// err-2, qual-2.
func TestE2E_FinalizeSubsetFilter(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	tdRoot := filepath.Join(repoRoot, "testdata")
	fx := "k8s-138754"

	t.Run("include-keeps-only-named-id", func(t *testing.T) {
		outDir := t.TempDir()
		argv := []string{
			"--diff", filepath.Join(tdRoot, "diffs", fx+".diff"),
			"--findings-dir", filepath.Join(tdRoot, "findings", fx),
			"--prior-issues", filepath.Join(tdRoot, "prior-issues", fx+".json"),
			"--head-sha", "0000000000000000000000000000000000000000",
			"--owner", "test-owner",
			"--repo", "test-repo",
			"--pr-number", "1",
			"--expected-roles", "security,errors,perf,quality",
			"--include-finding-ids", "err-2",
			"--out-consolidated", filepath.Join(outDir, "consolidated.json"),
			"--out-payload", filepath.Join(outDir, "payload.json"),
			"--out-pending-payload", filepath.Join(outDir, "payload-pending.json"),
			"--out-body", filepath.Join(outDir, "payload-body.json"),
			"--out-fallback", filepath.Join(outDir, "fallback.md"),
		}
		if err := runFinalize(argv); err != nil {
			t.Fatalf("runFinalize: %v", err)
		}

		// consolidated.json: PostingFilter recorded; pre-filter findings intact.
		var cons struct {
			InlineEligible []struct {
				ID string `json:"id"`
			} `json:"inline_eligible"`
			SummaryOnly []struct {
				ID string `json:"id"`
			} `json:"summary_only"`
			PostingFilter *struct {
				IncludeIDs []string `json:"include_ids"`
				ExcludeIDs []string `json:"exclude_ids"`
			} `json:"posting_filter"`
		}
		readJSON(t, filepath.Join(outDir, "consolidated.json"), &cons)

		if cons.PostingFilter == nil {
			t.Fatal("consolidated.json missing posting_filter")
		}
		if !equalStringSet(cons.PostingFilter.IncludeIDs, []string{"err-2"}) {
			t.Errorf("posting_filter.include_ids = %v, want [err-2]", cons.PostingFilter.IncludeIDs)
		}
		if len(cons.PostingFilter.ExcludeIDs) != 0 {
			t.Errorf("posting_filter.exclude_ids should be empty, got %v", cons.PostingFilter.ExcludeIDs)
		}
		// Pre-filter audit log still carries every classified finding.
		preFilter := append([]string{}, idsOf(cons.InlineEligible)...)
		preFilter = append(preFilter, idsOf(cons.SummaryOnly)...)
		for _, want := range []string{"sec-1", "err-2", "qual-2"} {
			if !contains(preFilter, want) {
				t.Errorf("consolidated.json pre-filter buckets missing %s; got %v", want, preFilter)
			}
		}

		// payload.json: only err-2 survives the filter. The renderer doesn't
		// embed the finding ID in the comment body, so len(Comments) == 1 is
		// the contract assertion — the source of truth for "which finding got
		// posted" is consolidated.posting_filter.include_ids, asserted above.
		var payload struct {
			Comments []struct {
				Body string `json:"body"`
			} `json:"comments"`
		}
		readJSON(t, filepath.Join(outDir, "payload.json"), &payload)
		if len(payload.Comments) != 1 {
			t.Fatalf("payload.json comments = %d, want 1", len(payload.Comments))
		}
	})

	t.Run("unknown-id-is-hard-error", func(t *testing.T) {
		outDir := t.TempDir()
		argv := []string{
			"--diff", filepath.Join(tdRoot, "diffs", fx+".diff"),
			"--findings-dir", filepath.Join(tdRoot, "findings", fx),
			"--prior-issues", filepath.Join(tdRoot, "prior-issues", fx+".json"),
			"--head-sha", "0000000000000000000000000000000000000000",
			"--owner", "test-owner",
			"--repo", "test-repo",
			"--pr-number", "1",
			"--expected-roles", "security,errors,perf,quality",
			"--include-finding-ids", "does-not-exist",
			"--out-consolidated", filepath.Join(outDir, "consolidated.json"),
			"--out-payload", filepath.Join(outDir, "payload.json"),
			"--out-pending-payload", filepath.Join(outDir, "payload-pending.json"),
			"--out-body", filepath.Join(outDir, "payload-body.json"),
			"--out-fallback", filepath.Join(outDir, "fallback.md"),
		}
		err := runFinalize(argv)
		if err == nil {
			t.Fatal("expected runFinalize to return error for unknown finding ID, got nil")
		}
		if !strings.Contains(err.Error(), "unknown id") {
			t.Errorf("error should mention 'unknown id'; got: %v", err)
		}
	})
}

// readJSON unmarshals a file into v, failing the test on any error.
func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, b)
	}
}

func idsOf(items []struct {
	ID string `json:"id"`
}) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// firstDivergence shows the first line where want and got differ. Goldens are
// large enough that a full diff drowns the signal; the first divergence is
// almost always sufficient to locate the regression.
func firstDivergence(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return "  line " + itoa(i+1) + ":\n  want: " + w + "\n  got:  " + g
		}
	}
	return "(no line-level difference found; trailing newlines may differ)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
