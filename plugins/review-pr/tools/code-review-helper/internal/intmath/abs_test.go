package intmath

import "testing"

func TestAbs(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"negative", -5, 5},
		{"zero", 0, 0},
		{"positive", 7, 7},
		{"negative one", -1, 1},
		{"large negative", -1000000, 1000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Abs(tc.in); got != tc.want {
				t.Errorf("Abs(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
