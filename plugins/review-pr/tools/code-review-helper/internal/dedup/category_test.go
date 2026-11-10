package dedup

import (
	"testing"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// TestRelatedCategoriesAreInVocabulary is the guard that the previous version of
// this table lacked. `relatedCategoryPairs` was keyed on role names
// (`security`, `quality`, `claude-md`, `typescript`, `react`) while being
// indexed by Finding.Category — so it silently matched nothing, and nothing in
// the suite noticed because every test that exercised dedup happened to rely on
// the severity fallback instead. Binding the table to the vocabulary makes that
// class of drift a compile-of-the-suite failure rather than a dead feature.
func TestRelatedCategoriesAreInVocabulary(t *testing.T) {
	for _, pair := range relatedCategories {
		for _, c := range pair {
			if !findings.KnownCategory(c) {
				t.Errorf("relatedCategories contains %q, which is not in findings.Categories — "+
					"the pair can never match a validated finding", c)
			}
		}
		if pair[0] == pair[1] {
			t.Errorf("relatedCategories pair %v is self-paired; identical categories already match via sameDefectSignal", pair)
		}
	}
}

// TestRelatedCategoryPairsAreSymmetric verifies the expansion covers both
// orderings, since callers pass (a, b) in whatever order they hold.
func TestRelatedCategoryPairsAreSymmetric(t *testing.T) {
	for key := range relatedCategoryPairs {
		mirror := [2]string{key[1], key[0]}
		if !relatedCategoryPairs[mirror] {
			t.Errorf("pair %v present but mirror %v missing", key, mirror)
		}
	}
	if got, want := len(relatedCategoryPairs), len(relatedCategories)*2; got != want {
		t.Errorf("len(relatedCategoryPairs)=%d, want %d — a duplicate pair in relatedCategories?", got, want)
	}
}
