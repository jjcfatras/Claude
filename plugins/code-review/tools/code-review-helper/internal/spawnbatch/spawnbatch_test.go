package spawnbatch

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir is the testdata directory shared with cmd/helper's e2e golden
// test, so a fixture edit can't silently desync between unit and e2e tests.
const fixtureDir = "../../testdata/spawn-batch/sample-roster"

func sampleRoster(t *testing.T) Roster {
	t.Helper()
	r, err := LoadRoster(filepath.Join(fixtureDir, "roster.json"))
	if err != nil {
		t.Fatalf("load fixture roster: %v", err)
	}
	return r
}

func sampleAssignments(t *testing.T) map[string]string {
	t.Helper()
	a, err := LoadAssignments(filepath.Join(fixtureDir, "assignments.json"))
	if err != nil {
		t.Fatalf("load fixture assignments: %v", err)
	}
	return a
}

func TestBuild_EmptyRosterRejected(t *testing.T) {
	for _, k := range []Kind{KindTasks, KindAgents, KindFinalize, KindShutdown} {
		_, err := Build(Input{Kind: k, Roster: Roster{TeamName: "code-review-1337"}})
		if err == nil {
			t.Errorf("kind=%v: expected error for empty roster, got nil", k)
		}
		if !strings.Contains(err.Error(), "roster has no members") {
			t.Errorf("kind=%v: expected 'roster has no members', got %v", k, err)
		}
	}
}

func TestBuild_AgentsRequiresAssignments(t *testing.T) {
	_, err := Build(Input{
		Kind:         KindAgents,
		Roster:       sampleRoster(t),
		ReviewTmpDir: "/tmp/x",
		Owner:        "o",
		Repo:         "r",
		PRNumber:     1,
	})
	if err == nil || !strings.Contains(err.Error(), "requires assignments") {
		t.Fatalf("expected assignments error, got %v", err)
	}
}

func TestBuild_AgentsRequiresPerPRScalars(t *testing.T) {
	_, err := Build(Input{
		Kind:        KindAgents,
		Roster:      sampleRoster(t),
		Assignments: sampleAssignments(t),
	})
	if err == nil || !strings.Contains(err.Error(), "review-tmpdir") {
		t.Fatalf("expected per-PR scalars error, got %v", err)
	}
}

func TestBuild_AgentsRejectsMissingAssignment(t *testing.T) {
	assigns := sampleAssignments(t)
	delete(assigns, "claude-md")
	_, err := Build(Input{
		Kind:         KindAgents,
		Roster:       sampleRoster(t),
		Assignments:  assigns,
		ReviewTmpDir: "/tmp/pr-review-XXXXXX",
		Owner:        "owner",
		Repo:         "repo",
		PRNumber:     1337,
	})
	if err == nil || !strings.Contains(err.Error(), "claude-md") {
		t.Fatalf("expected missing-assignment error naming claude-md, got %v", err)
	}
}

func TestBuild_PreservesRosterOrder(t *testing.T) {
	// Use names that don't sort alphabetically so we can detect accidental
	// sorting in the renderer.
	r := Roster{
		TeamName: "code-review-1",
		Members: []Member{
			{Role: "zeta", Name: "zeta-reviewer", SubagentType: "code-review-zeta"},
			{Role: "alpha", Name: "alpha-reviewer", SubagentType: "code-review-alpha"},
			{Role: "mu", Name: "mu-reviewer", SubagentType: "code-review-mu"},
		},
	}
	out, err := Build(Input{Kind: KindTasks, Roster: r})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	zetaIdx := strings.Index(out, "zeta")
	alphaIdx := strings.Index(out, "alpha")
	muIdx := strings.Index(out, "mu")
	if zetaIdx < 0 || alphaIdx < 0 || muIdx < 0 {
		t.Fatalf("missing role in output: %s", out)
	}
	if !(zetaIdx < alphaIdx && alphaIdx < muIdx) {
		t.Errorf("expected order zeta < alpha < mu, got positions %d, %d, %d", zetaIdx, alphaIdx, muIdx)
	}
}

func TestBuild_OutputStartsWithEchoMarker(t *testing.T) {
	out, err := Build(Input{Kind: KindFinalize, Roster: sampleRoster(t)})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(out, EchoMarker+"\n") {
		t.Errorf("expected output to start with EchoMarker + newline, got: %q", out[:min(80, len(out))])
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]Kind{
		"tasks":    KindTasks,
		"agents":   KindAgents,
		"finalize": KindFinalize,
		"shutdown": KindShutdown,
	}
	for s, want := range cases {
		got, err := ParseKind(s)
		if err != nil {
			t.Errorf("ParseKind(%q): unexpected error %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("ParseKind(%q): got %v, want %v", s, got, want)
		}
	}
	if _, err := ParseKind("nonsense"); err == nil {
		t.Error("ParseKind(\"nonsense\"): expected error, got nil")
	}
}

func TestRenderAgents_BakesInTaskID(t *testing.T) {
	out, err := Build(Input{
		Kind:         KindAgents,
		Roster:       sampleRoster(t),
		Assignments:  sampleAssignments(t),
		ReviewTmpDir: "/tmp/pr-review-xxxxxx",
		Owner:        "test-owner",
		Repo:         "test-repo",
		PRNumber:     1337,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The security spawn prompt should include ASSIGNMENT_TASK_ID: 7 (from
	// sampleAssignments).
	if !strings.Contains(out, `ASSIGNMENT_TASK_ID: 7`) {
		t.Error("expected ASSIGNMENT_TASK_ID: 7 in agents output (security role)")
	}
	if !strings.Contains(out, `ASSIGNMENT_TASK_ID: 11`) {
		t.Error("expected ASSIGNMENT_TASK_ID: 11 in agents output (claude-md role)")
	}
	// The Monitor line must appear exactly once at the end.
	if c := strings.Count(out, "Monitor("); c != 1 {
		t.Errorf("expected exactly one Monitor( line, got %d", c)
	}
}

func TestRenderAgents_PromptIsJSONEncoded(t *testing.T) {
	out, err := Build(Input{
		Kind:         KindAgents,
		Roster:       Roster{TeamName: "t", Members: []Member{{Role: "r", Name: "r-reviewer", SubagentType: "code-review-r"}}},
		Assignments:  map[string]string{"r": "1"},
		ReviewTmpDir: "/tmp/x",
		Owner:        "o",
		Repo:         "p",
		PRNumber:     1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The Agent call's prompt argument must be a JSON-encoded string with
	// escaped newlines (\n), not a literal multi-line value — otherwise the
	// lead can't echo it as a single tool_use line.
	if !strings.Contains(out, `prompt: "You are r-reviewer`) {
		t.Errorf("prompt should be a quoted JSON string starting with 'You are r-reviewer', got: %s", out)
	}
	if !strings.Contains(out, `\n\nASSIGNMENT_TASK_ID: 1`) {
		t.Error("expected \\n-escaped newlines in prompt body")
	}
}
