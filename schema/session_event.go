package schema

// SessionEventRetryError is the error a retry reports.
type SessionEventRetryError struct {
	Message         string            `json:"message"`
	StatusCode      *float64          `json:"statusCode,omitempty"`
	IsRetryable     bool              `json:"isRetryable"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    *string           `json:"responseBody,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

var (
	sessionEventDurable     = &EventDurable{Aggregate: "sessionID", Version: 1}
	sessionEventStepDurable = &EventDurable{Aggregate: "sessionID", Version: 2}
)

var SessionEventAgentSwitched = Define(DefineInput{
	Type:    "session.next.agent.switched",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "agent": nil,
	},
})

var SessionEventModelSwitched = Define(DefineInput{
	Type:    "session.next.model.switched",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "model": nil,
	},
})

var SessionEventTitleUpdated = Define(DefineInput{
	Type:    "session.next.title.updated",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "title": nil,
	},
})

var SessionEventMoved = Define(DefineInput{
	Type:    "session.next.moved",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "location": nil, "subdirectory": nil,
	},
})

var SessionEventPrompted = Define(DefineInput{
	Type:    "session.next.prompted",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "prompt": nil, "delivery": nil,
	},
})

var SessionEventPromptAdmitted = Define(DefineInput{
	Type:    "session.next.prompt.admitted",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "prompt": nil, "delivery": nil,
	},
})

// SessionEventAttachmentOmitted reports an attachment the selected model could
// not carry, so it was replaced by a text note in the request. It is durable so
// a client can perceive an omission rather than silently losing the attachment.
var SessionEventAttachmentOmitted = Define(DefineInput{
	Type:    "session.next.attachment.omitted",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "name": nil, "mime": nil,
	},
})

var SessionEventContextUpdated = Define(DefineInput{
	Type:    "session.next.context.updated",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "text": nil,
	},
})

var SessionEventSynthetic = Define(DefineInput{
	Type:    "session.next.synthetic",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "text": nil,
	},
})

// SessionEventShellStarted is defined and projected for parity, but has no
// producer: nothing publishes it (V2 core defines and projects it too, yet does
// not emit it).
var SessionEventShellStarted = Define(DefineInput{
	Type:    "session.next.shell.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "callID": nil, "command": nil,
	},
})

// SessionEventShellEnded is defined and projected for parity, but has no
// producer: nothing publishes it (V2 core defines and projects it too, yet does
// not emit it).
var SessionEventShellEnded = Define(DefineInput{
	Type:    "session.next.shell.ended",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "callID": nil, "output": nil,
	},
})

var SessionEventStepStarted = Define(DefineInput{
	Type:    "session.next.step.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "agent": nil, "model": nil, "snapshot": nil,
	},
})

var SessionEventStepEnded = Define(DefineInput{
	Type:    "session.next.step.ended",
	Durable: sessionEventStepDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "finish": nil,
		"cost": nil, "tokens": nil, "snapshot": nil, "files": nil,
	},
})

var SessionEventStepFailed = Define(DefineInput{
	Type:    "session.next.step.failed",
	Durable: sessionEventStepDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "error": nil,
	},
})

var SessionEventTextStarted = Define(DefineInput{
	Type:    "session.next.text.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "textID": nil,
	},
})

var SessionEventTextDelta = Define(DefineInput{
	Type: "session.next.text.delta",
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "textID": nil, "delta": nil,
	},
})

var SessionEventTextEnded = Define(DefineInput{
	Type:    "session.next.text.ended",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "textID": nil, "text": nil,
	},
})

var SessionEventReasoningStarted = Define(DefineInput{
	Type:    "session.next.reasoning.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "reasoningID": nil, "providerMetadata": nil,
	},
})

var SessionEventReasoningDelta = Define(DefineInput{
	Type: "session.next.reasoning.delta",
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "reasoningID": nil, "delta": nil,
	},
})

var SessionEventReasoningEnded = Define(DefineInput{
	Type:    "session.next.reasoning.ended",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "reasoningID": nil,
		"text": nil, "providerMetadata": nil,
	},
})

var SessionEventToolInputStarted = Define(DefineInput{
	Type:    "session.next.tool.input.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil, "name": nil,
	},
})

var SessionEventToolInputDelta = Define(DefineInput{
	Type: "session.next.tool.input.delta",
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil, "delta": nil,
	},
})

var SessionEventToolInputEnded = Define(DefineInput{
	Type:    "session.next.tool.input.ended",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil, "text": nil,
	},
})

