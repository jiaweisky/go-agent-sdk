package util

import (
	"math"
	"unicode/utf16"
)

const charsPerToken = 4

// TokenEstimate mirrors Token.estimate. The TS length is UTF-16 code units, so
// the same unit count is used here.
func TokenEstimate(input string) int {
	length := len(utf16.Encode([]rune(input)))
	return int(math.Max(0, math.Round(float64(length)/charsPerToken)))
}
