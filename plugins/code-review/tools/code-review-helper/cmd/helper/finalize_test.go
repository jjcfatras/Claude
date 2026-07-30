package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
	"github.com/jjcfatras/claude-tools/code-review-helper/internal/lines"
)

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b ,c", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"a,", []string{"a"}},
		{" , ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := splitCSV(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCollectIDs(t *testing.T) {
	a := []findings.Finding{{ID: "b"}, {ID: "a"}, {ID: ""}}
	b := []findings.Finding{{ID: "a"}, {ID: "c"}}

	got := collectIDs(a, b)
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("collectIDs = %v, want %v (sorted, deduped, drop empties)", got, want)
	}

	if got := collectIDs(); len(got) != 0 {
		t.Errorf("collectIDs() with no buckets should be empty, got %v", got)
	}
}

func TestMissingIDs(t *testing.T) {
	cases := []struct {
		name    string
		want    []string
		known   []string
		missing []string
	}{
		{"empty-want", nil, []string{"a"}, nil},
		{"all-known", []string{"a", "b"}, []string{"a", "b", "c"}, nil},
		{"partial-overlap", []string{"a", "z", "b", "y"}, []string{"a", "b"}, []string{"z", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := missingIDs(tc.want, tc.known)
			if !slices.Equal(got, tc.missing) {
				t.Errorf("missingIDs(%v, %v) = %v, want %v",
					tc.want, tc.known, got, tc.missing)
			}
		})
	}
}

func TestFilterFindings(t *testing.T) {
	in := []findings.Finding{
		{ID: "sec-1"},
		{ID: "err-2"},
		{ID: "qual-3"},
	}
	cases := []struct {
		name    string
		include []string
		exclude []string
		wantIDs []string
	}{
		{"no-filter", nil, nil, []string{"sec-1", "err-2", "qual-3"}},
		{"include-only", []string{"err-2"}, nil, []string{"err-2"}},
		{"exclude-only", nil, []string{"qual-3"}, []string{"sec-1", "err-2"}},
		// When both are supplied, include wins (caller cleared exclude with a
		// warning); filterFindings re-applies the same precedence as a safety
		// net even if a future caller forgets.
		{"include-wins-when-both", []string{"sec-1"}, []string{"sec-1"}, []string{"sec-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterFindings(in, tc.include, tc.exclude)
			gotIDs := make([]string, 0, len(got))
			for _, f := range got {
				gotIDs = append(gotIDs, f.ID)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Errorf("ids = %v, want %v", gotIDs, tc.wantIDs)
			}
		})
	}
}

func TestCoalesce(t *testing.T) {
	if got := coalesce[int](nil); got == nil || len(got) != 0 {
		t.Errorf("coalesce(nil) = %v, want non-nil empty slice", got)
	}
	in := []string{"a"}
	got := coalesce(in)
	if len(got) != 1 || &got[0] != &in[0] {
		t.Errorf("coalesce on non-nil should return same backing slice")
	}
}

func TestParseFinalizeArgs_ValidateMissing(t *testing.T) {
	opts, err := parseFinalizeArgs([]string{"--diff", "d"})
	if err != nil {
		t.Fatalf("parseFinalizeArgs: %v", err)
	}
	if err := opts.validate(); err == nil {
		t.Fatal("expected validate to fail for partial args")
	} else if !strings.Contains(err.Error(), "missing required flag") {
		t.Errorf("error should mention 'missing required flag'; got %v", err)
	}
}

func TestParseFinalizeArgs_ValidateAllSet(t *testing.T) {
	opts, err := parseFinalizeArgs([]string{
		"--diff", "d",
		"--findings-dir", "fd",
		"--prior-issues", "pi",
		"--head-sha", "h",
		"--owner", "o",
		"--repo", "r",
		"--out-consolidated", "oc",
		"--out-payload", "op",
		"--out-pending-payload", "opp",
		"--out-body", "ob",
		"--out-fallback", "of",
	})
	if err != nil {
		t.Fatalf("parseFinalizeArgs: %v", err)
	}
	if err := opts.validate(); err != nil {
		t.Errorf("validate should pass with all flags set; got %v", err)
	}
}

func TestBuildPostingFilter(t *testing.T) {
	classified := linesResultWithIDs("sec-1", "err-2", "qual-3")

	t.Run("empty-returns-nil", func(t *testing.T) {
		pf, err := buildPostingFilter("", "", classified)
		if err != nil {
			t.Fatalf("buildPostingFilter: %v", err)
		}
		if pf != nil {
			t.Errorf("expected nil filter, got %+v", pf)
		}
	})

	t.Run("include-only", func(t *testing.T) {
		pf, err := buildPostingFilter("sec-1,err-2", "", classified)
		if err != nil {
			t.Fatalf("buildPostingFilter: %v", err)
		}
		if pf == nil || !slices.Equal(pf.IncludeIDs, []string{"sec-1", "err-2"}) {
			t.Errorf("IncludeIDs = %+v, want [sec-1 err-2]", pf)
		}
	})

	t.Run("both-supplied-include-wins", func(t *testing.T) {
		pf, err := buildPostingFilter("sec-1", "qual-3", classified)
		if err != nil {
			t.Fatalf("buildPostingFilter: %v", err)
		}
		if pf == nil || !slices.Equal(pf.IncludeIDs, []string{"sec-1"}) {
			t.Errorf("IncludeIDs = %+v", pf)
		}
		if len(pf.ExcludeIDs) != 0 {
			t.Errorf("ExcludeIDs should be cleared, got %v", pf.ExcludeIDs)
		}
	})

	t.Run("unknown-include-id", func(t *testing.T) {
		_, err := buildPostingFilter("nope", "", classified)
		if err == nil {
			t.Fatal("expected error for unknown ID")
		}
		if !strings.Contains(err.Error(), "unknown id") {
			t.Errorf("error should mention 'unknown id'; got %v", err)
		}
	})

	t.Run("unknown-exclude-id", func(t *testing.T) {
		_, err := buildPostingFilter("", "nope", classified)
		if err == nil {
			t.Fatal("expected error for unknown ID")
		}
		if !strings.Contains(err.Error(), "unknown id") {
			t.Errorf("error should mention 'unknown id'; got %v", err)
		}
	})
}

