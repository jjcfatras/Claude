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

// Workflow agents live in <session>/subagents/workflows/<runId>/ with no Agent
// spawn pointing at them; the parser used to report 0 agents for a 7-agent run
// (transcript 255adf1a). One API response split across two jsonl lines must
// count once: each line repeats the response's usage block.
func TestParseSession_WorkflowRuns(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "s.jsonl")
	runDir := filepath.Join(root, "s", "subagents", "workflows", "wf_1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		session: `{"type":"assistant","uuid":"m1","timestamp":"2026-11-17T20:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"w1","name":"Workflow","input":{"name":"x"}}]}}
`,
		filepath.Join(runDir, "agent-a1.meta.json"): `{"agentType":"p:errors","description":"errors","workflowPhase":"Review","pickedEffortModel":"claude-opus-5-5"}`,
		filepath.Join(runDir, "agent-a1.jsonl"): `{"type":"assistant","uuid":"u1","isSidechain":true,"timestamp":"2026-11-17T20:00:01.000Z","message":{"id":"msg_1","model":"claude-sonnet-5-5","role":"assistant","content":[{"type":"thinking","thinking":"t"}],"usage":{"input_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":10,"output_tokens":5}}}
{"type":"assistant","uuid":"u2","isSidechain":true,"timestamp":"2026-11-17T20:00:01.500Z","message":{"id":"msg_1","model":"claude-sonnet-5-5","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"git grep x -A8 sha"}}],"usage":{"input_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":10,"output_tokens":5}}}
{"type":"user","uuid":"u3","isSidechain":true,"timestamp":"2026-11-17T20:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"fatal: unable to resolve revision: -A8"}]}}
`,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sess, err := parseSession(session)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}
	if len(sess.WorkflowRuns) != 1 || len(sess.WorkflowRuns[0].Agents) != 1 {
		t.Fatalf("WorkflowRuns = %+v, want 1 run with 1 agent", sess.WorkflowRuns)
	}
	run, agent := sess.WorkflowRuns[0], sess.WorkflowRuns[0].Agents[0]
	if run.RunID != "wf_1" || agent.AgentType != "p:errors" || agent.Phase != "Review" {
		t.Errorf("run/agent identity = %q %q %q", run.RunID, agent.AgentType, agent.Phase)
	}
	if len(agent.Models) != 1 || agent.Models[0] != "claude-sonnet-5-5" {
		t.Errorf("Models = %v, want [claude-sonnet-5-5] from message.model, not the meta", agent.Models)
	}
	if agent.APIResponses != 1 || agent.InputTokens != 112 || agent.OutputTokens != 5 {
		t.Errorf("responses/in/out = %d/%d/%d, want 1/112/5", agent.APIResponses, agent.InputTokens, agent.OutputTokens)
	}
	if sess.Stats.SidechainToolCalls != 1 || sess.Stats.ToolFailureCount != 1 {
		t.Errorf("rolled-up sidechain calls/failures = %d/%d, want 1/1", sess.Stats.SidechainToolCalls, sess.Stats.ToolFailureCount)
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
