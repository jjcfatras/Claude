package dedup

import (
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

func TestPriorReview_DropsContextLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "snippet code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, Snippet: "anything"},
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (want 0/1)", len(kept), len(dropped))
	}
}

func TestPriorReview_KeepsAddedLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "snippet code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, Snippet: "anything"},
		},
	}
	isAdded := func(_ string, _ int) bool { return true }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d (want 1/0)", len(kept), len(dropped))
	}
	if !strings.Contains(kept[0].Explanation, "prior review") {
		t.Errorf("expected prior-review note appended, got: %s", kept[0].Explanation)
	}
}

func TestPriorReview_SnippetMatch(t *testing.T) {
	snippet := strings.Repeat("Z", 50) + " distinctive code here"
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 1000, 80, findings.SeverityMedium, "expl", snippet),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 5, Snippet: "preamble " + snippet + " trail"},
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (snippet should match)", len(kept), len(dropped))
	}
}
