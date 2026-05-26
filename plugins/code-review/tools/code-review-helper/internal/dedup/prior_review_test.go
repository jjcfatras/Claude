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

func TestPriorReview_DescriptionOverlap_DropsContextLine(t *testing.T) {
	// Distinctive 70-char string lives only in the prior description and the
	// new finding's explanation — line and snippet both fail to match.
	overlap := "O(N_events) table scan in a hot Salesforce-facing path with two SQL queries"
	in := []findings.Finding{
		mkFinding("a", "perf", "perf", "src/x.ts", 899, 80, findings.SeverityMedium, "🟡 Medium - "+overlap+" - drop the limit:500 fan-out", "unrelated code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 886, Description: "🟡 Medium - " + overlap + " - prior review wording"},
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (description overlap should match)", len(kept), len(dropped))
	}
}

func TestPriorReview_AuthorDismissed_DropsEvenOnAddedLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "perf", "perf", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, AuthorDismissed: true},
		},
	}
	isAdded := func(_ string, _ int) bool { return true }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (author-dismissed should drop even on added line)", len(kept), len(dropped))
	}
}

func TestPriorReview_Resolved_DropsEvenOnAddedLine(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "perf", "perf", "src/x.ts", 50, 80, findings.SeverityMedium, "expl", "code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 51, IsResolved: true},
		},
	}
	isAdded := func(_ string, _ int) bool { return true }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d (resolved should drop even on added line)", len(kept), len(dropped))
	}
}

func TestPriorReview_NoMatch_KeepsAll(t *testing.T) {
	in := []findings.Finding{
		mkFinding("a", "security", "security", "src/x.ts", 50, 80, findings.SeverityMedium, "totally different topic", "code"),
	}
	prior := PriorIssuesFile{
		Issues: []PriorIssue{
			{Path: "src/x.ts", Line: 500, Description: "an unrelated concern about something else entirely"},
			{Path: "src/y.ts", Line: 51, AuthorDismissed: true}, // different file — must not match
		},
	}
	isAdded := func(_ string, _ int) bool { return false }
	kept, dropped := PriorReview(in, prior, isAdded)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d (no overlap, should keep all)", len(kept), len(dropped))
	}
}
