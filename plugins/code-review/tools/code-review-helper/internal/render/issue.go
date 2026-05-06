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
func Issue(f findings.Finding, opt IssueOptions) string {
	var b strings.Builder

	if opt.IncludePath {
		fmt.Fprintf(&b, "**%s:%d**\n\n", f.File, f.Line)
	}

	brief := briefDescription(f)
	fmt.Fprintf(&b, "%s **%s** (Confidence: %d/100) - %s\n\n",
		f.Severity.Emoji(), f.Severity, f.Confidence, brief)

	if strings.TrimSpace(f.Explanation) == "" {
		fmt.Fprint(&b, "**Explanation:** _(no explanation provided)_\n\n")
	} else {
		fmt.Fprintf(&b, "**Explanation:** %s\n\n", f.Explanation)
	}

	for _, r := range f.CrossRefs {
		fmt.Fprintf(&b, "_This finding was also independently raised by `%s` (confidence %d) at `%s:%d`._\n\n",
			r.Specialist, r.Confidence, r.File, r.Line)
	}

	if strings.TrimSpace(f.Code) != "" {
		fmt.Fprint(&b, "**Code:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", f.Language, strings.TrimRight(f.Code, "\n"))
	}

	if f.SuggestedFix != nil && *f.SuggestedFix != "" {
		fmt.Fprint(&b, "\n**Suggested fix:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", f.Language, strings.TrimRight(*f.SuggestedFix, "\n"))
	}

	return b.String()
}

func briefDescription(f findings.Finding) string {
	if r := strings.TrimSpace(f.Rationale); r != "" {
		return r
	}
	if s := firstSentence(f.Explanation); s != "" {
		return s
	}
	return "(no description)"
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == '.' || r == '\n' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}
