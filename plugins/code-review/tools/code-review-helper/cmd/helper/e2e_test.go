package main

import (
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
