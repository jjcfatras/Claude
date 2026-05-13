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
	comment := rev.Comments[0]
	if comment.Path != "src/auth/handler.ts" || comment.Line != 42 || comment.Side != "RIGHT" {
		t.Errorf("comment fields drift: %+v", comment)
	}
	if comment.StartLine != nil {
		t.Errorf("StartLine should be nil for single-line comment")
	}
	if !strings.Contains(comment.Body, "🔴 **Critical**") {
		t.Errorf("comment body missing severity line: %s", comment.Body)
	}
}

func TestBuild_MultiLine(t *testing.T) {
	finding := sampleFinding()
	start := 40
	finding.StartLine = &start
	rev := Build(BuildInput{
		HeadSHA: "sha", Owner: "o", Repo: "r",
		InlineEligible: []findings.Finding{finding},
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
	data, err := MarshalJSON(rev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Review
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CommitID != rev.CommitID || got.Event != rev.Event {
		t.Errorf("round-trip drift")
	}
	// `start_line` must be omitted (omitempty) for single-line — verify the JSON
	// shape directly.
	if strings.Contains(string(data), `"start_line"`) {
		t.Errorf("single-line comment should not emit start_line, got:\n%s", data)
	}
}

func TestFallback_IncludesAllIssues(t *testing.T) {
	in := BuildInput{
		HeadSHA: "sha", Owner: "o", Repo: "r",
		InlineEligible: []findings.Finding{sampleFinding()},
		SummaryOnly: []findings.Finding{
			func() findings.Finding {
				finding := sampleFinding()
				finding.ID = "f-2"
				finding.File = "out.ts"
				finding.Line = 1
				return finding
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

func TestBuildPending_OmitsEvent(t *testing.T) {
	in := BuildInput{
		HeadSHA: "sha", Owner: "o", Repo: "r",
		InlineEligible: []findings.Finding{sampleFinding()},
		Specialists:    []string{"security"},
	}
	rev := BuildPending(in)
	if rev.Event != "" {
		t.Errorf("BuildPending must leave Event empty, got %q", rev.Event)
	}
	// The rest of the payload must match Build except for Event — same commit_id,
	// same body, same comments. The two-step fallback relies on this equivalence
	// (the lead doesn't reconstruct anything between attempts).
	full := Build(in)
	if rev.CommitID != full.CommitID {
		t.Errorf("CommitID drift between Build and BuildPending: %q vs %q", rev.CommitID, full.CommitID)
	}
	if rev.Body != full.Body {
		t.Errorf("Body drift between Build and BuildPending")
	}
	if len(rev.Comments) != len(full.Comments) {
		t.Errorf("Comments length drift: %d vs %d", len(rev.Comments), len(full.Comments))
	}

	data, err := MarshalJSON(rev)
	if err != nil {
		t.Fatalf("marshal pending: %v", err)
	}
	if strings.Contains(string(data), `"event"`) {
		t.Errorf("pending payload must not include the \"event\" key, got:\n%s", data)
	}
}

func TestBodyOnly_MarshalShape(t *testing.T) {
	data, err := json.MarshalIndent(BodyOnly{Body: "## Hello\n"}, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"body"`) {
		t.Errorf("BodyOnly must emit a body key, got:\n%s", got)
	}
	if strings.Contains(got, `"event"`) || strings.Contains(got, `"comments"`) || strings.Contains(got, `"commit_id"`) {
		t.Errorf("BodyOnly must emit ONLY the body key, got:\n%s", got)
	}
}
