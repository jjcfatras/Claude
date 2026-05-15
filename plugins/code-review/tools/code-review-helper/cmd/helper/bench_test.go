package main

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkFinalizePipeline measures the wall-clock cost of a full finalize
// invocation per fixture. The pipeline is dedup → gate → snap → render +
// payload assembly + JSON marshal + three file writes; the benchmark therefore
// reflects every component a single review runs through. Use `-benchmem` to
// also see allocations.
func BenchmarkFinalizePipeline(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		b.Fatalf("locate repo root: %v", err)
	}

	for _, fx := range fixtures {
		b.Run(fx.id, func(b *testing.B) {
			tdRoot := filepath.Join(repoRoot, "testdata")
			outDir := b.TempDir()
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
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := runFinalize(argv); err != nil {
					b.Fatalf("runFinalize: %v", err)
				}
			}
		})
	}
}

// BenchmarkDiffParse isolates the unified-diff parser. The largest fixture
// (prisma-29514, ~525 lines, with a deeply hunked pnpm-lock.yaml) dominates;
// parsing speed bounds the diff subcommand's wall-clock.
func BenchmarkDiffParse(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		b.Fatalf("locate repo root: %v", err)
	}
	for _, fx := range fixtures {
		b.Run(fx.id, func(b *testing.B) {
			path := filepath.Join(repoRoot, "testdata", "diffs", fx.id+".diff")
			outDir := b.TempDir()
			argv := []string{
				"--in", path,
				"--out-changed-files", filepath.Join(outDir, "changed.json"),
				"--out-valid-lines", filepath.Join(outDir, "valid.json"),
			}
			// Dummy file existence check so failures surface before timing.
			if _, err := os.Stat(path); err != nil {
				b.Fatalf("missing fixture: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := runDiff(argv); err != nil {
					b.Fatalf("runDiff: %v", err)
				}
			}
		})
	}
}
