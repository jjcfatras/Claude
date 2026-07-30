package dedup

import (
	"maps"
	"slices"
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

// TestPositional_KeepsHigherSeverityUnrelatedPeer reproduces the failure this
// guard exists to prevent, using the shape from the real review that lost a
// finding to it: a one-line diff hunk where every specialist anchors to the same
// line, and an unrelated Minor wins the confidence tiebreak over two independent
// Mediums. The Minor must not bury them; the two related Mediums (security and
// errors are a related-category pair) must still collapse into one.
func TestPositional_KeepsHigherSeverityUnrelatedPeer(t *testing.T) {
	minor := mkFinding("quality-1", "quality", "quality", "src/x.ts", 61, 75, findings.SeverityMinor, "the & expansion is still first-occurrence-only", "code")
	sec := mkFinding("security-1", "security", "security", "src/x.ts", 61, 65, findings.SeverityMedium, "missing backfill for a changed identity key", "code")
	errs := mkFinding("errors-1", "errors", "errors", "src/x.ts", 61, 55, findings.SeverityMedium, "no error path distinguishes key drift from new data", "code")

	out := Positional([]findings.Finding{minor, sec, errs})
	if len(out) != 2 {
		t.Fatalf("want 2 survivors (unrelated Minor must not bury the Mediums), got %d: %+v", len(out), out)
	}

	byID := map[string]findings.Finding{}
	for _, f := range out {
		byID[f.ID] = f
	}
	if _, ok := byID["quality-1"]; !ok {
		t.Errorf("quality-1 (highest confidence) should survive; got %v", slices.Collect(maps.Keys(byID)))
	}
	survivor, ok := byID["security-1"]
	if !ok {
		t.Fatalf("security-1 (Medium) must survive an unrelated Minor; got %v", slices.Collect(maps.Keys(byID)))
	}
	if len(survivor.CrossRefs) != 1 || survivor.CrossRefs[0].ID != "errors-1" {
		t.Errorf("errors-1 should fold into security-1 (related categories), got CrossRefs: %+v", survivor.CrossRefs)
	}
	if got := survivor.CrossRefs[0].MergedBy; got != findings.MergedByPositional {
		t.Errorf("want merged_by=%q, got %q", findings.MergedByPositional, got)
	}
}

func TestPositional_FoldsLowerSeverityUnrelatedPeer(t *testing.T) {
	medium := mkFinding("a", "security", "security", "src/x.ts", 10, 60, findings.SeverityMedium, "A", "")
	minor := mkFinding("b", "perf", "perf", "src/x.ts", 11, 55, findings.SeverityMinor, "B", "")
	out := Positional([]findings.Finding{medium, minor})
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("a lower-severity peer should still fold under a more severe survivor, got %+v", out)
	}
}

func TestPositional_RelatedCategoriesFoldDespiteSeverity(t *testing.T) {
	// errors/security are a related-category pair, so the same-defect signal
	// permits the fold even though the peer is more severe than the survivor.
	medium := mkFinding("a", "security", "security", "src/x.ts", 10, 70, findings.SeverityMedium, "A", "")
	critical := mkFinding("b", "errors", "errors", "src/x.ts", 11, 60, findings.SeverityCritical, "B", "")
	out := Positional([]findings.Finding{medium, critical})
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("related categories should fold regardless of severity, got %+v", out)
	}
	if len(out[0].CrossRefs) != 1 || out[0].CrossRefs[0].Severity != findings.SeverityCritical {
		t.Errorf("folded peer's severity must be preserved in the CrossRef, got %+v", out[0].CrossRefs)
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
