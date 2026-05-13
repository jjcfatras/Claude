package dedup

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func mkFinding(id, specialist, category, file string, line, conf int, sev findings.Severity, expl, code string) findings.Finding {
	return findings.Finding{
		ID:          id,
		Specialist:  specialist,
		Category:    category,
		File:        file,
		Line:        line,
		Confidence:  conf,
		Severity:    sev,
		Explanation: expl,
		Code:        code,
	}
}

func TestPositional_DropsCloseLine(t *testing.T) {
	findingA := mkFinding("a", "security", "security", "src/x.ts", 10, 75, findings.SeverityCritical, "issue A", "code")
	findingB := mkFinding("b", "quality", "security", "src/x.ts", 12, 60, findings.SeverityCritical, "issue B", "code")
	out := Positional([]findings.Finding{findingA, findingB})
	if len(out) != 1 {
		t.Fatalf("want 1 finding, got %d", len(out))
	}
	if out[0].ID != "a" {
		t.Errorf("want kept=a (higher confidence), got %s", out[0].ID)
	}
	if len(out[0].CrossRefs) != 1 || out[0].CrossRefs[0].Specialist != "quality" {
		t.Errorf("expected one CrossRef for specialist=quality, got: %+v", out[0].CrossRefs)
	}
	if strings.Contains(out[0].Explanation, "quality") {
		t.Errorf("Explanation should remain pristine; got: %s", out[0].Explanation)
	}
}

func TestPositional_KeepsFarLines(t *testing.T) {
	findingA := mkFinding("a", "security", "security", "src/x.ts", 10, 75, findings.SeverityCritical, "A", "")
	findingB := mkFinding("b", "quality", "quality", "src/x.ts", 14, 60, findings.SeverityMedium, "B", "")
	// 14-10 = 4 > 3 → no merge
	out := Positional([]findings.Finding{findingA, findingB})
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}

func TestPositional_DomainTieBreak(t *testing.T) {
	findingA := mkFinding("a", "quality", "security", "src/x.ts", 10, 80, findings.SeverityCritical, "A", "")
	findingB := mkFinding("b", "security", "security", "src/x.ts", 11, 80, findings.SeverityCritical, "B", "")
	out := Positional([]findings.Finding{findingA, findingB})
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].ID != "b" {
		t.Errorf("expected security specialist to win the tie, got %s", out[0].ID)
	}
}

func TestPositional_DifferentFilesNotMerged(t *testing.T) {
	findingA := mkFinding("a", "security", "security", "src/x.ts", 10, 75, findings.SeverityCritical, "A", "")
	findingB := mkFinding("b", "security", "security", "src/y.ts", 10, 75, findings.SeverityCritical, "B", "")
	out := Positional([]findings.Finding{findingA, findingB})
	if len(out) != 2 {
		t.Fatalf("want 2 (different files), got %d", len(out))
	}
}

func TestSemantic_FileInExplanation(t *testing.T) {
	findingA := mkFinding("a", "quality", "quality", "src/generators/ts.ts", 10, 70, findings.SeverityMedium,
		"TS generator updated. The JS counterpart at src/generators/js.ts should mirror this.", "")
	findingB := mkFinding("b", "claude-md", "claude-md", "src/generators/js.ts", 5, 65, findings.SeverityMedium,
		"CLAUDE.md says update both generators in lockstep.", "")
	inDiff := func(path string) bool { return path == "src/generators/ts.ts" }
	out := Semantic([]findings.Finding{findingA, findingB}, inDiff)
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].ID != "a" {
		t.Errorf("expected in-diff representative (a), got %s", out[0].ID)
	}
}

func TestSemantic_FileMatchSkippedBelowMedium(t *testing.T) {
	findingA := mkFinding("a", "quality", "quality", "x.ts", 10, 50, findings.SeverityMinor,
		"the file y.ts is mentioned here", "")
	findingB := mkFinding("b", "claude-md", "claude-md", "y.ts", 5, 50, findings.SeverityMinor,
		"unrelated", "")
	out := Semantic([]findings.Finding{findingA, findingB}, func(string) bool { return true })
	if len(out) != 2 {
		t.Errorf("Minor severity should skip rule 1, got %d", len(out))
	}
}

func TestSemantic_RelatedCategorySubstring(t *testing.T) {
	shared := strings.Repeat("X", 60) + " — same defect text appears here"
	findingA := mkFinding("a", "security", "security", "x.ts", 10, 70, findings.SeverityMedium,
		"prefix "+shared+" suffix A", "")
	findingB := mkFinding("b", "errors", "errors", "y.ts", 20, 65, findings.SeverityMedium,
		"prefix B "+shared+" end", "")
	out := Semantic([]findings.Finding{findingA, findingB}, func(string) bool { return false })
	if len(out) != 1 {
		t.Fatalf("want 1 (related category + substring match), got %d", len(out))
	}
}

