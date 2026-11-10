// Package render produces the markdown bodies used in inline comments and
// the review summary, matching the ISSUE_FORMAT defined in the skill.
package render

import (
	"encoding/base64"
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
// The loader's `validateFinding` already rejects findings with empty
// `rationale`/`explanation`/`code`/`language`, so in practice the empty-field
// branches below are unreachable. They are kept as defense-in-depth against a
// validator regression that would otherwise emit the visible empty-placeholder
// bug observed in https://github.com/FS-Main/fairsquare/pull/1345#pullrequestreview-4232328571.
func Issue(finding findings.Finding, opt IssueOptions) string {
	var b strings.Builder

	if opt.IncludePath {
		fmt.Fprintf(&b, "**%s:%d**\n\n", finding.File, finding.Line)
	}

	brief := briefDescription(finding)
	fmt.Fprintf(&b, "%s **%s** (Confidence: %d/100) - %s\n\n",
		finding.Severity.Emoji(), finding.Severity, finding.Confidence, brief)

	// Hidden machine marker. Invisible on GitHub; the prior-review dedup pass
	// selects this plugin's own comments by this marker (not by any rendered
	// prose), and recovers a comparison snippet from snippet64. See
	// internal/findings.Fingerprint and internal/dedup.matchPrior.
	fmt.Fprintf(&b, "<!-- cr-finding id=\"%s\" snippet64=\"%s\" -->\n\n",
		findings.Fingerprint(finding.File, finding.Line, finding.Category),
		encodeSnippet(finding.Code))

	if finding.Explanation == "" {
		fmt.Fprint(&b, "**Issue & impact:** _(no explanation provided)_\n\n")
	} else {
		fmt.Fprintf(&b, "**Issue & impact:** %s\n\n", finding.Explanation)
	}

	if finding.Code != "" {
		fmt.Fprint(&b, "**Code:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", finding.Language, strings.TrimRight(finding.Code, "\n"))
	}

	if finding.SuggestedFix != nil && *finding.SuggestedFix != "" {
		fmt.Fprint(&b, "\n**Suggested fix:**\n\n")
		fmt.Fprintf(&b, "```%s\n%s\n```\n", finding.Language, strings.TrimRight(*finding.SuggestedFix, "\n"))
	}

	return b.String()
}

// encodeSnippet base64-encodes a bounded, UTF-8-safe prefix of the finding's
// code so the prior-review dedup pass can recover a comparison snippet from the
// hidden marker without parsing the rendered code fence. Bounded to keep the
// marker small; base64 keeps `-->` and newlines out of the comment body.
func encodeSnippet(code string) string {
	const maxSnippetBytes = 256
	if len(code) > maxSnippetBytes {
		code = code[:maxSnippetBytes]
	}
	code = strings.ToValidUTF8(code, "")
	return base64.StdEncoding.EncodeToString([]byte(code))
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
	if i := strings.IndexAny(text, ".\n"); i >= 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}
