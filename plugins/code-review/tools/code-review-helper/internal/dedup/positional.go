// Package dedup implements the three dedup passes from the code-review rubric:
//
//   - Positional: group by file + line ±3, fold same-defect peers into the
//     highest-confidence representative (see canFold).
//   - Semantic:   file-in-explanation match OR 60-char substring + related category.
//   - PriorReview: match against issues from a prior review on the same PR.
package dedup

import (
	"cmp"
	"slices"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// Positional groups findings by file + line proximity (within ±3) and folds
// each cluster down to one representative per distinct defect — not one per
// cluster. Folded peers are recorded on the survivor's CrossRefs; see canFold
// for when a fold is permitted.
//
// Order-stability: within a cluster, findings are visited highest-confidence
// first, ties broken by domain match (specialist == category) then by
// lexicographic specialist name, so the representative set is deterministic.
// Output is sorted by file then line for deterministic downstream processing.
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
			// Greedy fold: each cluster member joins the first representative it
			// may fold into (see canFold), otherwise it becomes a representative
			// itself. A cluster can therefore emit more than one survivor — line
			// proximity alone is not duplication.
			var reps []findings.Finding
			for _, finding := range cluster {
				if i := slices.IndexFunc(reps, func(rep findings.Finding) bool { return canFold(rep, finding) }); i >= 0 {
					reps[i].CrossRefs = append(reps[i].CrossRefs, makeCrossRef(finding, findings.MergedByPositional))
					continue
				}
				reps = append(reps, finding)
			}
			out = append(out, reps...)
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

// sameDefectOverlapN mirrors semantic Rule 2's threshold: explanations sharing
// this many contiguous bytes are describing the same thing.
const sameDefectOverlapN = 60

// canFold reports whether peer may be folded into rep, which is the difference
// between "these two findings are the same defect" and "these two findings
// happen to sit on the same line".
//
// A positive same-defect signal (shared category, a related-category pair, or a
// substantial shared explanation) always permits the fold. Absent that signal,
// a peer may only fold into a representative at least as severe as itself:
// otherwise an unrelated Minor that merely won the confidence tiebreak would
// bury a Medium or Critical, leaving only a CrossRef behind. That is not
// hypothetical — it cost a real review its highest-value finding, where a Minor
// (conf 75) buried two independent Mediums (conf 65, 55) anchored to the same
// one-line diff hunk.
func canFold(rep, peer findings.Finding) bool {
	if sameDefectSignal(rep, peer) {
		return true
	}
	return peer.Severity.Rank() <= rep.Severity.Rank()
}

// sameDefectSignal reports positive evidence that two findings describe one
// defect rather than two neighbours. Reuses semantic's related-category table
// and substring metric so both dedup passes agree on what "related" means.
func sameDefectSignal(a, b findings.Finding) bool {
	if a.Category == b.Category {
		return true
	}
	if relatedCategoryPairs[[2]string{a.Category, b.Category}] {
		return true
	}
	return longestCommonSubstringLen(a.Explanation, b.Explanation) >= sameDefectOverlapN
}

// makeCrossRef snapshots a folded peer finding into the kept finding's CrossRefs
// list. Storing this as a struct (rather than mutating Explanation in place)
// keeps the original specialist explanation pristine — semantic Rule 1's
// file-path check would otherwise trip on a peer file path that an earlier dedup
// pass injected. The CrossRefs list is not rendered into user-facing output (PR
// comments / fallback / summary); it is the audit trail in `consolidated.json`,
// and finalize's accounting pass reads it to reconcile every loaded finding.
func makeCrossRef(other findings.Finding, mergedBy string) findings.CrossRef {
	return findings.CrossRef{
		ID:         other.ID,
		Specialist: other.Specialist,
		Category:   other.Category,
		Severity:   other.Severity,
		Confidence: other.Confidence,
		File:       other.File,
		Line:       other.Line,
		Rationale:  other.Rationale,
		MergedBy:   mergedBy,
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
