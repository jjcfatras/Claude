package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file that clears neither cap must land on disk and be advertised as a local
// path, not as a `git show` chore for every specialist to repeat. This is the
// PR #1817 shape: the aggregate budget is nowhere near spent, but the per-file
// cap alone drops the file.
func TestBuild_MaterializesOversizeFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["small.ts","big.ts"]`)
	mustWrite(t, filepath.Join(dir, "roster.json"), `[]`)
	mustWrite(t, filepath.Join(dir, "prior-issues.json"), `{}`)
	mustWrite(t, filepath.Join(dir, "claude-md-files.json"), `{}`)
	rubric := seedRubric(t, dir)

	headDir := filepath.Join(dir, "head", "deadbeef")
	showFn := func(workdir, sha, path string) (string, int, error) {
		if path == "big.ts" {
			return padTo(path, 5000), 5000, nil
		}
		return padTo(path, 100), 100, nil
	}

	out, err := Build(Input{
		ReviewTmpDir:        dir,
		HeadSHA:             "deadbeef",
		PRNumber:            1,
		Owner:               "o",
		Repo:                "r",
		RubricPath:          rubric,
		MaxSourceBytes:      1024,
		MaxTotalSourceBytes: 100000,
		GitShowFn:           showFn,
		HeadDir:             headDir,
		ValidLines:          map[string][][2]int{"big.ts": {{200, 210}}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The small file still embeds — materialization is the overflow path only.
	if got := indexRow(out, "small.ts"); !strings.Contains(got, "| embedded |") {
		t.Errorf("small.ts should still embed; got: %s", got)
	}

	local := filepath.Join(headDir, "big.ts")
	body, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("oversize file was not written to HeadDir: %v", err)
	}
	if len(body) != 5000 {
		t.Errorf("materialized copy truncated: got %d bytes, want 5000", len(body))
	}

	row := indexRow(out, "big.ts")
	if !strings.Contains(row, "| materialized |") {
		t.Errorf("big.ts should be materialized; got: %s", row)
	}
	if !strings.Contains(row, local) {
		t.Errorf("index row must name the local path %q; got: %s", local, row)
	}
	// Windows are padded by windowPad on each side and clamped at line 1.
	if !strings.Contains(row, "offset:160 limit:91") {
		t.Errorf("index row should carry the padded hunk window; got: %s", row)
	}
	// The whole point is that specialists stop shelling out for it.
	if strings.Contains(row, "git show") {
		t.Errorf("a materialized row must not tell specialists to git show; got: %s", row)
	}
}

// With HeadDir unset the pre-2.4.0 contract must hold exactly — this is what
// keeps every existing golden and caller valid.
func TestBuild_WithoutHeadDirStillOmits(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "changed-files.json"), `["big.ts"]`)
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
		MaxSourceBytes: 1024,
		GitShowFn: func(workdir, sha, path string) (string, int, error) {
			return padTo(path, 5000), 5000, nil
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	row := indexRow(out, "big.ts")
	if !strings.Contains(row, "| omitted |") || !strings.Contains(row, "git show") {
		t.Errorf("without HeadDir the row must stay omitted-with-git-show; got: %s", row)
	}
}

func TestReadWindows(t *testing.T) {
	tests := []struct {
		name   string
		ranges [][2]int
		want   string
	}{
		{"none", nil, ""},
		{"single range is padded both sides", [][2]int{{200, 210}}, "offset:160 limit:91"},
		{"clamped at line 1", [][2]int{{5, 10}}, "offset:1 limit:50"},
		// 100..110 pads to 60..150 and 140..150 pads to 100..190: they overlap,
		// so a specialist must be handed one window, not two that re-read lines.
		{"overlapping ranges merge", [][2]int{{100, 110}, {140, 150}}, "offset:60 limit:131"},
		// Adjacent-after-padding windows (gap of exactly 1) also merge.
		{"touching ranges merge", [][2]int{{100, 100}, {181, 181}}, "offset:60 limit:162"},
		{"disjoint ranges stay separate", [][2]int{{100, 100}, {400, 400}}, "offset:60 limit:81, offset:360 limit:81"},
		// Input order must not matter.
		{"unsorted input", [][2]int{{400, 400}, {100, 100}}, "offset:60 limit:81, offset:360 limit:81"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readWindows(tt.ranges); got != tt.want {
				t.Errorf("readWindows(%v) = %q, want %q", tt.ranges, got, tt.want)
			}
		})
	}
}
