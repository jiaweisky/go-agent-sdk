package schema

import "testing"

func TestSessionEventDurableVersions(t *testing.T) {
	if SessionEventAgentSwitched.Durable == nil || SessionEventAgentSwitched.Durable.Version != 1 ||
		SessionEventAgentSwitched.Durable.Aggregate != "sessionID" {
		t.Fatalf("agent switched durable = %+v", SessionEventAgentSwitched.Durable)
	}
	if SessionEventStepEnded.Durable == nil || SessionEventStepEnded.Durable.Version != 2 {
		t.Fatalf("step ended durable = %+v", SessionEventStepEnded.Durable)
	}
	if SessionEventStepFailed.Durable == nil || SessionEventStepFailed.Durable.Version != 2 {
		t.Fatalf("step failed durable = %+v", SessionEventStepFailed.Durable)
	}
	if SessionEventTextDelta.Durable != nil {
		t.Fatalf("text delta should be live-only")
	}
	if SessionEventAttachmentOmitted.Durable == nil || SessionEventAttachmentOmitted.Durable.Version != 2 {
		t.Fatalf("attachment omitted durable = %+v", SessionEventAttachmentOmitted.Durable)
	}
	if len(SessionEventDurableDefinitions) != 31 {
		t.Fatalf("durable definitions = %d, want 31", len(SessionEventDurableDefinitions))
	}
	if len(SessionEventDefinitions) != 35 {
		t.Fatalf("definitions = %d, want 35", len(SessionEventDefinitions))
	}
}

func TestSessionTodoEvent(t *testing.T) {
	if SessionTodoEventUpdated.Type != "todo.updated" {
		t.Fatalf("todo event type = %q", SessionTodoEventUpdated.Type)
	}
	if len(SessionTodoEvent.Definitions) != 1 {
		t.Fatalf("todo definitions = %d", len(SessionTodoEvent.Definitions))
	}
}
