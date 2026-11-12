// Package intmath holds tiny integer helpers shared across the helper's
// internal packages. Kept separate so callers don't need to import unrelated
// packages just to reuse a one-line utility.
package intmath

// Abs returns the absolute value of v.
func Abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
