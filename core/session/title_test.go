package session

import "testing"

func TestIsDefaultTitle(t *testing.T) {
	for _, title := range []string{
		"",
		"New session - 2026-10-01T09:30:00.123Z",
		"Child session - 2026-10-01T09:30:00.123Z",
	} {
		if !IsDefaultTitle(title) {
			t.Errorf("IsDefaultTitle(%q) = false, want true", title)
		}
	}
	for _, title := range []string{
		"Session",
		"Fix the parser",
		"New session - not-a-date",
		"New session - 2026-10-01T09:30:00Z",
	} {
		if IsDefaultTitle(title) {
			t.Errorf("IsDefaultTitle(%q) = true, want false", title)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		"Hello world":                       "Hello world",
		"\n\n  Fix the build  \nmore":       "Fix the build",
		"<think>secret</think>Real title":   "Real title",
		"<think>a\nb</think>\n\nTitle here": "Title here",
		"   ":                               "",
	}
	for input, want := range cases {
		if got := CleanTitle(input); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", input, got, want)
		}
	}

	long := make([]rune, 200)
	for i := range long {
		long[i] = 'x'
	}
	got := CleanTitle(string(long))
	if len([]rune(got)) != 100 || got[len(got)-3:] != "..." {
		t.Errorf("CleanTitle(long) length = %d, suffix = %q", len([]rune(got)), got[len(got)-3:])
	}
}
