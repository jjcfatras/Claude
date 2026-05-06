package findings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadDir_BadFindingSkippedNotFatal(t *testing.T) {
	// One invalid finding (missing id) must not abort the whole load. The
	// finding is dropped, the role is still recorded, and the operator can
	// see the rejection via LoadResult.InvalidFindings.
	dir := t.TempDir()
	writeJSON(t, dir, "quality", RoleFile{
		Specialist: "quality",
		ScanStatus: ScanComplete,
		Findings: []Finding{
			{ID: "", File: "x.ts", Line: 1, Confidence: 50, Severity: SeverityMinor},
		},
	})
	res, err := LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("LoadDir should not error on schema violation, got %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("invalid finding leaked into Findings: %d", len(res.Findings))
	}
	if len(res.InvalidFindings) != 1 {
		t.Fatalf("want 1 invalid finding, got %d", len(res.InvalidFindings))
	}
	inv := res.InvalidFindings[0]
	if inv.Role != "quality" {
		t.Fatalf("want role=quality, got %q", inv.Role)
	}
	if inv.Reason == "" {
		t.Fatalf("Reason should be populated with the validateFinding error")
	}
	// Role still appears in Specialists — it produced output, just bad.
	if len(res.Specialists) != 1 || res.Specialists[0] != "quality" {
		t.Fatalf("want quality in Specialists even with invalid finding, got %v", res.Specialists)
	}
}

func TestLoadDir_MixedValidAndInvalid(t *testing.T) {
	// Repro of the failure mode in transcript 9c2b43de: perf-reviewer
	// produced a single bad finding (missing line). With the old behavior
	// this aborted the whole load and wasted 5 specialists' worth of work.
	// New behavior: bad finding is skipped, valid roles unaffected.
	dir := t.TempDir()
	writeJSON(t, dir, "security", RoleFile{
		Specialist: "security",
		ScanStatus: ScanComplete,
		Findings: []Finding{
			{
				ID: "s-1", Category: "security", File: "src/a.ts", Line: 10,
				Confidence: 80, Severity: SeverityCritical,
				Rationale: "x", Explanation: "y", Code: "z", Language: "ts",
			},
		},
	})
	writeJSON(t, dir, "perf", RoleFile{
		Specialist: "perf",
		ScanStatus: ScanComplete,
		Findings: []Finding{
			// Missing line (zero value) — validation rejects, but the run continues.
			{
				ID: "p-1", Category: "perf", File: "src/b.ts",
				Confidence: 60, Severity: SeverityMedium,
				Rationale: "x", Explanation: "y", Code: "z", Language: "ts",
			},
		},
	})

	res, err := LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("LoadDir should tolerate one invalid finding, got %v", err)
	}
	if len(res.Findings) != 1 || res.Findings[0].ID != "s-1" {
		t.Fatalf("expected only valid security finding, got %+v", res.Findings)
	}
	if len(res.InvalidFindings) != 1 || res.InvalidFindings[0].Role != "perf" {
		t.Fatalf("expected one invalid perf finding, got %+v", res.InvalidFindings)
	}
}

func TestLoadDir_RejectsEmptyContentFields(t *testing.T) {
	// Repro of the failure mode in
	// https://github.com/FS-Main/fairsquare/pull/1345#pullrequestreview-4232328571
	// where a specialist emitted findings with empty rationale/explanation/code
	// and the renderer printed visible empty placeholders. Validator now
	// rejects each of the four required content fields when blank or
	// whitespace-only.
	cases := []struct {
		name   string
		mutate func(*Finding)
		want   string
	}{
		{
			name:   "empty_rationale",
			mutate: func(f *Finding) { f.Rationale = "" },
			want:   "empty rationale",
		},
		{
			name:   "whitespace_rationale",
			mutate: func(f *Finding) { f.Rationale = "  \n  " },
			want:   "empty rationale",
		},
		{
			name:   "empty_explanation",
			mutate: func(f *Finding) { f.Explanation = "" },
			want:   "empty explanation",
		},
		{
			name:   "empty_code",
			mutate: func(f *Finding) { f.Code = "" },
			want:   "empty code",
		},
		{
			name:   "empty_language",
			mutate: func(f *Finding) { f.Language = "" },
			want:   "empty language",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := Finding{
				ID:          "f-1",
				Category:    "security",
				File:        "src/a.ts",
				Line:        10,
				Confidence:  80,
				Severity:    SeverityCritical,
				Rationale:   "x",
				Explanation: "y",
				Code:        "z",
				Language:    "ts",
			}
			tc.mutate(&f)
			writeJSON(t, dir, "security", RoleFile{
				Specialist: "security",
				ScanStatus: ScanComplete,
				Findings:   []Finding{f},
			})
			res, err := LoadDir(dir, nil)
			if err != nil {
				t.Fatalf("LoadDir: %v", err)
			}
			if len(res.Findings) != 0 {
				t.Fatalf("malformed finding leaked: %+v", res.Findings)
			}
			if len(res.InvalidFindings) != 1 {
				t.Fatalf("want 1 invalid finding, got %d", len(res.InvalidFindings))
			}
			inv := res.InvalidFindings[0]
			if inv.Role != "security" || inv.ID != "f-1" {
				t.Fatalf("unexpected invalid record: %+v", inv)
			}
			if !strings.Contains(inv.Reason, tc.want) {
				t.Fatalf("reason %q does not contain %q", inv.Reason, tc.want)
			}
		})
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
