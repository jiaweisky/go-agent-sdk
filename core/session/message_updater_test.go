package session

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func eventPayload(eventType string, data any) schema.EventPayload {
	return schema.EventPayload{Type: eventType, Data: data}
}

func testEnvelope(timestamp int, sessionID schema.SessionID) schema.SessionEventEnvelope {
	return schema.SessionEventEnvelope{Timestamp: schema.NonNegativeInt(timestamp), SessionID: sessionID}
}

func testModel() schema.ModelRef {
	return schema.ModelRef{ID: "model", ProviderID: "provider"}
}

func intPtr(value int) *schema.NonNegativeInt {
	converted := schema.NonNegativeInt(value)
	return &converted
}

func TestMemoryGetCurrentAssistant(t *testing.T) {
	stale := schema.SessionMessageAssistant{ID: "msg_stale", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}}
	completed := schema.SessionMessageAssistant{
		ID: "msg_completed", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 1, Completed: intPtr(2)},
	}
	adapter := Memory(&MemoryState{Messages: []schema.SessionMessage{stale, completed}})
	if _, ok := adapter.GetCurrentAssistant(); ok {
		t.Fatalf("expected no current assistant when latest is completed")
	}
	if _, ok := adapter.GetAssistant("msg_completed"); !ok {
		t.Fatalf("expected to find completed assistant by id")
	}
}

func TestUpdateStepStartedCompletesPrevious(t *testing.T) {
	state := &MemoryState{}
	adapter := Memory(state)
	for i, id := range []schema.MessageID{"msg_a1", "msg_a2"} {
		if err := Update(adapter, eventPayload(schema.SessionEventStepStarted.Type, schema.SessionEventStepStartedData{
			SessionEventEnvelope: testEnvelope(i+1, "ses_1"),
			AssistantMessageID:   id, Agent: "build", Model: testModel(),
		})); err != nil {
			t.Fatalf("step started: %v", err)
		}
	}
	if len(state.Messages) != 2 {
		t.Fatalf("messages = %d", len(state.Messages))
	}
	first := state.Messages[0].(schema.SessionMessageAssistant)
	if first.Time.Completed == nil || *first.Time.Completed != 2 {
		t.Fatalf("first assistant = %+v", first.Time)
	}
	second := state.Messages[1].(schema.SessionMessageAssistant)
	if second.Time.Completed != nil {
		t.Fatalf("second assistant should be incomplete: %+v", second.Time)
	}
}

func TestUpdateTextLifecycle(t *testing.T) {
	state := &MemoryState{Messages: []schema.SessionMessage{
		schema.SessionMessageAssistant{ID: "msg_a1", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}},
	}}
	adapter := Memory(state)
	steps := []struct {
		eventType string
		data      any
	}{
		{schema.SessionEventTextStarted.Type, schema.SessionEventTextStartedData{SessionEventEnvelope: testEnvelope(1, "ses_1"), AssistantMessageID: "msg_a1", TextID: "t1"}},
		{schema.SessionEventTextDelta.Type, schema.SessionEventTextDeltaData{SessionEventEnvelope: testEnvelope(2, "ses_1"), AssistantMessageID: "msg_a1", TextID: "t1", Delta: "hello"}},
		{schema.SessionEventTextDelta.Type, schema.SessionEventTextDeltaData{SessionEventEnvelope: testEnvelope(3, "ses_1"), AssistantMessageID: "msg_a1", TextID: "t1", Delta: " world"}},
		{schema.SessionEventTextEnded.Type, schema.SessionEventTextEndedData{SessionEventEnvelope: testEnvelope(4, "ses_1"), AssistantMessageID: "msg_a1", TextID: "t1", Text: "final"}},
	}
	for _, step := range steps {
		if err := Update(adapter, eventPayload(step.eventType, step.data)); err != nil {
			t.Fatalf("update %s: %v", step.eventType, err)
		}
	}
	assistant := state.Messages[0].(schema.SessionMessageAssistant)
	if len(assistant.Content) != 1 {
		t.Fatalf("content = %+v", assistant.Content)
	}
	text := assistant.Content[0].(schema.SessionMessageAssistantText)
	if text.Text != "final" {
		t.Fatalf("text = %q", text.Text)
	}
}

