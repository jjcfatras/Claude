// Package payload assembles the JSON payload posted to the GitHub Reviews API
// and the markdown fallback used when that POST fails.
package payload

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/render"
)

type Comment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine *int   `json:"start_line,omitempty"`
	Side      string `json:"side"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

type Review struct {
	CommitID string    `json:"commit_id"`
	Event    string    `json:"event"`
	Body     string    `json:"body"`
	Comments []Comment `json:"comments"`
}

type BuildInput struct {
	HeadSHA        string
	Owner          string
	Repo           string
	InlineEligible []findings.Finding
	SummaryOnly    []findings.Finding
	Specialists    []string
}

// Build constructs the GitHub Reviews API payload from the classification
// result. The summary body is rendered by `render.Summary`; per-comment bodies
// are rendered by `render.Issue` with `IncludePath:false` (GitHub attaches the
// path itself).
func Build(in BuildInput) Review {
	rev := Review{
		CommitID: in.HeadSHA,
		Event:    "COMMENT",
		Body: render.Summary(render.SummaryInput{
			Owner:          in.Owner,
			Repo:           in.Repo,
			HeadSHA:        in.HeadSHA,
			InlineEligible: in.InlineEligible,
			SummaryOnly:    in.SummaryOnly,
			Specialists:    in.Specialists,
		}),
		Comments: make([]Comment, 0, len(in.InlineEligible)),
	}
	for _, f := range in.InlineEligible {
		c := Comment{
			Path: f.File,
			Line: f.Line,
			Side: "RIGHT",
			Body: render.Issue(f, render.IssueOptions{}),
		}
		if f.StartLine != nil && *f.StartLine != f.Line {
			start := *f.StartLine
			c.StartLine = &start
			c.StartSide = "RIGHT"
		}
		rev.Comments = append(rev.Comments, c)
	}
	return rev
}

// MarshalJSON returns the payload formatted for `gh api ... --input`.
func MarshalJSON(rev Review) ([]byte, error) {
	return json.MarshalIndent(rev, "", "  ")
}

// Fallback renders the markdown body for `gh pr comment NUMBER -F`. Used when
// the Reviews API call fails. Lists every issue (inline + summary-only) using
// ISSUE_FORMAT with a `**path:line**` prefix. The API error (if known) is
// surfaced in the footer.
//
// Important: the skill writes this file at finalize time before the API call
// has been attempted, so `apiErr` is empty by default. The skill substitutes
// the actual error into the footer at post-failure time using simple text
// replacement on the placeholder `{API_ERROR}`.
func Fallback(in BuildInput, apiErr string) string {
	var b strings.Builder
	b.WriteString("### Code review\n\n")

	all := append(append([]findings.Finding(nil), in.InlineEligible...), in.SummaryOnly...)
	if len(all) == 0 {
		b.WriteString("No issues found.\n\n")
	} else {
		b.WriteString("Inline comment posting failed. All issues listed below.\n\n")
		for _, f := range all {
			b.WriteString(render.Issue(f, render.IssueOptions{IncludePath: true}))
			b.WriteString("\n")
		}
	}
	footerErr := apiErr
	if footerErr == "" {
		footerErr = "{API_ERROR}"
	}
	b.WriteString(fmt.Sprintf("\n_Note: Inline comments failed (%s)._\n\n", strings.TrimSpace(footerErr)))
	b.WriteString("🤖 Generated with [Claude Code](https://claude.ai/code)\n\n<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>\n")
	return b.String()
}
