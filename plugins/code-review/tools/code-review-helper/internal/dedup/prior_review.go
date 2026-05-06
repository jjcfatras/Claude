package dedup

import (
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// PriorIssue is the schema persisted by the skill's step 1c (prep agent for
// prior-review fetching). Mirrors the rubric's `prior-issues.json` shape.
type PriorIssue struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	StartLine   int    `json:"start_line"`
	Snippet     string `json:"snippet"`
	Description string `json:"description"`
}

type PriorIssuesFile struct {
	LastReviewDate   *string      `json:"last_review_date"`
	LastReviewCommit *string      `json:"last_review_commit"`
	Issues           []PriorIssue `json:"issues"`
}

const (
	priorLineWindow      = 5
	priorSnippetOverlapN = 40
	priorReviewKeptNote  = "\n\n_Note: This issue was flagged in a prior review but the code has since changed._"
)

// addedLineLookup mirrors diff.Parsed.IsAddedLine; injected so this package
// has no dependency on internal/diff.
type addedLineLookup func(path string, line int) bool

// PriorReview filters `in` against `prior`. Three buckets:
//
//   - Findings matched + on a context (unchanged) line → DROPPED.
//   - Findings matched + on an added line → KEPT with explanation note.
//   - Findings unmatched → KEPT unchanged.
//
// Returns (kept, dropped). Dropped findings carry a `description` derived from
// the prior issue, which the skill uses to render the "Skipped N issues"
// summary.
func PriorReview(in []findings.Finding, prior PriorIssuesFile, isAdded addedLineLookup) (kept []findings.Finding, dropped []findings.Finding) {
	for _, finding := range in {
		match, ok := matchPrior(finding, prior.Issues)
		if !ok {
			kept = append(kept, finding)
			continue
		}
		if isAdded(finding.File, finding.Line) {
			finding.Explanation += priorReviewKeptNote
			kept = append(kept, finding)
			continue
		}
		_ = match
		dropped = append(dropped, finding)
	}
	return kept, dropped
}

func matchPrior(finding findings.Finding, prior []PriorIssue) (PriorIssue, bool) {
	for _, priorIssue := range prior {
		if priorIssue.Path != finding.File {
			continue
		}
		if abs(priorIssue.Line-finding.Line) <= priorLineWindow {
			return priorIssue, true
		}
		if longestCommonSubstringLen(priorIssue.Snippet, finding.Code) >= priorSnippetOverlapN {
			return priorIssue, true
		}
	}
	return PriorIssue{}, false
}
