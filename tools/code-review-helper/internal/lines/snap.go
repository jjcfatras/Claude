// Package lines applies the diff-line validation step (Gate 2 in the skill's
// step 3). Each finding is classified as inline-eligible (with optional
// snapping) or summary-only.
package lines

import (
	"fmt"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/diff"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
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
	for _, f := range in {
		// Multi-line: check startLine first.
		if f.StartLine != nil {
			startInRange := parsed.InRange(f.File, *f.StartLine)
			lineInRange := parsed.InRange(f.File, f.Line)
			switch {
			case startInRange && lineInRange:
				res.InlineEligible = append(res.InlineEligible, f)
				continue
			case lineInRange && !startInRange:
				f.StartLine = nil
				res.InlineEligible = append(res.InlineEligible, f)
				continue
			default:
				// Line itself is out of range — fall through to single-line snapping
				// against `line` and drop the startLine.
				f.StartLine = nil
			}
		}

		if parsed.InRange(f.File, f.Line) {
			res.InlineEligible = append(res.InlineEligible, f)
			continue
		}

		nearest, ok := parsed.NearestValid(f.File, f.Line)
		if !ok {
			res.SummaryOnly = append(res.SummaryOnly, f)
			continue
		}
		if abs(nearest-f.Line) > snapWindow {
			res.SummaryOnly = append(res.SummaryOnly, f)
			continue
		}
		original := f.Line
		f.Line = nearest
		f.Explanation = fmt.Sprintf(snapNoteFmt, original) + f.Explanation
		res.InlineEligible = append(res.InlineEligible, f)
	}
	return res
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
