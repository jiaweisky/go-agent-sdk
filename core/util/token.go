package util

import (
	"math"
	"unicode/utf16"
)

const charsPerToken = 4

// TokenEstimate estimates the tokens in input, counting UTF-16 code units (four
// per token).
func TokenEstimate(input string) int {
	length := len(utf16.Encode([]rune(input)))
	return int(math.Max(0, math.Round(float64(length)/charsPerToken)))
}
