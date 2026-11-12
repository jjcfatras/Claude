// Package gates applies the confidence/severity gate from the rubric.
//
//   - confidence < 50: dropped.
//   - 50 <= confidence <= 74: kept only if severity is Critical or Medium.
//   - confidence >= 75: kept regardless of severity.
package gates

import "github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"

// Filter partitions findings by the gate. Dropped findings are surfaced in
// consolidated.json as dropped_by_gate so the reasoning trail behind gate
// decisions is visible in the terminal summary instead of vanishing.
func Filter(in []findings.Finding) (kept, dropped []findings.Finding) {
	kept = make([]findings.Finding, 0, len(in))
	for _, finding := range in {
		if Pass(finding.Confidence, finding.Severity) {
			kept = append(kept, finding)
		} else {
			dropped = append(dropped, finding)
		}
	}
	return kept, dropped
}

func Pass(confidence int, sev findings.Severity) bool {
	switch {
	case confidence < 50:
		return false
	case confidence < 75:
		return sev == findings.SeverityCritical || sev == findings.SeverityMedium
	default:
		return true
	}
}
