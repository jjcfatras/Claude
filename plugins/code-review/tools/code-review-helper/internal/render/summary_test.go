package render

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func TestSummary_NoIssues(t *testing.T) {
	out := Summary(SummaryInput{
		Specialists: []string{"security", "quality", "errors", "perf"},
	})
	if !strings.Contains(out, "No issues found") {
		t.Errorf("missing no-issues line: %s", out)
	}
	if !strings.Contains(out, "errors, perf, quality, security") {
		t.Errorf("specialists should appear sorted: %s", out)
	}
}

func TestSummary_HasInline(t *testing.T) {
	out := Summary(SummaryInput{
		Owner:   "anthropics",
		Repo:    "claude",
		HeadSHA: "abc123def",
		InlineEligible: []findings.Finding{
			sample(),
		},
	})
	wantParts := []string{
		"| # | Severity | Confidence | File |",
		"[handler.ts:42](https://github.com/anthropics/claude/blob/abc123def/src/auth/handler.ts#L42)",
		"See inline comments for full details",
	}
	for _, w := range wantParts {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q", w)
		}
	}
}

func TestSummary_HasInlineAndSummaryOnly(t *testing.T) {
	out := Summary(SummaryInput{
		Owner: "o", Repo: "r", HeadSHA: "deadbeef",
		InlineEligible: []findings.Finding{sample()},
		SummaryOnly: []findings.Finding{
			sample(func(finding *findings.Finding) {
				finding.ID = "f-2"
				finding.File = "out/of/diff.ts"
				finding.Line = 999
			}),
		},
	})
	if !strings.Contains(out, "Additional issues (could not attach inline)") {
		t.Errorf("expected the additional-issues section")
	}
	if !strings.Contains(out, "**out/of/diff.ts:999**") {
		t.Errorf("summary-only entry should include path prefix")
	}
}

func TestSummary_OnlySummaryOnly(t *testing.T) {
	out := Summary(SummaryInput{
		Owner: "o", Repo: "r", HeadSHA: "deadbeef",
		SummaryOnly: []findings.Finding{
			sample(func(finding *findings.Finding) { finding.File = "out.ts"; finding.Line = 1 }),
		},
	})
	if !strings.Contains(out, "Found 1 issue(s)") {
		t.Errorf("missing count line")
	}
	if !strings.Contains(out, "could not be placed as inline comments") {
		t.Errorf("missing summary-only intro")
	}
}

func TestFileLink_MultiLine(t *testing.T) {
	start := 40
	finding := sample(func(finding *findings.Finding) {
		finding.StartLine = &start
		finding.Line = 45
	})
	out := fileLink(SummaryInput{Owner: "o", Repo: "r", HeadSHA: "sha"}, finding)
	if !strings.Contains(out, "[handler.ts:40-45]") {
		t.Errorf("multi-line link missing range: %s", out)
	}
	if !strings.Contains(out, "#L40-L45") {
		t.Errorf("multi-line link missing fragment: %s", out)
	}
}
