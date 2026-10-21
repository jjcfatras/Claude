package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A hook deny and a harness refusal must both land in PermissionDenials, and an
// ordinary command failure must not. The hook case is the regression that
// matters: its deny text is author-defined prose that matches none of the
// permissionRejectMarkers, so before `toolDenialKind` was read, three real
// denials were filed as tool failures and permission_denial_count reported 0
// (transcript 8428d029).
func TestCorrelateAndStat_ClassifiesDenials(t *testing.T) {
	const transcript = `{"type":"assistant","uuid":"u1","timestamp":"2026-08-14T13:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sed -n '1,5p' f.txt"}}]}}
{"type":"user","uuid":"u2","timestamp":"2026-08-14T13:00:01.000Z","toolDenialKind":"permission-rule","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Use Edit for modifications, Read for line ranges — not ` + "`sed … <file>`" + `."}]}}
{"type":"assistant","uuid":"u3","timestamp":"2026-08-14T13:00:02.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"rm -rf /"}}]}}
{"type":"user","uuid":"u4","timestamp":"2026-08-14T13:00:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"The user doesn't want to proceed with this tool use"}]}}
{"type":"assistant","uuid":"u5","timestamp":"2026-08-14T13:00:04.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"grep -n x missing.ts"}}]}}
{"type":"user","uuid":"u6","timestamp":"2026-08-14T13:00:05.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","is_error":true,"content":"Exit code 2\nugrep: warning: missing.ts: No such file or directory"}]}}
`

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	pf, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	events, stats := correlateAndStat(pf)

	if got, want := len(events.PermissionDenials), 2; got != want {
		t.Fatalf("PermissionDenials = %d, want %d: %+v", got, want, events.PermissionDenials)
	}
	if got, want := stats.PermissionDenialCount, 2; got != want {
		t.Errorf("stats.PermissionDenialCount = %d, want %d", got, want)
	}
	// The hook deny is attributed to the structured field, not the prose match.
	if got, want := events.PermissionDenials[0].DenialKind, "permission-rule"; got != want {
		t.Errorf("hook deny DenialKind = %q, want %q", got, want)
	}
	if got, want := events.PermissionDenials[1].DenialKind, "text-match"; got != want {
		t.Errorf("harness refusal DenialKind = %q, want %q", got, want)
	}
	// The missing-file grep is a real failure, not a denial.
	if got, want := len(events.ToolFailures), 1; got != want {
		t.Fatalf("ToolFailures = %d, want %d: %+v", got, want, events.ToolFailures)
	}
	if got := events.ToolFailures[0].ToolUseID; got != "t3" {
		t.Errorf("ToolFailures[0].ToolUseID = %q, want \"t3\"", got)
	}
	if got, want := stats.ToolFailureCount, 1; got != want {
		t.Errorf("stats.ToolFailureCount = %d, want %d", got, want)
	}
}

// api-request-blob entries replay earlier tool_use/tool_result blocks with no
// timestamp; counting them doubled every call and failure (transcript f6147551).
func TestParseFile_IgnoresAPIRequestBlobs(t *testing.T) {
	const transcript = `{"type":"assistant","uuid":"u1","timestamp":"2026-10-20T20:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"false"}}]}}
{"type":"user","uuid":"u2","timestamp":"2026-10-20T20:00:01.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Exit code 1"}]}}
{"type":"api-request-blob","hash":"h","sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"x"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"false"}},{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Exit code 1"}]}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	pf, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	events, stats := correlateAndStat(pf)
	if got := len(events.ToolCalls); got != 1 {
		t.Errorf("ToolCalls = %d, want 1", got)
	}
	if got := stats.ToolFailureCount; got != 1 {
		t.Errorf("ToolFailureCount = %d, want 1", got)
	}
	if got := events.ToolFailures[0].Timestamp; got == "" {
		t.Error("failure lost its timestamp to the blob's replayed tool_result")
	}
}