// TestReconcile_ConservationInvariant is the check the pipeline lacked: every
// loaded finding must land in exactly one output bucket. Before this existed, a
// finding folded by dedup appeared in no bucket at all and the rendered summary
// still looked internally consistent — which is how a review lost its
// highest-value finding without a single count looking wrong.
func TestReconcile_ConservationInvariant(t *testing.T) {
	mk := func(id string) findings.Finding {
		return findings.Finding{ID: id, Severity: findings.SeverityMedium, Confidence: 60, File: "a.ts", Line: 1}
	}

	t.Run("every finding accounted for", func(t *testing.T) {
		loaded := []findings.Finding{mk("a"), mk("b"), mk("c"), mk("d")}
		cf := consolidatedFile{
			InlineEligible:     []findings.Finding{mk("a")},
			SummaryOnly:        []findings.Finding{mk("b")},
			DroppedPriorReview: []findings.Finding{mk("c")},
			DroppedByGate:      []droppedGateEntry{{ID: "d"}},
		}
		got := reconcile(loaded, nil, cf)
		if !got.OK {
			t.Errorf("want OK, got %+v", got)
		}
		if got.Loaded != 4 || got.Accounted != 4 {
			t.Errorf("want loaded=4 accounted=4, got loaded=%d accounted=%d", got.Loaded, got.Accounted)
		}
	})

	t.Run("dedup-folded findings count via the ledger", func(t *testing.T) {
		loaded := []findings.Finding{mk("a"), mk("b")}
		cf := consolidatedFile{
			InlineEligible: []findings.Finding{mk("a")},
			Deduped:        []dedupedEntry{{ID: "b", MergedInto: "a", MergedBy: findings.MergedByPositional}},
		}
		if got := reconcile(loaded, nil, cf); !got.OK || got.Accounted != 2 {
			t.Errorf("a folded finding must be accounted for by the ledger, got %+v", got)
		}
	})

	t.Run("a vanished finding is reported as an orphan", func(t *testing.T) {
		loaded := []findings.Finding{mk("a"), mk("b")}
		cf := consolidatedFile{InlineEligible: []findings.Finding{mk("a")}} // b dropped silently
		got := reconcile(loaded, nil, cf)
		if got.OK {
			t.Fatalf("want OK=false when a finding vanishes, got %+v", got)
		}
		if !slices.Equal(got.Orphans, []string{"b"}) {
			t.Errorf("want orphans=[b], got %v", got.Orphans)
		}
		if got.Loaded != 2 || got.Accounted != 1 {
			t.Errorf("want loaded=2 accounted=1, got loaded=%d accounted=%d", got.Loaded, got.Accounted)
		}
	})

	t.Run("invalid findings count toward the total", func(t *testing.T) {
		loaded := []findings.Finding{mk("a")}
		invalid := []findings.InvalidFinding{{Role: "perf", ID: "p-1", Reason: "non-positive line 0"}}
		cf := consolidatedFile{InlineEligible: []findings.Finding{mk("a")}}
		got := reconcile(loaded, invalid, cf)
		if !got.OK || got.Loaded != 2 || got.Accounted != 2 {
			t.Errorf("want OK with loaded=2 accounted=2, got %+v", got)
		}
	})
}

func TestCollectDeduped_WalksEveryBucket(t *testing.T) {
	withRef := func(id, refID string) findings.Finding {
		return findings.Finding{ID: id, CrossRefs: []findings.CrossRef{{ID: refID, MergedBy: findings.MergedBySemantic}}}
	}
	// A gate-dropped survivor carries CrossRefs just like an inline-eligible one;
	// skipping that bucket would report its folded peer as an orphan.
	got := collectDeduped(
		[]findings.Finding{withRef("inline", "folded-1")},
		nil,
		[]findings.Finding{withRef("prior", "folded-2")},
		[]findings.Finding{withRef("gated", "folded-3")},
	)
	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"folded-1", "folded-2", "folded-3"}) {
		t.Errorf("want all three folded peers, got %v", ids)
	}
	if got[0].MergedInto != "inline" {
		t.Errorf("want merged_into=inline, got %q", got[0].MergedInto)
	}
}

func TestLoadPriorIssues(t *testing.T) {
	t.Run("missing-file", func(t *testing.T) {
		_, err := loadPriorIssues(filepath.Join(t.TempDir(), "nope.json"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("malformed-json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_, err := loadPriorIssues(path)
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("error should mention path %q; got %v", path, err)
		}
	})

	t.Run("valid-empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ok.json")
		body := `{"last_review_date":null,"last_review_commit":null,"issues":[]}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := loadPriorIssues(path); err != nil {
			t.Fatalf("loadPriorIssues: %v", err)
		}
	})
}

// linesResultWithIDs builds a minimal lines.Result whose InlineEligible bucket
// carries the named IDs. buildPostingFilter only reads InlineEligible and
// SummaryOnly IDs, so this is the smallest fixture that exercises it.
func linesResultWithIDs(ids ...string) lines.Result {
	res := lines.Result{}
	for _, id := range ids {
		res.InlineEligible = append(res.InlineEligible, findings.Finding{ID: id})
	}
	return res
}