func TestSemantic_UnrelatedCategorySubstring(t *testing.T) {
	shared := strings.Repeat("X", 80)
	findingA := mkFinding("a", "security", "security", "x.ts", 10, 70, findings.SeverityMedium,
		shared, "")
	findingB := mkFinding("b", "perf", "perf", "y.ts", 20, 65, findings.SeverityMedium,
		shared, "")
	out := Semantic([]findings.Finding{findingA, findingB}, func(string) bool { return false })
	if len(out) != 2 {
		t.Errorf("unrelated categories should not merge: got %d", len(out))
	}
}

func TestPriorReview_DropsContextLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "snippet code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, Snippet: "anything"},
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (want 0/1)", len(kept), len(dropped))
	}
}

func TestPriorReview_KeepsAddedLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "snippet code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, Snippet: "anything"},
		},
	}
	isAdded := func(_ string, _ int) bool { return true }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d (want 1/0)", len(kept), len(dropped))
	}
	if !strings.Contains(kept[0].Explanation, "prior review") {
		t.Errorf("expected prior-review note appended, got: %s", kept[0].Explanation)
	}
}

func TestPriorReview_SnippetMatch(t *testing.T) {
	snippet := strings.Repeat("Z", 50) + " distinctive code here"
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 1000, 80, findings.SeverityMedium, "expl", snippet),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 5, Snippet: "preamble " + snippet + " trail"},
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (snippet should match)", len(kept), len(dropped))
	}
}

// TestSemantic_NoDuplicateEmit reproduces a real-world bug: when the at-position-i
// finding loses a semantic match, the previous implementation rewrote `in[i] = keep`,
// leaving `keep` at both index i and index j. The final emit loop appended both
// copies. Reproduced from the k8s-138754 fixture.
func TestSemantic_NoDuplicateEmit(t *testing.T) {
	// Three findings on the same file. A is the eventual loser of a Rule 1 match
	// against C; B exists to populate A's CrossRefs (legitimately, via positional)
	// before semantic runs.
	findingA := mkFinding("a", "errors", "errors", "src/x.ts", 100, 70, findings.SeverityMedium,
		"issue at hot path", "")
	findingB := mkFinding("b", "perf", "perf", "src/x.ts", 102, 60, findings.SeverityMinor,
		"adjacent perf nit", "")
	findingC := mkFinding("c", "quality", "quality", "src/x.ts", 200, 70, findings.SeverityMedium,
		// findingC's explanation mentions findingA's file → triggers semantic Rule 1.
		"the function in src/x.ts duplicates work", "")

	// Run positional first (findingA + findingB cluster, kept=findingA with cross-ref to findingB).
	step1 := Positional([]findings.Finding{findingA, findingB, findingC})
	out := Semantic(step1, func(_ string) bool { return true })

	seen := make(map[string]int)
	for _, finding := range out {
		seen[finding.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("finding %s emitted %d times; expected at most 1", id, count)
		}
	}
}

// TestPositionalThenSemantic_NoFilePathCascade reproduces the cascading false
// positive seen on k8s-138754: positional dedup folded perf into err-2, and the
// resulting cross-ref note added the file path "batch.go" to err-2.Explanation.
// Semantic Rule 1 then matched (err-2, qual-2) because qual-2's file ("batch.go")
// appeared in err-2's polluted Explanation. Both findings live on the same file,
// so the match was a false positive driven by the prior pass's bookkeeping.
func TestPositionalThenSemantic_NoFilePathCascade(t *testing.T) {
	errFinding := mkFinding("err", "errors", "errors", "pkg/batch.go", 100, 70, findings.SeverityMedium,
		"the new branch shares the same return path as the old one", "")
	perfFinding := mkFinding("perf", "perf", "perf", "pkg/batch.go", 101, 55, findings.SeverityMinor,
		"extra branch on hot path", "")
	qualFinding := mkFinding("qual", "quality", "quality", "pkg/batch.go", 200, 70, findings.SeverityMedium,
		"out-of-diff coverage gap on test side", "")

	step1 := Positional([]findings.Finding{errFinding, perfFinding, qualFinding})
	out := Semantic(step1, func(path string) bool { return path == "pkg/batch.go" })

	if len(out) != 2 {
		t.Fatalf("expected 2 survivors (err-after-positional + qual); got %d: %+v", len(out), out)
	}
	ids := map[string]bool{}
	for _, finding := range out {
		ids[finding.ID] = true
	}
	if !ids["err"] || !ids["qual"] {
		t.Errorf("expected both err and qual to survive; got: %v", ids)
	}
}

func TestLongestCommonSubstringLen(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"", "", 0},
		{"abc", "xyz", 0},
		{"abcdef", "zzcdezz", 3},
		{strings.Repeat("a", 100), "bbb" + strings.Repeat("a", 60) + "ccc", 60},
	}
	for _, tc := range cases {
		got := longestCommonSubstringLen(tc.left, tc.right)
		if got != tc.want {
			t.Errorf("LCS(%q, %q) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}
