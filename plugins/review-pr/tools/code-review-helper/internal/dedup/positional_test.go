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

// TestPositional_SeverityLeadsRepresentativeSelection pins the invariant that
// replaced the old evidence-free severity fallback: a cluster's representative
// is its most severe member, so a high-confidence Minor can neither bury a
// Medium nor — as happened on transcript db83dbda — block one from folding by
// winning the confidence tiebreak and making `Medium <= Minor` false for every
// peer. Shape taken from that review: every specialist anchored to the same
// one-line hunk, and the Minor carried the highest confidence.
//
// The two Mediums here carry unrelated categories, so they are NOT duplicates of
// each other and must both survive — line proximity alone is not duplication.
func TestPositional_SeverityLeadsRepresentativeSelection(t *testing.T) {
	minor := mkFinding("quality-1", "quality", "duplication", "src/x.ts", 61, 75, findings.SeverityMinor, "the & expansion is still first-occurrence-only", "code")
	sec := mkFinding("security-1", "security", "security", "src/x.ts", 61, 65, findings.SeverityMedium, "missing backfill for a changed identity key", "code")
	perf := mkFinding("perf-1", "perf", "performance", "src/x.ts", 61, 55, findings.SeverityMedium, "no error path distinguishes key drift from new data", "code")

	out := Positional([]findings.Finding{minor, sec, perf})
	byID := map[string]findings.Finding{}
	for _, f := range out {
		byID[f.ID] = f
	}
	if len(out) != 2 {
		t.Fatalf("want 2 survivors (two unrelated Mediums are not duplicates), got %d: %v", len(out), slices.Collect(maps.Keys(byID)))
	}
	if _, ok := byID["quality-1"]; ok {
		t.Errorf("a Minor must never represent a cluster containing a Medium, however confident; survivors: %v", slices.Collect(maps.Keys(byID)))
	}
	for _, id := range []string{"security-1", "perf-1"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("%s (Medium) must survive; got %v", id, slices.Collect(maps.Keys(byID)))
		}
	}
	survivor := byID["security-1"]
	if len(survivor.CrossRefs) != 1 || survivor.CrossRefs[0].ID != "quality-1" {
		t.Fatalf("the Minor should fold into the most severe representative, got CrossRefs: %+v", survivor.CrossRefs)
	}
	if got := survivor.CrossRefs[0].MergedBy; got != findings.MergedByPositional {
		t.Errorf("want merged_by=%q, got %q", findings.MergedByPositional, got)
	}
}

func TestPositional_FoldsLowerSeverityUnrelatedPeer(t *testing.T) {
	medium := mkFinding("a", "security", "security", "src/x.ts", 10, 60, findings.SeverityMedium, "A", "")
	minor := mkFinding("b", "perf", "performance", "src/x.ts", 11, 55, findings.SeverityMinor, "B", "")
	out := Positional([]findings.Finding{medium, minor})
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("a strictly lower-severity peer should still fold under a more severe survivor, got %+v", out)
	}
}

// TestPositional_EqualSeverityUnrelatedPeersBothSurvive is the counterpart: with
// the representative now always the most severe, `<=` in canFold would be
// vacuous and collapse every cluster to one finding regardless of evidence.
// Equal severity plus no same-defect signal must keep both.
func TestPositional_EqualSeverityUnrelatedPeersBothSurvive(t *testing.T) {
	a := mkFinding("a", "security", "security", "src/x.ts", 10, 60, findings.SeverityMedium, "A", "")
	b := mkFinding("b", "perf", "performance", "src/x.ts", 11, 55, findings.SeverityMedium, "B", "")
	out := Positional([]findings.Finding{a, b})
	if len(out) != 2 {
		t.Fatalf("equally-severe unrelated peers must not fold without evidence, got %+v", out)
	}
}

func TestPositional_RelatedCategoriesFoldDespiteSeverity(t *testing.T) {
	// security/config are a related-category pair, so the same-defect signal
	// permits the fold across differing severities. The Critical wins
	// representative selection, and the Medium folds into it.
	medium := mkFinding("a", "security", "security", "src/x.ts", 10, 70, findings.SeverityMedium, "A", "")
	critical := mkFinding("b", "infra", "config", "src/x.ts", 11, 60, findings.SeverityCritical, "B", "")
	out := Positional([]findings.Finding{medium, critical})
	if len(out) != 1 || out[0].ID != "b" {
		t.Fatalf("related categories should fold into the most severe survivor, got %+v", out)
	}
	if len(out[0].CrossRefs) != 1 || out[0].CrossRefs[0].Severity != findings.SeverityMedium {
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

// TestPositional_TieBreakIsDeterministic pins the tiebreak that remains once
// severity and confidence are equal: specialist name, then ID. The previous
// `domainMatch` tiebreaker (prefer a finding whose category equals its own role)
// was removed with the closed vocabulary, since a category now names the defect
// rather than the specialist and the two coincide only for "security".
func TestPositional_TieBreakIsDeterministic(t *testing.T) {
	findingA := mkFinding("a", "quality", "security", "src/x.ts", 10, 80, findings.SeverityCritical, "A", "")
	findingB := mkFinding("b", "security", "security", "src/x.ts", 11, 80, findings.SeverityCritical, "B", "")
	out := Positional([]findings.Finding{findingA, findingB})
	if len(out) != 1 {
		t.Fatalf("want 1 (shared category is a same-defect signal), got %d", len(out))
	}
	// "quality" sorts before "security", so a represents the cluster.
	if out[0].ID != "a" {
		t.Errorf("expected the lexicographically-first specialist to win the tie, got %s", out[0].ID)
	}
	// Order of the input must not change the outcome.
	reversed := Positional([]findings.Finding{findingB, findingA})
	if len(reversed) != 1 || reversed[0].ID != out[0].ID {
		t.Errorf("tiebreak is input-order dependent: got %+v", reversed)
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
