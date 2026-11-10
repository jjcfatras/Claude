package findings

import "slices"

// Categories is the closed vocabulary for a finding's `category` field, mirrored
// in references/code-review-rubrics.md § "Category vocabulary". Keep the two in
// sync; the rubric is what specialists read, this is what the pipeline enforces.
//
// The field used to be documented as a free-form string, which quietly disabled
// every consumer that keyed on it. `dedup.relatedCategoryPairs` was keyed on
// ROLE names (`security`, `quality`, `claude-md`, …) while being indexed by
// Category, so on transcript db83dbda — whose four findings carried the
// categories `security`, `observability`, `convention-adherence` and
// `terraform` — the table matched zero pairs and both dedup passes fell back to
// severity alone. `domainMatch` (specialist == category) made the same
// assumption. A closed vocabulary is what makes those consumers mean something.
//
// Categories name the DEFECT, not the specialist that found it. Two roles that
// spot the same problem from different angles are meant to land on the same
// value — that shared value is the strongest same-defect signal dedup has.
var Categories = []string{
	"accessibility",
	"api-contract",
	"concurrency",
	"config",
	"convention-adherence",
	"correctness",
	"data-integrity",
	"design",
	"duplication",
	"error-handling",
	"observability",
	"performance",
	"security",
	"testing",
	"type-safety",
}

// KnownCategory reports whether c is in the closed vocabulary.
func KnownCategory(c string) bool {
	return slices.Contains(Categories, c)
}
