package lines

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func parsedWith(runs map[string][]diff.Run) *diff.Parsed {
	return &diff.Parsed{ValidLines: runs}
}

func mk(file string, line int, startLine *int) findings.Finding {
	return findings.Finding{
		ID:         "x",
		File:       file,
		Line:       line,
		StartLine:  startLine,
		Confidence: 80,
		Severity:   findings.SeverityCritical,
	}
}

func TestClassify_InRange(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 20}}})
	r := Classify([]findings.Finding{mk("x.go", 15, nil)}, p)
	if len(r.InlineEligible) != 1 || len(r.SummaryOnly) != 0 {
		t.Fatalf("got %d/%d", len(r.InlineEligible), len(r.SummaryOnly))
	}
	if r.InlineEligible[0].Line != 15 {
		t.Errorf("line should not have moved")
	}
}

func TestClassify_SnapsWithin5(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 20}}})
	r := Classify([]findings.Finding{mk("x.go", 23, nil)}, p)
	if len(r.InlineEligible) != 1 {
		t.Fatalf("expected snap to inline-eligible, got %d", len(r.InlineEligible))
	}
	got := r.InlineEligible[0]
	if got.Line != 20 {
		t.Errorf("line should snap to 20, got %d", got.Line)
	}
	if !strings.Contains(got.Explanation, "line 23") {
		t.Errorf("snap note should reference original line 23, got: %s", got.Explanation)
	}
}

func TestClassify_TooFarToSnap(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 20}}})
	r := Classify([]findings.Finding{mk("x.go", 30, nil)}, p)
	if len(r.SummaryOnly) != 1 {
		t.Fatalf("expected summary-only, got %d", len(r.SummaryOnly))
	}
}

func TestClassify_FileNotInDiff(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 20}}})
	r := Classify([]findings.Finding{mk("y.go", 15, nil)}, p)
	if len(r.SummaryOnly) != 1 {
		t.Fatalf("expected summary-only for unknown file")
	}
}

func TestClassify_MultilineBothInRange(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 30}}})
	start := 12
	r := Classify([]findings.Finding{mk("x.go", 18, &start)}, p)
	if len(r.InlineEligible) != 1 {
		t.Fatalf("expected inline-eligible")
	}
	if r.InlineEligible[0].StartLine == nil || *r.InlineEligible[0].StartLine != 12 {
		t.Errorf("startLine should be preserved")
	}
}

func TestClassify_MultilineStartOOR_LineValid(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 30}}})
	start := 5 // OOR
	r := Classify([]findings.Finding{mk("x.go", 15, &start)}, p)
	if len(r.InlineEligible) != 1 {
		t.Fatalf("expected inline-eligible")
	}
	if r.InlineEligible[0].StartLine != nil {
		t.Errorf("startLine should have been dropped, got %v", *r.InlineEligible[0].StartLine)
	}
	if r.InlineEligible[0].Line != 15 {
		t.Errorf("line should be unchanged")
	}
}

func TestClassify_MultilineLineOOR_Snap(t *testing.T) {
	p := parsedWith(map[string][]diff.Run{"x.go": {{10, 30}}})
	start := 12
	r := Classify([]findings.Finding{mk("x.go", 33, &start)}, p)
	if len(r.InlineEligible) != 1 {
		t.Fatalf("expected snap to inline-eligible")
	}
	got := r.InlineEligible[0]
	if got.StartLine != nil {
		t.Errorf("multi-line collapse: startLine should be nil")
	}
	if got.Line != 30 {
		t.Errorf("line should snap to 30, got %d", got.Line)
	}
	if !strings.Contains(got.Explanation, "line 33") {
		t.Errorf("snap note missing")
	}
}
