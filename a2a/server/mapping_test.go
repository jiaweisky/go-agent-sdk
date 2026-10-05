package server

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestPermissionReplyFromMessage(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"yes allows once", "yes", "once"},
		{"allow allows once", "allow", "once"},
		{"always", "always", "always"},
		{"yes always", "yes, always", "always"},
		{"no rejects", "no", "reject"},
		{"deny rejects", "deny", "reject"},
		{"reject rejects", "reject", "reject"},
		{"negation fails closed", "please don't run it", "reject"},
		{"rejection wins over allowance", "no, I mean always", "reject"},
		{"unrecognized fails closed", "what?", "reject"},
		{"empty fails closed", "", "reject"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(tc.text))
			if got := permissionReplyFromMessage(msg); got != tc.want {
				t.Fatalf("permissionReplyFromMessage(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
