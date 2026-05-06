package render

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func sample(opts ...func(*findings.Finding)) findings.Finding {
	fix := "const ok = isAuthorized(req);"
	finding := findings.Finding{
		ID:           "f-1",
		Specialist:   "security",
		Category:     "security",
		File:         "src/auth/handler.ts",
		Line:         42,
		Confidence:   85,
		Severity:     findings.SeverityCritical,
		Rationale:    "auth bypass on the happy path",
		Explanation:  "The middleware does not check `req.user.role` before granting access to admin endpoints.",
		Code:         "if (req.user) return next();",
		SuggestedFix: &fix,
		Language:     "ts",
	}
	for _, applyOpt := range opts {
		applyOpt(&finding)
	}
	return finding
}

func TestIssue_FullFormat(t *testing.T) {
	out := Issue(sample(), IssueOptions{})
	want := []string{
		"🔴 **Critical** (Confidence: 85/100) - auth bypass on the happy path",
		"**Explanation:** The middleware does not check",
		"**Code:**",
		"```ts\nif (req.user) return next();\n```",
		"**Suggested fix:**",
		"```ts\nconst ok = isAuthorized(req);\n```",
	}
	for _, wantPart := range want {
		if !strings.Contains(out, wantPart) {
			t.Errorf("missing %q\n--- output ---\n%s", wantPart, out)
		}
	}
	if strings.Contains(out, "**src/auth/handler.ts:") {
		t.Errorf("default render should NOT include path prefix")
	}
}

func TestIssue_WithPathPrefix(t *testing.T) {
	out := Issue(sample(), IssueOptions{IncludePath: true})
	if !strings.HasPrefix(out, "**src/auth/handler.ts:42**") {
		t.Errorf("expected path prefix, got: %s", out[:80])
	}
}

func TestIssue_NoSuggestedFix(t *testing.T) {
	finding := sample(func(finding *findings.Finding) { finding.SuggestedFix = nil })
	out := Issue(finding, IssueOptions{})
	if strings.Contains(out, "Suggested fix") {
		t.Errorf("nil suggested_fix should suppress the section")
	}
}

func TestIssue_EmptyCodeOmitsBlock(t *testing.T) {
	// Defense-in-depth: validator already rejects empty code, but if a
	// regression bypassed it, the renderer must not emit the visible empty
	// fenced block observed in PR #1345.
	finding := sample(func(finding *findings.Finding) { finding.Code = "" })
	out := Issue(finding, IssueOptions{})
	if strings.Contains(out, "**Code:**") {
		t.Errorf("empty Code should suppress the Code section, got:\n%s", out)
	}
	if strings.Contains(out, "```ts\n\n```") {
		t.Errorf("empty Code should not emit an empty fenced block, got:\n%s", out)
	}
}

func TestIssue_EmptyExplanationPlaceholder(t *testing.T) {
	finding := sample(func(finding *findings.Finding) { finding.Explanation = "" })
	out := Issue(finding, IssueOptions{})
	if !strings.Contains(out, "_(no explanation provided)_") {
		t.Errorf("empty Explanation should produce the placeholder, got:\n%s", out)
	}
	if strings.Contains(out, "**Explanation:** \n\n") {
		t.Errorf("empty Explanation should not render as a bare label, got:\n%s", out)
	}
}

func TestIssue_EmptyRationaleAndExplanationFallback(t *testing.T) {
	finding := sample(func(finding *findings.Finding) {
		finding.Rationale = ""
		finding.Explanation = ""
	})
	out := Issue(finding, IssueOptions{})
	if !strings.Contains(out, "(Confidence: 85/100) - (no description)") {
		t.Errorf("brief should fall back to (no description), got:\n%s", out)
	}
}

func TestIssue_RationaleFallbackToFirstSentenceOfExplanation(t *testing.T) {
	finding := sample(func(finding *findings.Finding) {
		finding.Rationale = ""
		finding.Explanation = "First sentence here. Second sentence ignored."
	})
	out := Issue(finding, IssueOptions{})
	if !strings.Contains(out, "(Confidence: 85/100) - First sentence here") {
		t.Errorf("brief should fall back to firstSentence(explanation), got:\n%s", out)
	}
}

func TestIssue_DoesNotRenderCrossRefs(t *testing.T) {
	// CrossRefs are dedup scaffolding for the lead's audit trail in
	// consolidated.json. They must NOT leak into rendered findings (inline
	// comments, summary section, fallback markdown) — the PR author should see
	// only information about the finding itself. Regression for
	// https://github.com/FS-Main/fairsquare/pull/1345#discussion_r3197489250.
	finding := sample(func(finding *findings.Finding) {
		finding.CrossRefs = []findings.CrossRef{
			{Specialist: "quality", Confidence: 70, File: "src/auth/handler.ts", Line: 44},
			{Specialist: "errors", Confidence: 65, File: "src/auth/other.ts", Line: 12},
		}
	})
	for _, opt := range []IssueOptions{{}, {IncludePath: true}} {
		out := Issue(finding, opt)
		if strings.Contains(out, "independently raised") {
			t.Errorf("opt=%+v: rendered output must not mention 'independently raised', got:\n%s", opt, out)
		}
		for _, peer := range []string{"quality", "errors", "src/auth/other.ts"} {
			if strings.Contains(out, peer) {
				t.Errorf("opt=%+v: rendered output leaked CrossRef peer %q, got:\n%s", opt, peer, out)
			}
		}
	}
}

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
