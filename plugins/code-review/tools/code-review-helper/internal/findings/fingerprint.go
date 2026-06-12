package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Fingerprint returns a stable 12-hex-char identity for a finding, derived from
// its file, line, and category. It is embedded as a hidden HTML-comment marker
// in every rendered finding body (see internal/render) so the prior-review
// dedup pass can recognise this plugin's own comments without depending on any
// human-readable rendered text. Selecting prior threads on the marker replaces
// the previous brittle regex on the "(Confidence: NN/100)" header, which broke
// silently whenever the rendered header wording changed.
func Fingerprint(file string, line int, category string) string {
	sum := sha256.Sum256([]byte(file + ":" + strconv.Itoa(line) + ":" + category))
	return hex.EncodeToString(sum[:])[:12]
}
