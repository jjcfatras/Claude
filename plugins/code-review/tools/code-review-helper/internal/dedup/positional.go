// Package dedup implements the three dedup passes from the code-review rubric:
//
//   - Positional: group by file + line ±3, keep highest confidence.
//   - Semantic:   file-in-explanation match OR 60-char substring + related category.
//   - PriorReview: match against issues from a prior review on the same PR.
package dedup

import (
	"cmp"
	"slices"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// Positional groups findings by file + line proximity (within ±3) and keeps
// the highest-confidence representative per group. The kept finding gets a
// note appended to its `explanation` listing the other specialists that
// independently raised the same defect.
//
// Order-stability: within a group, ties are broken first by domain match
// (specialist == category), then by lexicographic specialist name. Output is
// sorted by file then line for deterministic downstream processing.
func Positional(in []findings.Finding) []findings.Finding {
	if len(in) <= 1 {
		return append([]findings.Finding(nil), in...)
	}

	byFile := make(map[string][]findings.Finding)
	for _, finding := range in {
		byFile[finding.File] = append(byFile[finding.File], finding)
	}

	var out []findings.Finding
	for _, group := range byFile {
		slices.SortFunc(group, func(a, b findings.Finding) int {
			return cmp.Compare(a.Line, b.Line)
		})

		// Group is sorted by line, so a new finding can only join the most-recent
		// cluster (its last member is the largest line so far). Check that one
		// boundary instead of scanning every cluster.
		var clusters [][]findings.Finding
		for _, finding := range group {
			if n := len(clusters); n > 0 {
				last := clusters[n-1]
				if finding.Line-last[len(last)-1].Line <= 3 {
					clusters[n-1] = append(last, finding)
					continue
				}
			}
			clusters = append(clusters, []findings.Finding{finding})
		}

		for _, cluster := range clusters {
			slices.SortFunc(cluster, func(a, b findings.Finding) int {
				return cmp.Or(
					cmp.Compare(b.Confidence, a.Confidence),
					boolCompare(domainMatch(a.Specialist, a.Category), domainMatch(b.Specialist, b.Category)),
					cmp.Compare(a.Specialist, b.Specialist),
					cmp.Compare(a.ID, b.ID),
				)
			})
			kept := cluster[0]
			for _, other := range cluster[1:] {
				kept.CrossRefs = append(kept.CrossRefs, makeCrossRef(other))
			}
			out = append(out, kept)
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

// makeCrossRef snapshots the fields of a folded peer finding into the kept
// finding's CrossRefs list. Storing this as a struct (rather than mutating
// Explanation in place) keeps the original specialist explanation pristine —
// semantic Rule 1's file-path check would otherwise trip on a peer file path
// that an earlier dedup pass injected. The CrossRefs list is no longer rendered
// into user-facing output (PR comments / fallback / summary); it remains an
// audit trail in `consolidated.json` so the lead can see which peer findings
// were folded by dedup.
func makeCrossRef(other findings.Finding) findings.CrossRef {
	return findings.CrossRef{
		Specialist: other.Specialist,
		Confidence: other.Confidence,
		File:       other.File,
		Line:       other.Line,
	}
}

func domainMatch(specialist, category string) bool {
	return specialist == category
}

// boolCompare orders true before false (sort-descending on a bool flag).
func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}
