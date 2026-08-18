package main

import (
	"slices"
	"strings"
	"unicode"

	"github.com/jjcfatras/claude-tools/code-review-helper/internal/findings"
)

// Specialists scan independently and never see each other's output — the plugin
// has no cross-agent peer verification by design. That makes disagreement
// invisible rather than erroneous: on transcript db83dbda (Aug 2026) `infra`
// flagged a variable's MISSING default as the defect ("that app's next
// automated apply fails outright") while `quality` and `security`, four lines
// away, both cited that same missing default as the correct fail-loud pattern
// to copy. All three passed the gate, and the PR author received inline comments
// giving opposite advice about one variable.
//
// This pass detects the shape and reports it. It deliberately does NOT arbitrate:
// picking a winner would require either an arbiter agent with less context than
// either specialist (and would trade away the no-cross-agent-verification design
// the whole plugin is calibrated around) or prose valence analysis, which is not
// reliable. The user already has the affordance to act — the post prompt accepts
// a finding-ID subset — so surfacing beats guessing.
const (
	// conflictLineWindow is how close two findings must anchor to plausibly be
	// about the same code. Wider than positional dedup's ±3 because these
	// findings are explicitly NOT duplicates: they survived dedup, so the
	// interesting case is neighbours discussing one another.
	conflictLineWindow = 5
	// minIdentifierLen filters out short tokens that carry no signal.
	minIdentifierLen = 4
)

// conflictEntry records two surviving findings that anchor near each other where
// one's prose names the other's subject. "Potential" is load-bearing: this is a
// prompt to look, not a verdict that the two disagree.
type conflictEntry struct {
	IDA        string `json:"id_a"`
	IDB        string `json:"id_b"`
	File       string `json:"file"`
	LineA      int    `json:"line_a"`
	LineB      int    `json:"line_b"`
	Identifier string `json:"identifier"`
}

// detectConflicts reports pairs of findings in the same file, within
// conflictLineWindow lines, where one finding's explanation names an identifier
// that appears in the other's code but not its own. That asymmetry is the
// signal: a finding that discusses a symbol it did not itself anchor to is
// reasoning about its neighbour's subject.
//
// Input should be only the findings that will actually reach the user (inline
// eligible + summary only); flagging a pair the gate already dropped is noise.
func detectConflicts(in []findings.Finding) []conflictEntry {
	var out []conflictEntry
	for i := 0; i < len(in); i++ {
		for j := i + 1; j < len(in); j++ {
			a, b := in[i], in[j]
			if a.File != b.File || abs(a.Line-b.Line) > conflictLineWindow {
				continue
			}
			id, ok := crossReferencedIdentifier(a, b)
			if !ok {
				id, ok = crossReferencedIdentifier(b, a)
			}
			if !ok {
				continue
			}
			out = append(out, conflictEntry{
				IDA: a.ID, IDB: b.ID,
				File:  a.File,
				LineA: a.Line, LineB: b.Line,
				Identifier: id,
			})
		}
	}
	return out
}

// crossReferencedIdentifier reports an identifier that `subject` anchors on (it
// appears in subject.Code) and that `commenter` discusses in prose without
// having anchored on it itself.
func crossReferencedIdentifier(subject, commenter findings.Finding) (string, bool) {
	prose := commenter.Explanation + "\n" + commenter.Rationale
	for _, id := range projectIdentifiers(subject.Code) {
		if strings.Contains(commenter.Code, id) {
			// Shared context, not a cross-reference — both findings sit on the
			// same snippet, which line proximity already told us.
			continue
		}
		if strings.Contains(prose, id) {
			return id, true
		}
	}
	return "", false
}

// projectIdentifiers extracts plausible project symbols from a code snippet:
// tokens that are snake_case or camelCase. Requiring a case transition or an
// underscore is what keeps language keywords (`function`, `return`, `const`,
// `variable`) and prose words out without maintaining a per-language keyword
// list — a single-word identifier like `region` is missed, which is the right
// trade for an advisory signal that must not cry wolf.
func projectIdentifiers(code string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.FieldsFunc(code, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if len(tok) < minIdentifierLen || seen[tok] {
			continue
		}
		if unicode.IsDigit(rune(tok[0])) {
			continue
		}
		if !isCompoundIdentifier(tok) {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	slices.Sort(out)
	return out
}

// isCompoundIdentifier reports whether tok looks like a deliberate multi-word
// symbol: it contains an underscore, or a lower-to-upper case transition.
func isCompoundIdentifier(tok string) bool {
	if strings.Contains(tok, "_") {
		return true
	}
	runes := []rune(tok)
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i-1]) {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
