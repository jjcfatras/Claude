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
