package dedup

import (
	"cmp"
	"slices"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// relatedCategoryPairs lists category pairs that count as "related" for the
// semantic dedup substring rule. Symmetric — order doesn't matter.
var relatedCategoryPairs = map[[2]string]bool{
	{"security", "errors"}:   true,
	{"errors", "security"}:   true,
	{"quality", "claude-md"}: true,
	{"claude-md", "quality"}: true,
	{"typescript", "react"}:  true,
	{"react", "typescript"}:  true,
}

// inDiff reports whether the finding's file appears in the parsed diff's
// valid-line map (i.e., is anchored to a file the PR actually touched with
// content changes — binary files, pure renames, and deletions are excluded).
type inDiff func(path string) bool

// Semantic runs after Positional. It looks for findings that describe the same
// defect at different anchors. Two findings are semantic duplicates when
// either condition holds:
//
//  1. One finding's `file` appears as a path-string in the other's
//     `explanation` AND both are severity Critical or Medium.
//  2. The two findings' `explanation` fields share a common substring of
//     length >= 60, AND their `category` fields are in `relatedCategoryPairs`.
//
// On a match: keep the finding whose file is in-diff (so the result tends to
// be inline-eligible). If both or neither are in-diff, keep the higher
// confidence. Cross-references are stored on the kept finding's CrossRefs
// slice so the renderer can list them after the Explanation; matchers always
// see the specialist's pristine Explanation, never one polluted by an earlier
// pass's cross-ref note.
func Semantic(in []findings.Finding, isInDiff inDiff) []findings.Finding {
	if len(in) <= 1 {
		return append([]findings.Finding(nil), in...)
	}
	// Track each finding's current state by ID. The map drives both the
	// pairwise loop and final emission, which avoids the historical bug where
	// rewriting `in[i] = keep` left two slice slots pointing at the same
	// surviving finding and the final loop emitted it twice.
	current := make(map[string]findings.Finding, len(in))
	order := make([]string, 0, len(in))
	for _, finding := range in {
		if _, exists := current[finding.ID]; exists {
			continue
		}
		current[finding.ID] = finding
		order = append(order, finding.ID)
	}

	for i := 0; i < len(order); i++ {
		idA := order[i]
		left, ok := current[idA]
		if !ok {
			continue
		}
		for j := i + 1; j < len(order); j++ {
			idB := order[j]
			right, ok := current[idB]
			if !ok {
				continue
			}
			if !semanticDup(left, right) {
				continue
			}
			keep, drop := pickRep(left, right, isInDiff)
			keep.CrossRefs = append(keep.CrossRefs, makeCrossRef(drop))
			delete(current, drop.ID)
			current[keep.ID] = keep
			if drop.ID == idA {
				// `left` lost — keep matching against the surviving partner from this slot.
				left = keep
			}
		}
	}

	out := make([]findings.Finding, 0, len(current))
	for _, id := range order {
		if finding, ok := current[id]; ok {
			out = append(out, finding)
		}
	}

	slices.SortFunc(out, func(a, b findings.Finding) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
		)
	})
	return out
}

func semanticDup(left, right findings.Finding) bool {
	if left.File == right.File && left.Line == right.Line {
		// Already merged by positional pass; don't double-merge.
		return false
	}
	// Rule 1: cross-file path mention with both >= Medium.
	if left.Severity.Rank() >= findings.SeverityMedium.Rank() &&
		right.Severity.Rank() >= findings.SeverityMedium.Rank() {
		if strings.Contains(left.Explanation, right.File) || strings.Contains(right.Explanation, left.File) {
			return true
		}
	}
	// Rule 2: related-category pair + 60-char common substring.
	if relatedCategoryPairs[[2]string{left.Category, right.Category}] {
		if longestCommonSubstringLen(left.Explanation, right.Explanation) >= 60 {
			return true
		}
	}
	return false
}

func pickRep(left, right findings.Finding, isInDiff inDiff) (keep, drop findings.Finding) {
	leftIn, rightIn := isInDiff(left.File), isInDiff(right.File)
	switch {
	case leftIn && !rightIn:
		return left, right
	case rightIn && !leftIn:
		return right, left
	}
	if left.Confidence != right.Confidence {
		if left.Confidence > right.Confidence {
			return left, right
		}
		return right, left
	}
	// Stable: keep the lexicographically-earlier ID so the output is deterministic.
	if left.ID < right.ID {
		return left, right
	}
	return right, left
}

// longestCommonSubstringLen returns the length of the longest contiguous
// substring shared by left and right. O(n*m) DP; both inputs are bounded by
// the rubric's specialist-written explanation length (a few KB at most).
func longestCommonSubstringLen(left, right string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	prev := make([]int, len(right)+1)
	curr := make([]int, len(right)+1)
	best := 0
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > best {
					best = curr[j]
				}
			} else {
				curr[j] = 0
			}
		}
		prev, curr = curr, prev
		clear(curr)
	}
	return best
}
