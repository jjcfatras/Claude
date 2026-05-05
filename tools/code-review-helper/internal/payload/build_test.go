package payload

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func sampleFinding() findings.Finding {
	fix := "fixed"
	return findings.Finding{
		ID: "f-1", Specialist: "security", Category: "security",
		File: "src/auth/handler.ts", Line: 42,
		Confidence: 90, Severity: findings.SeverityCritical,
		Rationale: "auth bypass", Explanation: "Detailed.",
		Code: "if (true) {}", SuggestedFix: &fix, Language: "ts",
	}
}

func TestBuild_SingleLine(t *testing.T) {
	in := BuildInput{
		HeadSHA: "deadbeef",
		Owner:   "o", Repo: "r",
		InlineEligible: []findings.Finding{sampleFinding()},
		Specialists:    []string{"security"},
	}
	rev := Build(in)
	if rev.CommitID != "deadbeef" || rev.Event != "COMMENT" {
		t.Fatalf("payload header drift: %+v", rev)
	}
	if len(rev.Comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(rev.Comments))
	}
	c := rev.Comments[0]
	if c.Path != "src/auth/handler.ts" || c.Line != 42 || c.Side != "RIGHT" {
		t.Errorf("comment fields drift: %+v", c)
	}
	if c.StartLine != nil {
		t.Errorf("StartLine should be nil for single-line comment")
	}
	if !strings.Contains(c.Body, "🔴 **Critical**") {
		t.Errorf("comment body missing severity line: %s", c.Body)
	}
}

func TestBuild_MultiLine(t *testing.T) {
	f := sampleFinding()
	start := 40
	f.StartLine = &start
	rev := Build(BuildInput{
		HeadSHA: "sha", Owner: "o", Repo: "r",
		InlineEligible: []findings.Finding{f},
	})
	if rev.Comments[0].StartLine == nil || *rev.Comments[0].StartLine != 40 {
		t.Errorf("multi-line: StartLine should be 40")
	}
	if rev.Comments[0].StartSide != "RIGHT" {
		t.Errorf("multi-line: StartSide should be RIGHT")
	}
}

func TestBuild_OnlySummaryOnlyEmptyComments(t *testing.T) {
	rev := Build(BuildInput{
		HeadSHA: "x", Owner: "o", Repo: "r",
		SummaryOnly: []findings.Finding{sampleFinding()},
	})
	if len(rev.Comments) != 0 {
		t.Errorf("summary-only-only should produce empty comments[], got %d", len(rev.Comments))
	}
}

func TestMarshal_RoundTrip(t *testing.T) {
	rev := Build(BuildInput{
		HeadSHA: "sha",
		Owner:   "o", Repo: "r",
		InlineEligible: []findings.Finding{sampleFinding()},
	})
	b, err := MarshalJSON(rev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Review
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CommitID != rev.CommitID || got.Event != rev.Event {
		t.Errorf("round-trip drift")
	}
	// `start_line` must be omitted (omitempty) for single-line — verify the JSON
	// shape directly.
	if strings.Contains(string(b), `"start_line"`) {
		t.Errorf("single-line comment should not emit start_line, got:\n%s", b)
	}
}

func TestFallback_IncludesAllIssues(t *testing.T) {
	in := BuildInput{
		HeadSHA: "sha", Owner: "o", Repo: "r",
		InlineEligible: []findings.Finding{sampleFinding()},
		SummaryOnly: []findings.Finding{
			func() findings.Finding {
				f := sampleFinding()
				f.ID = "f-2"
				f.File = "out.ts"
				f.Line = 1
				return f
			}(),
		},
		Specialists: []string{"security"},
	}
	out := Fallback(in, "API: 422")
	if !strings.Contains(out, "**src/auth/handler.ts:42**") {
		t.Errorf("inline-eligible issue missing from fallback")
	}
	if !strings.Contains(out, "**out.ts:1**") {
		t.Errorf("summary-only issue missing from fallback")
	}
	if !strings.Contains(out, "Inline comments failed (API: 422)") {
		t.Errorf("fallback footer missing")
	}
}

func TestFallback_PlaceholderWhenNoErrorYet(t *testing.T) {
	out := Fallback(BuildInput{
		HeadSHA:        "x",
		InlineEligible: []findings.Finding{sampleFinding()},
	}, "")
	if !strings.Contains(out, "{API_ERROR}") {
		t.Errorf("expected placeholder when apiErr empty (skill substitutes at post-failure)")
	}
}
