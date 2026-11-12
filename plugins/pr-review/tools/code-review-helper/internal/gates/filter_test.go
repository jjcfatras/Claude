package gates

import (
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func TestFilter(t *testing.T) {
	in := []findings.Finding{
		{ID: "f-1", Confidence: 80, Severity: findings.SeverityMinor},    // kept
		{ID: "f-2", Confidence: 45, Severity: findings.SeverityCritical}, // dropped
		{ID: "f-3", Confidence: 60, Severity: findings.SeverityMedium},   // kept
		{ID: "f-4", Confidence: 60, Severity: findings.SeverityMinor},    // dropped
	}
	kept, dropped := Filter(in)
	wantKept := []string{"f-1", "f-3"}
	wantDropped := []string{"f-2", "f-4"}
	if len(kept) != len(wantKept) {
		t.Fatalf("kept = %d findings, want %d", len(kept), len(wantKept))
	}
	for i, id := range wantKept {
		if kept[i].ID != id {
			t.Errorf("kept[%d].ID = %q, want %q (order must be preserved)", i, kept[i].ID, id)
		}
	}
	if len(dropped) != len(wantDropped) {
		t.Fatalf("dropped = %d findings, want %d", len(dropped), len(wantDropped))
	}
	for i, id := range wantDropped {
		if dropped[i].ID != id {
			t.Errorf("dropped[%d].ID = %q, want %q (order must be preserved)", i, dropped[i].ID, id)
		}
	}
}

func TestFilter_Empty(t *testing.T) {
	kept, dropped := Filter(nil)
	if len(kept) != 0 || dropped != nil {
		t.Errorf("Filter(nil) = (%v, %v), want (empty, nil)", kept, dropped)
	}
}

func TestPass(t *testing.T) {
	cases := []struct {
		conf int
		sev  findings.Severity
		want bool
	}{
		{49, findings.SeverityCritical, false},
		{50, findings.SeverityCritical, true},
		{50, findings.SeverityMedium, true},
		{50, findings.SeverityMinor, false},
		{74, findings.SeverityMinor, false},
		{74, findings.SeverityCritical, true},
		{75, findings.SeverityMinor, true},
		{75, findings.SeverityMedium, true},
		{100, findings.SeverityMinor, true},
		{0, findings.SeverityCritical, false},
	}
	for _, tc := range cases {
		got := Pass(tc.conf, tc.sev)
		if got != tc.want {
			t.Errorf("Pass(%d, %s) = %v, want %v", tc.conf, tc.sev, got, tc.want)
		}
	}
}
