package main

import (
	"bufio"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk"
)

// TestInteractivePermissionFlow guards the three-stage permission prompt: the
// default choice (Allow once), the always confirmation, its cancel path, and
// rejection with a message.
func TestInteractivePermissionFlow(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantAllow  bool
		wantAlways bool
	}{
		{"empty defaults to always, confirmed", "\n1\n", true, true},
		{"empty always, cancelled then once", "\n2\n1\n", true, false},
		{"explicit once", "1\n", true, false},
		{"always confirmed", "2\n1\n", true, true},
		{"reject", "3\nbecause\n", false, false},
		{"eof rejects", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			c := &console{reader: bufio.NewReader(strings.NewReader(tc.input)), out: &out}
			request := agent.PermissionRequest{Action: "bash", Resources: []string{"ls"}, Save: []string{"ls"}}
			decision, err := interactivePermission(c)(request)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if decision.Allow != tc.wantAllow || decision.Always != tc.wantAlways {
				t.Fatalf("decision = %+v, want allow=%v always=%v", decision, tc.wantAllow, tc.wantAlways)
			}
		})
	}
}

// TestPermissionTitle guards the external-directory title and the generic
// fallback.
func TestPermissionTitle(t *testing.T) {
	external := agent.PermissionRequest{Action: "external_directory", Resources: []string{"/srv/data/*"}}
	icon, title := permissionTitle(external)
	if icon != "←" || title != "Access external directory /srv/data" {
		t.Fatalf("external = %q %q", icon, title)
	}

	generic := agent.PermissionRequest{Action: "bash", Resources: []string{"ls"}}
	icon, title = permissionTitle(generic)
	if icon != "⚙" || title != "Call tool bash" {
		t.Fatalf("generic = %q %q", icon, title)
	}
}

// TestAlwaysLines guards the wildcard line versus the pattern list.
func TestAlwaysLines(t *testing.T) {
	wildcard := alwaysLines(agent.PermissionRequest{Action: "edit", Save: []string{"*"}})
	if len(wildcard) != 1 || wildcard[0] != "This will allow edit until the rule is removed (saved for this project)." {
		t.Fatalf("wildcard = %q", wildcard)
	}

	list := alwaysLines(agent.PermissionRequest{Action: "bash", Save: []string{"ls", "pwd"}})
	want := []string{
		"This will allow the following patterns until the rule is removed (saved for this project)",
		"- ls",
		"- pwd",
	}
	if len(list) != len(want) {
		t.Fatalf("list = %q, want %q", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("list[%d] = %q, want %q", i, list[i], want[i])
		}
	}
}
