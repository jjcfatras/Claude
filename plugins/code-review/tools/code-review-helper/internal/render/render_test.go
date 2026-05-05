package render

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func sample(opts ...func(*findings.Finding)) findings.Finding {
	fix := "const ok = isAuthorized(req);"
	f := findings.Finding{
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
	for _, o := range opts {
		o(&f)
	}
	return f
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
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q\n--- output ---\n%s", w, out)
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
	f := sample(func(f *findings.Finding) { f.SuggestedFix = nil })
	out := Issue(f, IssueOptions{})
	if strings.Contains(out, "Suggested fix") {
		t.Errorf("nil suggested_fix should suppress the section")
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
		"| # | Severity | Confidence | File | Description |",
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
			sample(func(f *findings.Finding) {
				f.ID = "f-2"
				f.File = "out/of/diff.ts"
				f.Line = 999
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
			sample(func(f *findings.Finding) { f.File = "out.ts"; f.Line = 1 }),
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
	f := sample(func(f *findings.Finding) {
		f.StartLine = &start
		f.Line = 45
	})
	out := fileLink(SummaryInput{Owner: "o", Repo: "r", HeadSHA: "sha"}, f)
	if !strings.Contains(out, "[handler.ts:40-45]") {
		t.Errorf("multi-line link missing range: %s", out)
	}
	if !strings.Contains(out, "#L40-L45") {
		t.Errorf("multi-line link missing fragment: %s", out)
	}
}

func TestTableEscape(t *testing.T) {
	if got := tableEscape("a|b"); got != `a\|b` {
		t.Errorf("got %q", got)
	}
	if got := tableEscape("a\nb"); got != "a b" {
		t.Errorf("got %q", got)
	}
}
