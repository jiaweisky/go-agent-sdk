package schema

import (
	"encoding/json"
	"testing"
)

func TestDecodeSessionMessageShell(t *testing.T) {
	original := SessionMessageShell{
		ID:      "msg_1",
		Time:    SessionMessageTimeRange{Created: 10},
		Type:    "shell",
		CallID:  "call_1",
		Command: "ls",
		Output:  "ok",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := DecodeSessionMessage(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	shell, ok := decoded.(SessionMessageShell)
	if !ok {
		t.Fatalf("decoded type = %T", decoded)
	}
	if shell.ID != "msg_1" || shell.Command != "ls" || shell.Output != "ok" || shell.CallID != "call_1" {
		t.Fatalf("shell = %+v", shell)
	}
}

func TestDecodeSessionMessageAssistantContent(t *testing.T) {
	original := SessionMessageAssistant{
		ID:    "msg_2",
		Time:  SessionMessageTimeRange{Created: 1},
		Type:  "assistant",
		Agent: "build",
		Model: ModelRef{ID: "m", ProviderID: "p"},
		Content: []SessionMessageAssistantContent{
			SessionMessageAssistantText{Type: "text", ID: "t1", Text: "hi"},
			SessionMessageAssistantTool{
				Type: "tool",
				ID:   "t2",
				Name: "bash",
				State: SessionMessageToolStateCompleted{
					Status:     "completed",
					Input:      map[string]any{"command": "ls"},
					Structured: map[string]any{},
					Content:    []ToolContent{},
				},
				Time: SessionMessageAssistantToolTime{Created: 1},
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := DecodeSessionMessage(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	assistant, ok := decoded.(SessionMessageAssistant)
	if !ok {
		t.Fatalf("decoded type = %T", decoded)
	}
	if len(assistant.Content) != 2 {
		t.Fatalf("content = %+v", assistant.Content)
	}
	if text, ok := assistant.Content[0].(SessionMessageAssistantText); !ok || text.Text != "hi" {
		t.Fatalf("content[0] = %#v", assistant.Content[0])
	}
	tool, ok := assistant.Content[1].(SessionMessageAssistantTool)
	if !ok {
		t.Fatalf("content[1] = %#v", assistant.Content[1])
	}
	if _, ok := tool.State.(SessionMessageToolStateCompleted); !ok {
		t.Fatalf("tool state = %#v", tool.State)
	}
}

func TestDecodeSessionMessageValidatesNestedRequiredFields(t *testing.T) {
	cases := []string{
		// model-switched model requires id + providerID
		`{"id":"msg_1","type":"model-switched","time":{"created":0},"model":{}}`,
		// optional files, but present entries must have uri + mime
		`{"id":"msg_1","type":"user","text":"x","time":{"created":0},"files":[{}]}`,
		// assistant model requires id + providerID
		`{"id":"msg_1","type":"assistant","time":{"created":0},"agent":"a","model":{},"content":[]}`,
	}
	for _, input := range cases {
		if _, err := DecodeSessionMessage([]byte(input)); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}

	valid := `{"id":"msg_1","type":"user","text":"x","time":{"created":0},"files":[{"uri":"u","mime":"m"}]}`
	if _, err := DecodeSessionMessage([]byte(valid)); err != nil {
		t.Fatalf("valid nested optional failed: %v", err)
	}
}

func TestDecodeSessionMessageRejectsOptionalNull(t *testing.T) {
	cases := []string{
		`{"id":"msg_1","type":"user","text":"x","time":{"created":0},"files":null}`,
		`{"id":"msg_1","type":"user","text":"x","time":{"created":0},"metadata":null}`,
	}
	for _, input := range cases {
		if _, err := DecodeSessionMessage([]byte(input)); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestDecodeSessionMessageToolStateNull(t *testing.T) {
	// result accepts any JSON value, so an explicit null is allowed.
	ok := `{"status":"completed","input":{},"content":[],"structured":{},"result":null}`
	if _, err := DecodeSessionMessageToolState([]byte(ok)); err != nil {
		t.Fatalf("result null should be allowed: %v", err)
	}
	// input is a Record, so null is rejected.
	bad := `{"status":"completed","input":null,"content":[],"structured":{}}`
	if _, err := DecodeSessionMessageToolState([]byte(bad)); err == nil {
		t.Fatalf("input null should fail")
	}
}

func TestDecodeSessionMessageRejectsInvalidContent(t *testing.T) {
	// content entries are validated by their concrete decoder.
	input := `{"id":"msg_1","type":"assistant","time":{"created":0},"agent":"a",` +
		`"model":{"id":"m","providerID":"p"},"content":[{"type":"text","id":"t"}]}`
	if _, err := DecodeSessionMessage([]byte(input)); err == nil {
		t.Fatalf("expected error for content missing text")
	}
	// tool state is validated by its concrete decoder.
	tool := `{"id":"msg_1","type":"assistant","time":{"created":0},"agent":"a",` +
		`"model":{"id":"m","providerID":"p"},"content":[{"type":"tool","id":"t","name":"bash",` +
		`"time":{"created":0},"state":{"status":"running","input":{},"structured":{}}}]}`
	if _, err := DecodeSessionMessage([]byte(tool)); err == nil {
		t.Fatalf("expected error for tool state missing content")
	}
}

func TestDecodeSessionMessageUnknownType(t *testing.T) {
	if _, err := DecodeSessionMessage([]byte(`{"id":"msg_1","type":"nope"}`)); err == nil {
		t.Fatalf("expected error for unknown type")
	}
}

func TestDecodeSessionMessageRejectsMissingRequiredFields(t *testing.T) {
	cases := []string{
		`{"id":"msg_1","type":"user","time":{"created":0}}`,             // missing text
		`{"id":"msg_1","type":"user","text":null,"time":{"created":0}}`, // null text
		`{"id":"msg_1","type":"user","text":"x","time":{}}`,             // missing time.created
		`{"type":"user","text":"x","time":{"created":0}}`,               // missing id
	}
	for _, input := range cases {
		if _, err := DecodeSessionMessage([]byte(input)); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}
