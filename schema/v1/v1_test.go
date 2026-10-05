package v1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

func roundTrip(t *testing.T, value, into any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(encoded, into); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestIDPrefixes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		prefix string
		id     func() string
	}{
		{name: "permission", prefix: "per_", id: func() string { return string(NewPermissionID()) }},
		{name: "message", prefix: "msg_", id: func() string { return string(NewMessageID()) }},
		{name: "part", prefix: "prt_", id: func() string { return string(NewPartID()) }},
	} {
		if got := testCase.id(); !strings.HasPrefix(got, testCase.prefix) {
			t.Fatalf("%s id = %q, want %s prefix", testCase.name, got, testCase.prefix)
		}
	}
}

func TestPermissionEventInventory(t *testing.T) {
	if PermissionEventAsked.Type != "permission.asked" {
		t.Fatalf("asked type = %q", PermissionEventAsked.Type)
	}
	if PermissionEventReplied.Type != "permission.replied" {
		t.Fatalf("replied type = %q", PermissionEventReplied.Type)
	}
	if PermissionEvent.Asked.Type != PermissionEventAsked.Type || PermissionEvent.Replied.Type != PermissionEventReplied.Type {
		t.Fatal("PermissionEvent does not mirror the singular definitions")
	}
	if len(PermissionEvent.Definitions) != 2 {
		t.Fatalf("Definitions = %d, want 2", len(PermissionEvent.Definitions))
	}
}

func TestSessionEventsAreDurable(t *testing.T) {
	// Every v1 session event is durable, aggregated by sessionID at version 1.
	for _, definition := range []schema.EventDefinition{
		V1EventCreated, V1EventUpdated, V1EventDeleted, V1EventMessageUpdated,
		V1EventMessageRemoved, V1EventPartUpdated, V1EventPartRemoved,
	} {
		if definition.Durable == nil {
			t.Fatalf("%s is not durable", definition.Type)
		}
		if definition.Durable.Aggregate != "sessionID" || definition.Durable.Version != 1 {
			t.Fatalf("%s durable = %+v", definition.Type, definition.Durable)
		}
	}
}

func TestSessionCreatedDataJSONRoundTrip(t *testing.T) {
	info := V1SessionInfo{
		ID:        schema.SessionID("ses_1"),
		Slug:      "slug",
		ProjectID: schema.ProjectID("proj_1"),
		Directory: "/tmp/project",
		Title:     "a title",
		Version:   "1.0.0",
	}

	var decoded V1SessionCreatedData
	roundTrip(t, V1SessionCreatedData{SessionID: info.ID, Info: info}, &decoded)
	if decoded.SessionID != info.ID || decoded.Info.ID != info.ID {
		t.Fatalf("session id mismatch: %+v", decoded)
	}
	if decoded.Info.Title != "a title" || decoded.Info.Directory != "/tmp/project" {
		t.Fatalf("info mismatch: %+v", decoded.Info)
	}
}

func TestPermissionRequestJSONRoundTrip(t *testing.T) {
	request := PermissionRequest{
		ID:         NewPermissionID(),
		SessionID:  schema.SessionID("ses_1"),
		Permission: "bash",
		Patterns:   []string{"ls *"},
		Metadata:   map[string]any{"k": "v"},
		Always:     []string{"bash"},
		Tool:       &PermissionTool{MessageID: "msg_1", CallID: "call_1"},
	}

	var decoded PermissionRequest
	roundTrip(t, request, &decoded)
	if decoded.ID != request.ID || decoded.Permission != "bash" || decoded.Tool == nil || decoded.Tool.CallID != "call_1" {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

func TestPermissionReplyBodyOmitsEmptyMessage(t *testing.T) {
	encoded, err := json.Marshal(PermissionReplyBody{Reply: PermissionReplyOnce})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "message") {
		t.Fatalf("nil message should be omitted: %s", encoded)
	}
}
