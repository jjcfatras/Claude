package dedup

import (
	"sort"
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
	for _, f := range in {
		if _, exists := current[f.ID]; exists {
			continue
		}
		current[f.ID] = f
		order = append(order, f.ID)
	}

	for i := 0; i < len(order); i++ {
		idA := order[i]
		a, ok := current[idA]
		if !ok {
			continue
		}
		for j := i + 1; j < len(order); j++ {
			idB := order[j]
			b, ok := current[idB]
			if !ok {
				continue
			}
			if !semanticDup(a, b) {
				continue
			}
			keep, drop := pickRep(a, b, isInDiff)
			keep.CrossRefs = append(keep.CrossRefs, makeCrossRef(drop))
			delete(current, drop.ID)
			current[keep.ID] = keep
			if drop.ID == idA {
				// `a` lost — keep matching against the surviving partner from this slot.
				a = keep
			}
		}
	}

	out := make([]findings.Finding, 0, len(current))
	for _, id := range order {
		if f, ok := current[id]; ok {
			out = append(out, f)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func semanticDup(a, b findings.Finding) bool {
	if a.File == b.File && a.Line == b.Line {
		// Already merged by positional pass; don't double-merge.
		return false
	}
	// Rule 1: cross-file path mention with both >= Medium.
	if a.Severity.Rank() >= findings.SeverityMedium.Rank() &&
		b.Severity.Rank() >= findings.SeverityMedium.Rank() {
		if strings.Contains(a.Explanation, b.File) || strings.Contains(b.Explanation, a.File) {
			return true
		}
	}
	// Rule 2: related-category pair + 60-char common substring.
	if relatedCategoryPairs[[2]string{a.Category, b.Category}] {
		if longestCommonSubstringLen(a.Explanation, b.Explanation) >= 60 {
			return true
		}
	}
	return false
}

func pickRep(a, b findings.Finding, isInDiff inDiff) (keep, drop findings.Finding) {
	aIn, bIn := isInDiff(a.File), isInDiff(b.File)
	switch {
	case aIn && !bIn:
		return a, b
	case bIn && !aIn:
		return b, a
	}
	if a.Confidence != b.Confidence {
		if a.Confidence > b.Confidence {
			return a, b
		}
		return b, a
	}
	// Stable: keep the lexicographically-earlier ID so the output is deterministic.
	if a.ID < b.ID {
		return a, b
	}
	return b, a
}

// longestCommonSubstringLen returns the length of the longest contiguous
// substring shared by a and b. O(n*m) DP; both inputs are bounded by the
// rubric's specialist-written explanation length (a few KB at most).
func longestCommonSubstringLen(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	best := 0
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > best {
					best = curr[j]
				}
			} else {
				curr[j] = 0
			}
		}
		prev, curr = curr, prev
		for j := range curr {
			curr[j] = 0
		}
	}
	return best
}
