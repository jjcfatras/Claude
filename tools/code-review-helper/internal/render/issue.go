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
func Issue(f findings.Finding, opt IssueOptions) string {
	var b strings.Builder

	if opt.IncludePath {
		fmt.Fprintf(&b, "**%s:%d**\n\n", f.File, f.Line)
	}

	brief := briefDescription(f)
	fmt.Fprintf(&b, "%s **%s** (Confidence: %d/100) - %s\n\n",
		f.Severity.Emoji(), f.Severity, f.Confidence, brief)

	fmt.Fprintf(&b, "**Explanation:** %s\n\n", f.Explanation)

	for _, r := range f.CrossRefs {
		fmt.Fprintf(&b, "_This finding was also independently raised by `%s` (confidence %d) at `%s:%d`._\n\n",
			r.Specialist, r.Confidence, r.File, r.Line)
	}

	fmt.Fprint(&b, "**Code:**\n\n")
	fmt.Fprintf(&b, "```%s\n%s\n```\n", f.Language, strings.TrimRight(f.Code, "\n"))

	if f.SuggestedFix != nil && *f.SuggestedFix != "" {
		fmt.Fprint(&b, "\n**Suggested fix:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", f.Language, strings.TrimRight(*f.SuggestedFix, "\n"))
	}

	return b.String()
}

func briefDescription(f findings.Finding) string {
	if f.Rationale != "" {
		return strings.TrimSpace(f.Rationale)
	}
	return firstSentence(f.Explanation)
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
