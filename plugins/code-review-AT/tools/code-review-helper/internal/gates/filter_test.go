package gates

import (
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

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
