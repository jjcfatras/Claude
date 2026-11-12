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
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 20}}})
	result := Classify([]findings.Finding{mk("x.go", 15, nil)}, parsed)
	if len(result.InlineEligible) != 1 || len(result.SummaryOnly) != 0 {
		t.Fatalf("got %d/%d", len(result.InlineEligible), len(result.SummaryOnly))
	}
	if result.InlineEligible[0].Line != 15 {
		t.Errorf("line should not have moved")
	}
}

func TestClassify_SnapsWithin5(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 20}}})
	result := Classify([]findings.Finding{mk("x.go", 23, nil)}, parsed)
	if len(result.InlineEligible) != 1 {
		t.Fatalf("expected snap to inline-eligible, got %d", len(result.InlineEligible))
	}
	got := result.InlineEligible[0]
	if got.Line != 20 {
		t.Errorf("line should snap to 20, got %d", got.Line)
	}
	if !strings.Contains(got.Explanation, "line 23") {
		t.Errorf("snap note should reference original line 23, got: %s", got.Explanation)
	}
}

func TestClassify_TooFarToSnap(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 20}}})
	result := Classify([]findings.Finding{mk("x.go", 30, nil)}, parsed)
	if len(result.SummaryOnly) != 1 {
		t.Fatalf("expected summary-only, got %d", len(result.SummaryOnly))
	}
}

func TestClassify_FileNotInDiff(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 20}}})
	result := Classify([]findings.Finding{mk("y.go", 15, nil)}, parsed)
	if len(result.SummaryOnly) != 1 {
		t.Fatalf("expected summary-only for unknown file")
	}
}

func TestClassify_MultilineBothInRange(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 30}}})
	start := 12
	result := Classify([]findings.Finding{mk("x.go", 18, &start)}, parsed)
	if len(result.InlineEligible) != 1 {
		t.Fatalf("expected inline-eligible")
	}
	if result.InlineEligible[0].StartLine == nil || *result.InlineEligible[0].StartLine != 12 {
		t.Errorf("startLine should be preserved")
	}
}

func TestClassify_MultilineStartOOR_LineValid(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 30}}})
	start := 5 // OOR
	result := Classify([]findings.Finding{mk("x.go", 15, &start)}, parsed)
	if len(result.InlineEligible) != 1 {
		t.Fatalf("expected inline-eligible")
	}
	if result.InlineEligible[0].StartLine != nil {
		t.Errorf("startLine should have been dropped, got %v", *result.InlineEligible[0].StartLine)
	}
	if result.InlineEligible[0].Line != 15 {
		t.Errorf("line should be unchanged")
	}
}

func TestClassify_MultilineLineOOR_Snap(t *testing.T) {
	parsed := parsedWith(map[string][]diff.Run{"x.go": {{Start: 10, End: 30}}})
	start := 12
	result := Classify([]findings.Finding{mk("x.go", 33, &start)}, parsed)
	if len(result.InlineEligible) != 1 {
		t.Fatalf("expected snap to inline-eligible")
	}
	got := result.InlineEligible[0]
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
