package findings

type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityMedium   Severity = "Medium"
	SeverityMinor    Severity = "Minor"
)

func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityMedium:
		return 2
	case SeverityMinor:
		return 1
	}
	return 0
}

func (s Severity) Emoji() string {
	switch s {
	case SeverityCritical:
		return "🔴"
	case SeverityMedium:
		return "🟡"
	case SeverityMinor:
		return "📝"
	}
	return ""
}

type ScanStatus string

const (
	ScanComplete ScanStatus = "complete"
	ScanTimedOut ScanStatus = "timed_out"
)

type Verdict string

const (
	VerdictConfirmed       Verdict = "confirmed"
	VerdictFalsePositive   Verdict = "false_positive"
	VerdictOutOfScope      Verdict = "out_of_scope"
	VerdictPeerTimeout     Verdict = "peer_timeout"
	VerdictPeerUnavailable Verdict = "peer_unavailable"
)

type Verification struct {
	Asked             string  `json:"asked"`
	Verdict           Verdict `json:"verdict"`
	Note              string  `json:"note"`
	AppliedAdjustment int     `json:"applied_adjustment"`
}

type Finding struct {
	ID            string         `json:"id"`
	Category      string         `json:"category"`
	File          string         `json:"file"`
	Line          int            `json:"line"`
	StartLine     *int           `json:"startLine"`
	Confidence    int            `json:"confidence"`
	Severity      Severity       `json:"severity"`
	Rationale     string         `json:"rationale"`
	Explanation   string         `json:"explanation"`
	Code          string         `json:"code"`
	SuggestedFix  *string        `json:"suggested_fix"`
	Language      string         `json:"language"`
	Verifications []Verification `json:"verifications"`

	// CrossRefs accumulates cross-references when dedup passes fold a peer
	// finding into this one. They are rendered at output time as italic notes
	// following the Explanation. Stored separately so dedup matchers (notably
	// the file-path check in semantic Rule 1) operate on the specialist's
	// original Explanation, not text that an earlier dedup pass injected.
	CrossRefs []CrossRef `json:"cross_refs,omitempty"`

	// Specialist is set by the loader from the file name; not part of the on-disk per-finding payload.
	Specialist string `json:"-"`
}

// MergedBy names the dedup pass that folded a peer finding, so a surprising
// merge can be traced back to the rule that made it.
const (
	MergedByPositional = "positional"
	MergedBySemantic   = "semantic"
)

// CrossRef is a peer finding folded into another by the dedup pipeline. It is
// the *only* surviving record of that folded finding, so it carries enough
// identity to name it: finalize's accounting pass reconciles every loaded
// finding against the buckets plus these cross-refs, and reports each one in
// the `deduped` ledger. A stub with just specialist/confidence/file/line (what
// this held before) left a merged-away finding unidentifiable — a Medium could
// vanish under an unrelated Minor on the same line with nothing in the audit
// trail naming it.
type CrossRef struct {
	ID         string   `json:"id"`
	Specialist string   `json:"specialist"`
	Category   string   `json:"category"`
	Severity   Severity `json:"severity"`
	Confidence int      `json:"confidence"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Rationale  string   `json:"rationale"`
	MergedBy   string   `json:"merged_by"`
}

type RoleFile struct {
	Specialist string     `json:"specialist"`
	ScanStatus ScanStatus `json:"scan_status"`
	Findings   []Finding  `json:"findings"`
}
