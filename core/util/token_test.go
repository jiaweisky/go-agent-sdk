package util

import "testing"

func TestTokenEstimate(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"a":        0,
		"ab":       1,
		"abcd":     1,
		"abcde":    1,
		"abcdef":   2,
		"abcdefgh": 2,
	}
	for input, want := range cases {
		if got := TokenEstimate(input); got != want {
			t.Fatalf("TokenEstimate(%q) = %d, want %d", input, got, want)
		}
	}
}