func TestUpdateToolLifecycle(t *testing.T) {
	state := &MemoryState{Messages: []schema.SessionMessage{
		schema.SessionMessageAssistant{ID: "msg_a1", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}},
	}}
	adapter := Memory(state)
	steps := []struct {
		eventType string
		data      any
	}{
		{schema.SessionEventToolInputStarted.Type, schema.SessionEventToolInputStartedData{SessionEventEnvelope: testEnvelope(1, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Name: "bash"}},
		{schema.SessionEventToolInputEnded.Type, schema.SessionEventToolInputEndedData{SessionEventEnvelope: testEnvelope(2, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Text: `{"command":"ls"}`}},
		{schema.SessionEventToolCalled.Type, schema.SessionEventToolCalledData{SessionEventEnvelope: testEnvelope(3, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Tool: "bash", Input: map[string]any{"command": "ls"}, Provider: schema.SessionEventToolProvider{Executed: false}}},
		{schema.SessionEventToolProgress.Type, schema.SessionEventToolProgressData{SessionEventEnvelope: testEnvelope(4, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Structured: map[string]any{"partial": true}, Content: []schema.ToolContent{}}},
		{schema.SessionEventToolSuccess.Type, schema.SessionEventToolSuccessData{SessionEventEnvelope: testEnvelope(5, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Structured: map[string]any{"ok": true}, Content: []schema.ToolContent{}, Provider: schema.SessionEventToolProvider{Executed: true}}},
	}
	for _, step := range steps {
		if err := Update(adapter, eventPayload(step.eventType, step.data)); err != nil {
			t.Fatalf("update %s: %v", step.eventType, err)
		}
	}
	assistant := state.Messages[0].(schema.SessionMessageAssistant)
	tool := assistant.Content[0].(schema.SessionMessageAssistantTool)
	completed, ok := tool.State.(schema.SessionMessageToolStateCompleted)
	if !ok {
		t.Fatalf("state = %#v", tool.State)
	}
	if completed.Input["command"] != "ls" || completed.Structured["ok"] != true {
		t.Fatalf("completed = %+v", completed)
	}
	if tool.Provider == nil || !tool.Provider.Executed {
		t.Fatalf("provider = %+v", tool.Provider)
	}
	if tool.Time.Completed == nil || *tool.Time.Completed != 5 {
		t.Fatalf("time = %+v", tool.Time)
	}
}

func TestUpdateToolFailedFromPending(t *testing.T) {
	state := &MemoryState{Messages: []schema.SessionMessage{
		schema.SessionMessageAssistant{ID: "msg_a1", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}},
	}}
	adapter := Memory(state)
	Update(adapter, eventPayload(schema.SessionEventToolInputStarted.Type, schema.SessionEventToolInputStartedData{
		SessionEventEnvelope: testEnvelope(1, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1", Name: "bash",
	}))
	Update(adapter, eventPayload(schema.SessionEventToolFailed.Type, schema.SessionEventToolFailedData{
		SessionEventEnvelope: testEnvelope(2, "ses_1"), AssistantMessageID: "msg_a1", CallID: "c1",
		Error:    schema.SessionMessageUnknownError{Type: "unknown", Message: "boom"},
		Provider: schema.SessionEventToolProvider{Executed: false},
	}))
	assistant := state.Messages[0].(schema.SessionMessageAssistant)
	tool := assistant.Content[0].(schema.SessionMessageAssistantTool)
	stateError, ok := tool.State.(schema.SessionMessageToolStateError)
	if !ok {
		t.Fatalf("state = %#v", tool.State)
	}
	if stateError.Input == nil || len(stateError.Input) != 0 || stateError.Error.Message != "boom" {
		t.Fatalf("error state = %+v", stateError)
	}
}

func TestUpdateShellLifecycle(t *testing.T) {
	state := &MemoryState{}
	adapter := Memory(state)
	Update(adapter, eventPayload(schema.SessionEventShellStarted.Type, schema.SessionEventShellStartedData{
		SessionEventEnvelope: testEnvelope(1, "ses_1"), MessageID: "msg_s", CallID: "c1", Command: "pwd",
	}))
	Update(adapter, eventPayload(schema.SessionEventShellEnded.Type, schema.SessionEventShellEndedData{
		SessionEventEnvelope: testEnvelope(2, "ses_1"), CallID: "c1", Output: "/project",
	}))
	shell := state.Messages[0].(schema.SessionMessageShell)
	if shell.Output != "/project" || shell.Time.Completed == nil || *shell.Time.Completed != 2 {
		t.Fatalf("shell = %+v", shell)
	}
}

func TestUpdateCompactionEnded(t *testing.T) {
	state := &MemoryState{}
	adapter := Memory(state)
	if err := Update(adapter, eventPayload(schema.SessionEventCompactionEnded.Type, schema.SessionEventCompactionEndedData{
		SessionEventEnvelope: testEnvelope(1, "ses_1"), MessageID: "msg_c", Reason: "manual", Text: "summary", Recent: "recent",
	})); err != nil {
		t.Fatalf("compaction ended: %v", err)
	}
	compaction := state.Messages[0].(schema.SessionMessageCompaction)
	if compaction.Summary != "summary" || compaction.Recent != "recent" || compaction.Reason != "manual" {
		t.Fatalf("compaction = %+v", compaction)
	}
}

func TestUpdateStepEnded(t *testing.T) {
	state := &MemoryState{Messages: []schema.SessionMessage{
		schema.SessionMessageAssistant{ID: "msg_a1", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}},
	}}
	adapter := Memory(state)
	snapshot := "snap1"
	if err := Update(adapter, eventPayload(schema.SessionEventStepEnded.Type, schema.SessionEventStepEndedData{
		SessionEventEnvelope: testEnvelope(9, "ses_1"),
		AssistantMessageID:   "msg_a1", Finish: "stop", Cost: 0.5,
		Tokens:   schema.SessionMessageTokens{Input: 1, Output: 2, Reasoning: 3, Cache: schema.SessionMessageCache{Read: 4, Write: 5}},
		Snapshot: &snapshot, Files: []schema.RelativePath{"a.go"},
	})); err != nil {
		t.Fatalf("step ended: %v", err)
	}
	assistant := state.Messages[0].(schema.SessionMessageAssistant)
	if assistant.Finish == nil || *assistant.Finish != "stop" || assistant.Cost == nil || *assistant.Cost != 0.5 {
		t.Fatalf("assistant = %+v", assistant)
	}
	if assistant.Tokens == nil || assistant.Tokens.Input != 1 || assistant.Tokens.Cache.Write != 5 {
		t.Fatalf("tokens = %+v", assistant.Tokens)
	}
	if assistant.Snapshot == nil || assistant.Snapshot.End == nil || *assistant.Snapshot.End != "snap1" ||
		len(assistant.Snapshot.Files) != 1 || assistant.Snapshot.Files[0] != "a.go" {
		t.Fatalf("snapshot = %+v", assistant.Snapshot)
	}
	if assistant.Time.Completed == nil || *assistant.Time.Completed != 9 {
		t.Fatalf("time = %+v", assistant.Time)
	}
}

func TestUpdateReasoningLifecycle(t *testing.T) {
	state := &MemoryState{Messages: []schema.SessionMessage{
		schema.SessionMessageAssistant{ID: "msg_a1", Type: "assistant", Time: schema.SessionMessageTimeRange{Created: 0}},
	}}
	adapter := Memory(state)
	steps := []struct {
		eventType string
		data      any
	}{
		{schema.SessionEventReasoningStarted.Type, schema.SessionEventReasoningStartedData{SessionEventEnvelope: testEnvelope(1, "ses_1"), AssistantMessageID: "msg_a1", ReasoningID: "r1"}},
		{schema.SessionEventReasoningDelta.Type, schema.SessionEventReasoningDeltaData{SessionEventEnvelope: testEnvelope(2, "ses_1"), AssistantMessageID: "msg_a1", ReasoningID: "r1", Delta: "think"}},
		{schema.SessionEventReasoningEnded.Type, schema.SessionEventReasoningEndedData{SessionEventEnvelope: testEnvelope(3, "ses_1"), AssistantMessageID: "msg_a1", ReasoningID: "r1", Text: "final"}},
	}
	for _, step := range steps {
		if err := Update(adapter, eventPayload(step.eventType, step.data)); err != nil {
			t.Fatalf("update %s: %v", step.eventType, err)
		}
	}
	assistant := state.Messages[0].(schema.SessionMessageAssistant)
	reasoning := assistant.Content[0].(schema.SessionMessageAssistantReasoning)
	if reasoning.Text != "final" {
		t.Fatalf("text = %q", reasoning.Text)
	}
	if reasoning.Time == nil || reasoning.Time.Created != 1 || reasoning.Time.Completed == nil || *reasoning.Time.Completed != 3 {
		t.Fatalf("time = %+v", reasoning.Time)
	}
}

func TestUpdatePromptedAppendsUser(t *testing.T) {
	state := &MemoryState{}
	adapter := Memory(state)
	if err := Update(adapter, eventPayload(schema.SessionEventPrompted.Type, schema.SessionEventPromptData{
		SessionEventEnvelope: testEnvelope(1, "ses_1"), MessageID: "msg_u",
		Prompt: schema.Prompt{Text: "hello"}, Delivery: schema.SessionDeliveryQueue,
	})); err != nil {
		t.Fatalf("prompted: %v", err)
	}
	user := state.Messages[0].(schema.SessionMessageUser)
	if user.Text != "hello" || user.Type != "user" || user.ID != "msg_u" {
		t.Fatalf("user = %+v", user)
	}
}
