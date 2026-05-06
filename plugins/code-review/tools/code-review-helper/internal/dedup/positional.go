// Package dedup implements the three dedup passes from the code-review rubric:
//
//   - Positional: group by file + line ±3, keep highest confidence.
//   - Semantic:   file-in-explanation match OR 60-char substring + related category.
//   - PriorReview: match against issues from a prior review on the same PR.
package dedup

import (
	"sort"

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
	for _, f := range in {
		byFile[f.File] = append(byFile[f.File], f)
	}

	var out []findings.Finding
	for file, group := range byFile {
		_ = file
		sort.Slice(group, func(i, j int) bool {
			return group[i].Line < group[j].Line
		})

		var clusters [][]findings.Finding
		for _, f := range group {
			placed := false
			for ci := range clusters {
				for _, member := range clusters[ci] {
					if abs(member.Line-f.Line) <= 3 {
						clusters[ci] = append(clusters[ci], f)
						placed = true
						break
					}
				}
				if placed {
					break
				}
			}
			if !placed {
				clusters = append(clusters, []findings.Finding{f})
			}
		}

		for _, cluster := range clusters {
			sort.Slice(cluster, func(i, j int) bool {
				if cluster[i].Confidence != cluster[j].Confidence {
					return cluster[i].Confidence > cluster[j].Confidence
				}
				ai := domainMatch(cluster[i].Specialist, cluster[i].Category)
				aj := domainMatch(cluster[j].Specialist, cluster[j].Category)
				if ai != aj {
					return ai
				}
				if cluster[i].Specialist != cluster[j].Specialist {
					return cluster[i].Specialist < cluster[j].Specialist
				}
				// Last-resort tiebreak by ID so the outcome is fully deterministic
				// when two findings share confidence, domain, and specialist.
				return cluster[i].ID < cluster[j].ID
			})
			kept := cluster[0]
			for _, other := range cluster[1:] {
				kept.CrossRefs = append(kept.CrossRefs, makeCrossRef(other))
			}
			out = append(out, kept)
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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
