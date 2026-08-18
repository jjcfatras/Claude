package main

import (
	"slices"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func conflictFinding(id, file string, line int, code, explanation string) findings.Finding {
	return findings.Finding{
		ID:          id,
		File:        file,
		Line:        line,
		Code:        code,
		Explanation: explanation,
		Severity:    findings.SeverityMedium,
		Confidence:  60,
	}
}

// TestDetectConflicts_RealTranscriptShape reproduces PR #1784 (transcript
// db83dbda): `infra` flagged devops_account's missing default as the defect at
// variables.tf:83, while `quality` and `security` at :87/:88 both cited that
// same missing default as the correct fail-loud pattern to copy. Three findings
// survived the gate and two posted as inline comments giving opposite advice.
func TestDetectConflicts_RealTranscriptShape(t *testing.T) {
	infra := conflictFinding("infra-1", "variables.tf", 83,
		"variable \"devops_account\" {\n  type = string\n}",
		"The new devops_account variable has no default and is required by the new opensearch provider config. If any workspace's TFC variables weren't updated alongside this PR, that app's next automated apply fails outright.")
	quality := conflictFinding("quality-1", "variables.tf", 87,
		"variable \"teams_destination_id\" {\n  default = \"nAV_2Z8BHN5SG3lzOOaq\"\n}",
		"A hard-coded environment-specific default. devops_account, added immediately above in the same hunk, uses the fail-loud default-less form.")

	got := detectConflicts([]findings.Finding{infra, quality})
	if len(got) != 1 {
		t.Fatalf("want 1 potential conflict, got %d: %+v", len(got), got)
	}
	if got[0].Identifier != "devops_account" {
		t.Errorf("identifier = %q, want devops_account", got[0].Identifier)
	}
	if got[0].IDA != "infra-1" || got[0].IDB != "quality-1" {
		t.Errorf("pair = (%s, %s), want (infra-1, quality-1)", got[0].IDA, got[0].IDB)
	}
}

func TestDetectConflicts_Negatives(t *testing.T) {
	tests := []struct {
		name string
		in   []findings.Finding
	}{
		{
			// Far apart: no reason to think they discuss the same code.
			name: "outside the line window",
			in: []findings.Finding{
				conflictFinding("a", "x.tf", 10, "variable \"devops_account\" {}", "about devops_account"),
				conflictFinding("b", "x.tf", 40, "other_thing = 1", "mentions devops_account too"),
			},
		},
		{
			name: "different files",
			in: []findings.Finding{
				conflictFinding("a", "x.tf", 10, "variable \"devops_account\" {}", "a"),
				conflictFinding("b", "y.tf", 11, "y = 1", "mentions devops_account"),
			},
		},
		{
			// Both anchor on the same symbol — that is shared context, which line
			// proximity already conveys, not one finding discussing the other's.
			name: "identifier present in both snippets",
			in: []findings.Finding{
				conflictFinding("a", "x.tf", 10, "variable \"devops_account\" {}", "a"),
				conflictFinding("b", "x.tf", 11, "devops_account = var.devops_account", "mentions devops_account"),
			},
		},
		{
			// Only single-word tokens overlap; those are not distinctive enough
			// to claim one finding is reasoning about the other's subject.
			name: "no compound identifier shared",
			in: []findings.Finding{
				conflictFinding("a", "x.tf", 10, "variable \"region\" {}", "a"),
				conflictFinding("b", "x.tf", 11, "count = 2", "the region is wrong"),
			},
		},
		{
			name: "single finding",
			in: []findings.Finding{
				conflictFinding("a", "x.tf", 10, "variable \"devops_account\" {}", "a"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectConflicts(tc.in); len(got) != 0 {
				t.Errorf("want no conflicts, got %+v", got)
			}
		})
	}
}

// TestProjectIdentifiers_ExcludesKeywords guards the filter that keeps this
// advisory signal from crying wolf: language keywords and prose words share a
// snippet with every finding, so only compound symbols count.
func TestProjectIdentifiers_ExcludesKeywords(t *testing.T) {
	code := "function handleRequest(const value) { return user_id; } // variable string"
	got := projectIdentifiers(code)
	want := map[string]bool{"handleRequest": true, "user_id": true}
	for _, id := range got {
		if !want[id] {
			t.Errorf("projectIdentifiers returned %q, which is not a compound project symbol", id)
		}
	}
	for id := range want {
		if !slices.Contains(got, id) {
			t.Errorf("projectIdentifiers missed %q; got %v", id, got)
		}
	}
}
