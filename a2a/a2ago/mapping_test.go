package a2ago

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	a2acommon "github.com/user/go-agent-sdk/a2a"
	"github.com/user/go-agent-sdk/a2a/client"
)

func TestPartEventsMapsDataParts(t *testing.T) {
	call := a2a.NewDataPart(map[string]any{"name": "edit", "id": "c1", "args": map[string]any{"file": "a.go"}})
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionCall)

	diff := a2a.NewDataPart(map[string]any{"file": "a.go", "additions": 2, "deletions": 0})
	diff.SetMeta(a2acommon.ToSDKObjectMetaKey(a2acommon.MetaType), a2acommon.MetaFileDiff)

	events := partEvents(a2a.ContentParts{call, diff}, "t1", "c1")
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Kind != client.EventToolCall || events[0].ToolName != "edit" || events[0].CallID != "c1" {
		t.Fatalf("tool call event = %+v", events[0])
	}
	if events[1].Kind != client.EventFileDiff {
		t.Fatalf("file diff event kind = %v, want EventFileDiff", events[1].Kind)
	}
}

func TestStatusEventsExtractsFunctionCall(t *testing.T) {
	call := a2a.NewDataPart(map[string]any{"name": "await_input", "id": "que_1"})
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionCall)
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaIsLongRunning), true)
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("waiting"), call)
	event := a2a.NewStatusUpdateEvent(a2a.TaskInfo{TaskID: "t1", ContextID: "c1"}, a2a.TaskStateInputRequired, message)

	events := statusEvents(event)
	if len(events) != 1 || events[0].Kind != client.EventInputRequired {
		t.Fatalf("events = %+v", events)
	}
	if events[0].RequestID != "que_1" || events[0].ToolName != "await_input" {
		t.Fatalf("call = %q/%q, want que_1/await_input", events[0].RequestID, events[0].ToolName)
	}
}

func TestFunctionResponsePart(t *testing.T) {
	part := functionResponsePart(client.Response{CallID: "que_1", Name: "await_input", Text: "blue"})
	if kind, _ := part.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaType)].(string); kind != a2acommon.TypeFunctionResponse {
		t.Fatalf("kind = %q", kind)
	}
	fields, _ := part.Data().(map[string]any)
	if fields["id"] != "que_1" || fields["name"] != "await_input" || fields["response"] != "blue" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestStatusEventsExtractsQuestions(t *testing.T) {
	call := a2a.NewDataPart(map[string]any{
		"name": "await_input",
		"id":   "que_1",
		"args": map[string]any{"questions": []any{map[string]any{
			"question": "Pick a color",
			"header":   "color",
			"options":  []any{map[string]any{"label": "blue", "description": "cool"}},
		}}},
	})
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaType), a2acommon.TypeFunctionCall)
	call.SetMeta(a2acommon.ToA2AMetaKey(a2acommon.MetaIsLongRunning), true)
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("waiting"), call)
	event := a2a.NewStatusUpdateEvent(a2a.TaskInfo{TaskID: "t1", ContextID: "c1"}, a2a.TaskStateInputRequired, message)

	events := statusEvents(event)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if len(events[0].Questions) != 1 || events[0].Questions[0].Question != "Pick a color" {
		t.Fatalf("questions = %+v", events[0].Questions)
	}
	if len(events[0].Questions[0].Options) != 1 || events[0].Questions[0].Options[0].Label != "blue" {
		t.Fatalf("options = %+v", events[0].Questions[0].Options)
	}
}
