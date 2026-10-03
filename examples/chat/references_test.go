package main

import (
	"strings"
	"testing"
)

// TestReferenceAddLocal guards the local reference path and its listing.
func TestReferenceAddLocal(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/reference add docs /tmp/example-docs") {
			t.Error("/reference was not recognised")
		}
		c.console.write("--list--\n")
		handleCommand(c, "/reference")
	})
	for _, want := range []string{"added reference docs", "--list--", "References (1):", "[local]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestReferenceAddGitRejectsBadSource guards that a non-remote repository or an
// invalid branch is reported instead of silently "added". Neither schedules a
// clone, so the test stays offline.
func TestReferenceAddGitRejectsBadSource(t *testing.T) {
	cases := []string{
		"/reference add-git bad file:///tmp/nope",
		"/reference add-git bad https://example.com/a/b -x",
	}
	for _, input := range cases {
		out := runCommand(t, nil, func(c *chat) {
			if !handleCommand(c, input) {
				t.Errorf("%q was not recognised", input)
			}
		})
		if !strings.Contains(out, "was not accepted") {
			t.Fatalf("%q out = %q, want the rejection", input, out)
		}
	}
}
