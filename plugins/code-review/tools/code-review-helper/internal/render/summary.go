package render

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

const summaryFooter = `🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>`

type SummaryInput struct {
	Owner          string
	Repo           string
	HeadSHA        string
	InlineEligible []findings.Finding
	SummaryOnly    []findings.Finding
	Specialists    []string
}

// Summary returns the review summary body markdown that GitHub posts as the
// top-level review body. Picks one of three layouts based on which buckets
// have content.
func Summary(in SummaryInput) string {
	switch {
	case len(in.InlineEligible) == 0 && len(in.SummaryOnly) == 0:
		return summaryNoIssues(in.Specialists)
	case len(in.InlineEligible) == 0:
		return summaryOnlyAll(in)
	default:
		return summaryWithInline(in)
	}
}

func summaryNoIssues(specialists []string) string {
	roles := append([]string(nil), specialists...)
	sort.Strings(roles)
	return fmt.Sprintf("### Code review\n\nNo issues found. Reviewed by: %s.\n\n%s",
		strings.Join(roles, ", "), summaryFooter)
}

func summaryWithInline(in SummaryInput) string {
	var b strings.Builder
	b.WriteString("### Code review\n\n")
	b.WriteString("| # | Severity | Confidence | File |\n")
	b.WriteString("| - | -------- | ---------- | ---- |\n")
	for i, f := range in.InlineEligible {
		fmt.Fprintf(&b, "| %d | %s %s | %d | %s |\n",
			i+1,
			f.Severity.Emoji(), f.Severity,
			f.Confidence,
			fileLink(in, f),
		)
	}
	b.WriteString("\nSee inline comments for full details, code examples, and suggested fixes.\n")

	if len(in.SummaryOnly) > 0 {
		b.WriteString("\n#### Additional issues (could not attach inline)\n\n")
		for _, f := range in.SummaryOnly {
			b.WriteString(Issue(f, IssueOptions{IncludePath: true}))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(summaryFooter)
	return b.String()
}

func summaryOnlyAll(in SummaryInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Code review\n\nFound %d issue(s). These could not be placed as inline comments because their line numbers fall outside the diff's visible range.\n\n",
		len(in.SummaryOnly))
	for _, f := range in.SummaryOnly {
		b.WriteString(Issue(f, IssueOptions{IncludePath: true}))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(summaryFooter)
	return b.String()
}

func fileLink(in SummaryInput, f findings.Finding) string {
	base := path.Base(f.File)
	if f.StartLine != nil && *f.StartLine != f.Line {
		return fmt.Sprintf("[%s:%d-%d](https://github.com/%s/%s/blob/%s/%s#L%d-L%d)",
			base, *f.StartLine, f.Line, in.Owner, in.Repo, in.HeadSHA, f.File, *f.StartLine, f.Line)
	}
	return fmt.Sprintf("[%s:%d](https://github.com/%s/%s/blob/%s/%s#L%d)",
		base, f.Line, in.Owner, in.Repo, in.HeadSHA, f.File, f.Line)
}
