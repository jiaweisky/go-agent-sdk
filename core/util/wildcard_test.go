package util

import (
	"runtime"
	"testing"
)

func TestMatchNormalizesSeparators(t *testing.T) {
	if !Match(`C:\Users\me\tool-output\x`, `C:/Users/me/tool-output/*`) {
		t.Fatal("windows input should match a posix pattern")
	}
	if !Match("/data/tool-output/x", `\data\tool-output\*`) {
		t.Fatal("posix input should match a windows pattern")
	}
}

func TestMatchWildcards(t *testing.T) {
	cases := []struct {
		input, pattern string
		want           bool
	}{
		{"src/a.ts", "src/*", true},
		{"src/a.ts", "*.ts", true},
		{"src/a.ts", "*.tsx", false},
		{"read", "*", true},
		{"a\nb", "a*b", true}, // dotAll: `.` matches newline
	}
	for _, c := range cases {
		if got := Match(c.input, c.pattern); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.input, c.pattern, got, c.want)
		}
	}
}

func TestMatchCaseSensitivity(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !Match("README", "readme") {
			t.Fatal("windows match should be case-insensitive")
		}
		return
	}
	if Match("README", "readme") {
		t.Fatal("non-windows match should be case-sensitive")
	}
}
