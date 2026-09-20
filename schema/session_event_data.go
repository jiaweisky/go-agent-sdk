package schema

// SessionEventEnvelope holds the fields shared by every SessionEvent payload.
type SessionEventEnvelope struct {
	Timestamp NonNegativeInt `json:"timestamp"`
	SessionID SessionID      `json:"sessionID"`
}

type SessionEventAgentSwitchedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Agent     string    `json:"agent"`
}

type SessionEventModelSwitchedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Model     ModelRef  `json:"model"`
}

type SessionEventMovedData struct {
	SessionEventEnvelope
	Location     LocationRef   `json:"location"`
	Subdirectory *RelativePath `json:"subdirectory,omitempty"`
}

// SessionEventPromptData covers both session.next.prompted and
// session.next.prompt.admitted (they share the same fields).
type SessionEventPromptData struct {
	SessionEventEnvelope
	MessageID MessageID       `json:"messageID"`
	Prompt    Prompt          `json:"prompt"`
	Delivery  SessionDelivery `json:"delivery"`
}

// SessionEventMessageTextData covers context.updated and synthetic.
type SessionEventMessageTextData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Text      string    `json:"text"`
}

type SessionEventShellStartedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	CallID    string    `json:"callID"`
	Command   string    `json:"command"`
}

type SessionEventShellEndedData struct {
	SessionEventEnvelope
	CallID string `json:"callID"`
	Output string `json:"output"`
}

type SessionEventStepStartedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	Agent              string    `json:"agent"`
	Model              ModelRef  `json:"model"`
	Snapshot           *string   `json:"snapshot,omitempty"`
}

type SessionEventStepEndedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID            `json:"assistantMessageID"`
	Finish             string               `json:"finish"`
	Cost               float64              `json:"cost"`
	Tokens             SessionMessageTokens `json:"tokens"`
	Snapshot           *string              `json:"snapshot,omitempty"`
	Files              []RelativePath       `json:"files,omitempty"`
}

type SessionEventStepFailedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID                  `json:"assistantMessageID"`
	Error              SessionMessageUnknownError `json:"error"`
}

type SessionEventTextStartedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	TextID             string    `json:"textID"`
}

type SessionEventTextDeltaData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	TextID             string    `json:"textID"`
	Delta              string    `json:"delta"`
}

type SessionEventTextEndedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	TextID             string    `json:"textID"`
	Text               string    `json:"text"`
}

type SessionEventReasoningStartedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID         `json:"assistantMessageID"`
	ReasoningID        string            `json:"reasoningID"`
	ProviderMetadata   *ProviderMetadata `json:"providerMetadata,omitempty"`
}

type SessionEventReasoningDeltaData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	ReasoningID        string    `json:"reasoningID"`
	Delta              string    `json:"delta"`
}

type SessionEventReasoningEndedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID         `json:"assistantMessageID"`
	ReasoningID        string            `json:"reasoningID"`
	Text               string            `json:"text"`
	ProviderMetadata   *ProviderMetadata `json:"providerMetadata,omitempty"`
}

type SessionEventToolProvider struct {
	Executed bool              `json:"executed"`
	Metadata *ProviderMetadata `json:"metadata,omitempty"`
}

type SessionEventToolInputStartedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	CallID             string    `json:"callID"`
	Name               string    `json:"name"`
}

type SessionEventToolInputDeltaData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	CallID             string    `json:"callID"`
	Delta              string    `json:"delta"`
}

type SessionEventToolInputEndedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID `json:"assistantMessageID"`
	CallID             string    `json:"callID"`
	Text               string    `json:"text"`
}

type SessionEventToolCalledData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID                `json:"assistantMessageID"`
	CallID             string                   `json:"callID"`
	Tool               string                   `json:"tool"`
	Input              map[string]any           `json:"input"`
	Provider           SessionEventToolProvider `json:"provider"`
}

type SessionEventToolProgressData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID      `json:"assistantMessageID"`
	CallID             string         `json:"callID"`
	Structured         map[string]any `json:"structured"`
	Content            ToolContents   `json:"content"`
}

type SessionEventToolSuccessData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID                `json:"assistantMessageID"`
	CallID             string                   `json:"callID"`
	Structured         map[string]any           `json:"structured"`
	Content            ToolContents             `json:"content"`
	OutputPaths        []string                 `json:"outputPaths,omitempty"`
	Result             any                      `json:"result,omitempty"`
	Provider           SessionEventToolProvider `json:"provider"`
}

type SessionEventToolFailedData struct {
	SessionEventEnvelope
	AssistantMessageID MessageID                  `json:"assistantMessageID"`
	CallID             string                     `json:"callID"`
	Error              SessionMessageUnknownError `json:"error"`
	Result             any                        `json:"result,omitempty"`
	Provider           SessionEventToolProvider   `json:"provider"`
}

type SessionEventRetriedData struct {
	SessionEventEnvelope
	Attempt float64                `json:"attempt"`
	Error   SessionEventRetryError `json:"error"`
}

type SessionEventCompactionStartedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Reason    string    `json:"reason"`
}

type SessionEventCompactionDeltaData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Text      string    `json:"text"`
}

type SessionEventCompactionEndedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
	Reason    string    `json:"reason"`
	Text      string    `json:"text"`
	Recent    string    `json:"recent"`
}

type SessionEventRevertStagedData struct {
	SessionEventEnvelope
	Revert RevertState `json:"revert"`
}

type SessionEventRevertClearedData struct {
	SessionEventEnvelope
}

type SessionEventRevertCommittedData struct {
	SessionEventEnvelope
	MessageID MessageID `json:"messageID"`
}