var SessionEventToolCalled = Define(DefineInput{
	Type:    "session.next.tool.called",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil,
		"tool": nil, "input": nil, "provider": nil,
	},
})

var SessionEventToolProgress = Define(DefineInput{
	Type:    "session.next.tool.progress",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil,
		"structured": nil, "content": nil,
	},
})

var SessionEventToolSuccess = Define(DefineInput{
	Type:    "session.next.tool.success",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil,
		"structured": nil, "content": nil, "outputPaths": nil, "result": nil, "provider": nil,
	},
})

var SessionEventToolFailed = Define(DefineInput{
	Type:    "session.next.tool.failed",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "assistantMessageID": nil, "callID": nil,
		"error": nil, "result": nil, "provider": nil,
	},
})

// SessionEventRetried is defined for parity, but has no producer: nothing
// publishes it (V2 core does not emit it either, and its projector is commented
// out there).
var SessionEventRetried = Define(DefineInput{
	Type:    "session.next.retried",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "attempt": nil, "error": nil,
	},
})

var SessionEventCompactionStarted = Define(DefineInput{
	Type:    "session.next.compaction.started",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "reason": nil,
	},
})

var SessionEventCompactionDelta = Define(DefineInput{
	Type: "session.next.compaction.delta",
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "text": nil,
	},
})

var SessionEventCompactionEnded = Define(DefineInput{
	Type:    "session.next.compaction.ended",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil, "reason": nil, "text": nil, "recent": nil,
	},
})

var SessionEventRevertStaged = Define(DefineInput{
	Type:    "session.next.revert.staged",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "revert": nil,
	},
})

var SessionEventRevertCleared = Define(DefineInput{
	Type:    "session.next.revert.cleared",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil,
	},
})

var SessionEventRevertCommitted = Define(DefineInput{
	Type:    "session.next.revert.committed",
	Durable: sessionEventDurable,
	Schema: map[string]any{
		"timestamp": nil, "sessionID": nil, "messageID": nil,
	},
})

// SessionEventDurableDefinitions are the session events that are persisted.
var SessionEventDurableDefinitions = Inventory(
	SessionEventAgentSwitched,
	SessionEventModelSwitched,
	SessionEventTitleUpdated,
	SessionEventMoved,
	SessionEventPrompted,
	SessionEventPromptAdmitted,
	SessionEventAttachmentOmitted,
	SessionEventContextUpdated,
	SessionEventSynthetic,
	SessionEventShellStarted,
	SessionEventShellEnded,
	SessionEventStepStarted,
	SessionEventStepEnded,
	SessionEventStepFailed,
	SessionEventTextStarted,
	SessionEventTextEnded,
	SessionEventToolInputStarted,
	SessionEventToolInputEnded,
	SessionEventToolCalled,
	SessionEventToolProgress,
	SessionEventToolSuccess,
	SessionEventToolFailed,
	SessionEventReasoningStarted,
	SessionEventReasoningEnded,
	SessionEventRetried,
	SessionEventCompactionStarted,
	SessionEventCompactionEnded,
	SessionEventRevertStaged,
	SessionEventRevertCleared,
	SessionEventRevertCommitted,
)

// SessionEventDefinitions are every session event, including the live-only
// deltas.
var SessionEventDefinitions = Inventory(
	SessionEventAgentSwitched,
	SessionEventModelSwitched,
	SessionEventTitleUpdated,
	SessionEventMoved,
	SessionEventPrompted,
	SessionEventPromptAdmitted,
	SessionEventAttachmentOmitted,
	SessionEventContextUpdated,
	SessionEventSynthetic,
	SessionEventShellStarted,
	SessionEventShellEnded,
	SessionEventStepStarted,
	SessionEventStepEnded,
	SessionEventStepFailed,
	SessionEventTextStarted,
	SessionEventTextDelta,
	SessionEventTextEnded,
	SessionEventReasoningStarted,
	SessionEventReasoningDelta,
	SessionEventReasoningEnded,
	SessionEventToolInputStarted,
	SessionEventToolInputDelta,
	SessionEventToolInputEnded,
	SessionEventToolCalled,
	SessionEventToolProgress,
	SessionEventToolSuccess,
	SessionEventToolFailed,
	SessionEventRetried,
	SessionEventCompactionStarted,
	SessionEventCompactionDelta,
	SessionEventCompactionEnded,
	SessionEventRevertStaged,
	SessionEventRevertCleared,
	SessionEventRevertCommitted,
)
