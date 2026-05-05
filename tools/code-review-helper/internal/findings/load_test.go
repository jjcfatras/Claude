package findings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, dir, role string, payload any) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", role, err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".json"), b, 0o644); err != nil {
		t.Fatalf("write %s: %v", role, err)
	}
}

func TestLoadDir_MixedRoles(t *testing.T) {
	dir := t.TempDir()

	startLine := 40
	writeJSON(t, dir, "security", RoleFile{
		Specialist: "security",
		ScanStatus: ScanComplete,
		Findings: []Finding{
			{
				ID:          "s-1",
				Category:    "security",
				File:        "src/auth/handler.ts",
				Line:        42,
				StartLine:   &startLine,
				Confidence:  80,
				Severity:    SeverityCritical,
				Rationale:   "hardcoded auth bypass",
				Explanation: "explained",
				Code:        "const ok = true;",
				Language:    "ts",
			},
		},
	})

	writeJSON(t, dir, "perf", RoleFile{
		Specialist: "perf",
		ScanStatus: ScanTimedOut,
		Findings: []Finding{
			{
				ID:          "p-1",
				Category:    "perf",
				File:        "src/api/list.ts",
				Line:        10,
				Confidence:  60,
				Severity:    SeverityMedium,
				Rationale:   "n+1 query",
				Explanation: "nested loop hits db",
				Code:        "for (...) await db.find()",
				Language:    "ts",
			},
		},
	})

	res, err := LoadDir(dir, []string{"security", "perf", "react"})
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(res.Findings))
	}
	if res.Findings[0].Specialist == "" {
		t.Fatalf("specialist field not populated by loader")
	}
	if len(res.TimedOutRoles) != 1 || res.TimedOutRoles[0] != "perf" {
		t.Fatalf("expected perf in timed-out roles, got %v", res.TimedOutRoles)
	}
	if len(res.MissingRoles) != 1 || res.MissingRoles[0] != "react" {
		t.Fatalf("expected react in missing roles, got %v", res.MissingRoles)
	}
}

func TestLoadDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	res, err := LoadDir(dir, []string{"security"})
	if err != nil {
		t.Fatalf("LoadDir on empty: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("want 0, got %d", len(res.Findings))
	}
	if len(res.MissingRoles) != 1 {
		t.Fatalf("expected security marked missing, got %v", res.MissingRoles)
	}
}

func TestLoadDir_BadFinding(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, dir, "quality", RoleFile{
		Specialist: "quality",
		ScanStatus: ScanComplete,
		Findings: []Finding{
			{ID: "", File: "x.ts", Line: 1, Confidence: 50, Severity: SeverityMinor},
		},
	})
	if _, err := LoadDir(dir, nil); err == nil {
		t.Fatalf("expected validation error for missing id")
	}
}

func TestLoadDir_RoundTripRubricExample(t *testing.T) {
	// Verbatim from ~/.claude/references/code-review-rubrics.md "Findings file schema".
	const example = `{
        "specialist": "security",
        "scan_status": "complete",
        "findings": [
          {
            "id": "f-1",
            "category": "security",
            "file": "src/auth/handler.ts",
            "line": 42,
            "startLine": null,
            "confidence": 75,
            "severity": "Critical",
            "rationale": "One-sentence justification for confidence and severity.",
            "explanation": "Detailed explanation of why this is an issue and its impact.",
            "code": "the problematic code from the PR (multi-line allowed)",
            "suggested_fix": "example of how to fix it (or null if not applicable)",
            "language": "ts",
            "verifications": [
              {
                "asked": "typescript",
                "verdict": "confirmed",
                "note": "TS reviewer agrees: assertion bypasses narrowing.",
                "applied_adjustment": 25
              }
            ]
          }
        ]
      }`
	var rf RoleFile
	if err := json.Unmarshal([]byte(example), &rf); err != nil {
		t.Fatalf("rubric example failed to unmarshal: %v", err)
	}
	if len(rf.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(rf.Findings))
	}
	f := rf.Findings[0]
	if f.Confidence != 75 || f.Severity != SeverityCritical {
		t.Fatalf("confidence/severity drift: %d %s", f.Confidence, f.Severity)
	}
	if f.StartLine != nil {
		t.Fatalf("StartLine should decode to nil for JSON null")
	}
	if len(f.Verifications) != 1 || f.Verifications[0].Verdict != VerdictConfirmed {
		t.Fatalf("verifications drift: %+v", f.Verifications)
	}
}
