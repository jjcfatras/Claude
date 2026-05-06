// Package render produces the markdown bodies used in inline comments and
// the review summary, matching the ISSUE_FORMAT defined in the skill.
package render

import (
	"fmt"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// IssueOptions controls how a finding is rendered.
type IssueOptions struct {
	// IncludePath: if true, prepend the issue body with `**path:line**`. Used
	// for the fallback markdown and for summary-only blocks in the review body.
	// For inline comments leave this false (GitHub already attaches the path).
	IncludePath bool
}

// Issue renders one finding in ISSUE_FORMAT.
//
// Defense-in-depth note: the loader's `validateFinding` already rejects
// findings with empty `rationale`/`explanation`/`code`/`language`, so in
// practice the empty-field branches below are unreachable. They exist so any
// future regression that bypasses the validator degrades to readable output
// instead of the visible empty-placeholder bug observed in
// https://github.com/FS-Main/fairsquare/pull/1345#pullrequestreview-4232328571.
func Issue(finding findings.Finding, opt IssueOptions) string {
	var b strings.Builder

	if opt.IncludePath {
		fmt.Fprintf(&b, "**%s:%d**\n\n", finding.File, finding.Line)
	}

	brief := briefDescription(finding)
	fmt.Fprintf(&b, "%s **%s** (Confidence: %d/100) - %s\n\n",
		finding.Severity.Emoji(), finding.Severity, finding.Confidence, brief)

	if strings.TrimSpace(finding.Explanation) == "" {
		fmt.Fprint(&b, "**Explanation:** _(no explanation provided)_\n\n")
	} else {
		fmt.Fprintf(&b, "**Explanation:** %s\n\n", finding.Explanation)
	}

	if strings.TrimSpace(finding.Code) != "" {
		fmt.Fprint(&b, "**Code:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", finding.Language, strings.TrimRight(finding.Code, "\n"))
	}

	if finding.SuggestedFix != nil && *finding.SuggestedFix != "" {
		fmt.Fprint(&b, "\n**Suggested fix:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", finding.Language, strings.TrimRight(*finding.SuggestedFix, "\n"))
	}

	return b.String()
}

func briefDescription(finding findings.Finding) string {
	if rationale := strings.TrimSpace(finding.Rationale); rationale != "" {
		return rationale
	}
	if sentence := firstSentence(finding.Explanation); sentence != "" {
		return sentence
	}
	return "(no description)"
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	for i, r := range text {
		if r == '.' || r == '\n' {
			return strings.TrimSpace(text[:i])
		}
	}
	return text
}
