package main

import (
	"strings"
	"testing"
)

// TestMatchURITemplate guards the resource-template matcher: it must anchor the
// whole URI, so a template never claims a URI with extra segments or a value
// crossing a path separator.
func TestMatchURITemplate(t *testing.T) {
	cases := []struct {
		template, uri string
		want          bool
	}{
		{"notes://note/{id}", "notes://note/todo", true},
		{"notes://note/{id}", "notes://note/", false},
		{"notes://note/{id}", "notes://note/a/b", false},
		{"notes://note/{id}", "notes://note/todo/extra", false},
		{"notes://note/{id}/comments", "notes://note/a/comments", true},
		{"notes://note/{id}/comments", "notes://note/a/comments/x", false},
		{"notes://index", "notes://index", true},
		{"notes://index", "notes://index/x", false},
	}
	for _, tc := range cases {
		if got := matchURITemplate(tc.template, tc.uri); got != tc.want {
			t.Errorf("matchURITemplate(%q, %q) = %v, want %v", tc.template, tc.uri, got, tc.want)
		}
	}
}

// TestMCPCommandWithoutServers guards the no-servers case: the test client has
// no MCP configuration, so /mcp reports that instead of panicking.
func TestMCPCommandWithoutServers(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/mcp prompts") {
			t.Error("/mcp was not recognised")
		}
	})
	if !strings.Contains(out, "no MCP servers configured") {
		t.Fatalf("out = %q, want the no-servers hint", out)
	}
}
