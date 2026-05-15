// Package lines applies the diff-line validation step (Gate 2 in the skill's
// step 3). Each finding is classified as inline-eligible (with optional
// snapping) or summary-only.
package lines

import (
	"fmt"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/intmath"
)

const snapWindow = 5

const snapNoteFmt = "_Note: This comment was placed on the nearest diff line; the issue actually occurs on line %d._\n\n"

type Result struct {
	InlineEligible []findings.Finding
	SummaryOnly    []findings.Finding
}

// Classify partitions `in` into inline-eligible and summary-only buckets by
// checking each finding's `(file, line)` against `parsed.ValidLines`.
//
//   - In range → inline-eligible, finding unchanged.
//   - Out of range, |line - nearestValid| <= 5 → inline-eligible, line snapped
//     to the nearest valid line, snap note prepended to the explanation.
//   - Out of range, > 5 away (or file not in valid-lines map) → summary-only.
//
// Multi-line findings (startLine != nil): if startLine is out-of-range but
// line is valid, drop startLine (collapse to single-line). If line is
// out-of-range, apply snapping to line; startLine is dropped.
func Classify(in []findings.Finding, parsed *diff.Parsed) Result {
	var res Result
	for _, finding := range in {
		// Multi-line: check startLine first.
		if finding.StartLine != nil {
			startInRange := parsed.InRange(finding.File, *finding.StartLine)
			lineInRange := parsed.InRange(finding.File, finding.Line)
			switch {
			case startInRange && lineInRange:
				res.InlineEligible = append(res.InlineEligible, finding)
				continue
			case lineInRange && !startInRange:
				finding.StartLine = nil
				res.InlineEligible = append(res.InlineEligible, finding)
				continue
			default:
				// Line itself is out of range — fall through to single-line snapping
				// against `line` and drop the startLine.
				finding.StartLine = nil
			}
		}

		if parsed.InRange(finding.File, finding.Line) {
			res.InlineEligible = append(res.InlineEligible, finding)
			continue
		}

		nearest, ok := parsed.NearestValid(finding.File, finding.Line)
		if !ok {
			res.SummaryOnly = append(res.SummaryOnly, finding)
			continue
		}
		if intmath.Abs(nearest-finding.Line) > snapWindow {
			res.SummaryOnly = append(res.SummaryOnly, finding)
			continue
		}
		original := finding.Line
		finding.Line = nearest
		finding.Explanation = fmt.Sprintf(snapNoteFmt, original) + finding.Explanation
		res.InlineEligible = append(res.InlineEligible, finding)
	}
	return res
}
