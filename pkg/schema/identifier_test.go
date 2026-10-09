package schema

import (
	"strings"
	"testing"
)

func TestParseIDRejectsMismatchedPrefix(t *testing.T) {
	if _, err := ParseSessionID("msg_1"); err == nil {
		t.Fatalf("expected error for a session id without the ses prefix")
	}
	if _, err := ParseWorkspaceID("ses_1"); err == nil {
		t.Fatalf("expected error for a workspace id without the wrk prefix")
	}
	if _, err := ParsePermissionID("que_1"); err == nil {
		t.Fatalf("expected error for a permission id without the per prefix")
	}
	if _, err := ParseQuestionID("per_1"); err == nil {
		t.Fatalf("expected error for a question id without the que prefix")
	}
	if _, err := ParseTurnID("ses_1"); err == nil {
		t.Fatalf("expected error for a turn id without the trn prefix")
	}
}

func TestParseIDAcceptsMatchingPrefix(t *testing.T) {
	if got, err := ParseSessionID("ses_custom"); err != nil || got != "ses_custom" {
		t.Fatalf("session id = %q, err = %v", got, err)
	}
	if got, err := ParseWorkspaceID("wrk_custom"); err != nil || got != "wrk_custom" {
		t.Fatalf("workspace id = %q, err = %v", got, err)
	}
	if got, err := ParsePermissionID("per_custom"); err != nil || got != "per_custom" {
		t.Fatalf("permission id = %q, err = %v", got, err)
	}
	if got, err := ParseQuestionID("que_custom"); err != nil || got != "que_custom" {
		t.Fatalf("question id = %q, err = %v", got, err)
	}
	if got, err := ParseTurnID("trn_custom"); err != nil || got != "trn_custom" {
		t.Fatalf("turn id = %q, err = %v", got, err)
	}
}

func TestIDConstructorsGeneratePrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		id     string
	}{
		{"session", "ses_", string(NewSessionID())},
		{"workspace", "wrk_", string(NewWorkspaceID())},
		{"permission", "per_", string(NewPermissionID())},
		{"question", "que_", string(NewQuestionID())},
		{"turn", "trn_", string(NewTurnID())},
	}
	for _, test := range cases {
		if !strings.HasPrefix(test.id, test.prefix) {
			t.Fatalf("%s id = %q, want prefix %q", test.name, test.id, test.prefix)
		}
	}
}
