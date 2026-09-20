package schema

type MessageID string

func NewMessageID() MessageID {
	return MessageID("msg_" + Ascending())
}

func (id MessageID) String() string {
	return string(id)
}

type SessionMessageTime struct {
	Created NonNegativeInt `json:"created"`
}

// SessionMessageTimeRange mirrors the { created, completed? } time shape used by
// Shell, Reasoning and Assistant messages.
type SessionMessageTimeRange struct {
	Created   NonNegativeInt  `json:"created"`
	Completed *NonNegativeInt `json:"completed,omitempty"`
}

type SessionMessageUnknownError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type SessionMessageCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

type SessionMessageTokens struct {
	Input     float64             `json:"input"`
	Output    float64             `json:"output"`
	Reasoning float64             `json:"reasoning"`
	Cache     SessionMessageCache `json:"cache"`
}

type SessionMessageAgentSwitched struct {
	ID       MessageID          `json:"id"`
	Metadata map[string]any     `json:"metadata,omitempty"`
	Time     SessionMessageTime `json:"time"`
	Type     string             `json:"type"`
	Agent    string             `json:"agent"`
}

func (SessionMessageAgentSwitched) SessionMessageType() string { return "agent-switched" }

type SessionMessageModelSwitched struct {
	ID       MessageID          `json:"id"`
	Metadata map[string]any     `json:"metadata,omitempty"`
	Time     SessionMessageTime `json:"time"`
	Type     string             `json:"type"`
	Model    ModelRef           `json:"model"`
}

func (SessionMessageModelSwitched) SessionMessageType() string { return "model-switched" }

type SessionMessageUser struct {
	ID       MessageID               `json:"id"`
	Metadata map[string]any          `json:"metadata,omitempty"`
	Time     SessionMessageTime      `json:"time"`
	Text     string                  `json:"text"`
	Files    []PromptFileAttachment  `json:"files,omitempty"`
	Agents   []PromptAgentAttachment `json:"agents,omitempty"`
	Type     string                  `json:"type"`
}

func (SessionMessageUser) SessionMessageType() string { return "user" }

type SessionMessageSynthetic struct {
	ID        MessageID          `json:"id"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
	Time      SessionMessageTime `json:"time"`
	SessionID SessionID          `json:"sessionID"`
	Text      string             `json:"text"`
	Type      string             `json:"type"`
}

func (SessionMessageSynthetic) SessionMessageType() string { return "synthetic" }

type SessionMessageSystem struct {
	ID       MessageID          `json:"id"`
	Metadata map[string]any     `json:"metadata,omitempty"`
	Time     SessionMessageTime `json:"time"`
	Type     string             `json:"type"`
	Text     string             `json:"text"`
}

func (SessionMessageSystem) SessionMessageType() string { return "system" }

type SessionMessageShell struct {
	ID       MessageID               `json:"id"`
	Metadata map[string]any          `json:"metadata,omitempty"`
	Time     SessionMessageTimeRange `json:"time"`
	Type     string                  `json:"type"`
	CallID   string                  `json:"callID"`
	Command  string                  `json:"command"`
	Output   string                  `json:"output"`
}

func (SessionMessageShell) SessionMessageType() string { return "shell" }

type SessionMessageToolStatePending struct {
	Status string `json:"status"`
	Input  string `json:"input"`
}

func (SessionMessageToolStatePending) ToolStateStatus() string { return "pending" }

type SessionMessageToolStateRunning struct {
	Status     string         `json:"status"`
	Input      map[string]any `json:"input"`
	Structured map[string]any `json:"structured"`
	Content    ToolContents   `json:"content"`
}

func (SessionMessageToolStateRunning) ToolStateStatus() string { return "running" }

type SessionMessageToolStateCompleted struct {
	Status      string                 `json:"status"`
	Input       map[string]any         `json:"input"`
	Attachments []PromptFileAttachment `json:"attachments,omitempty"`
	Content     ToolContents           `json:"content"`
	OutputPaths []string               `json:"outputPaths,omitempty"`
	Structured  map[string]any         `json:"structured"`
	Result      any                    `json:"result,omitempty"`
}

func (SessionMessageToolStateCompleted) ToolStateStatus() string { return "completed" }

type SessionMessageToolStateError struct {
	Status     string                     `json:"status"`
	Input      map[string]any             `json:"input"`
	Content    ToolContents               `json:"content"`
	Structured map[string]any             `json:"structured"`
	Error      SessionMessageUnknownError `json:"error"`
	Result     any                        `json:"result,omitempty"`
}

func (SessionMessageToolStateError) ToolStateStatus() string { return "error" }

// SessionMessageToolState mirrors the tagged ToolState union.
type SessionMessageToolState interface {
	ToolStateStatus() string
}

type SessionMessageAssistantProvider struct {
	Executed       bool              `json:"executed"`
	Metadata       *ProviderMetadata `json:"metadata,omitempty"`
	ResultMetadata *ProviderMetadata `json:"resultMetadata,omitempty"`
}

type SessionMessageAssistantToolTime struct {
	Created   NonNegativeInt  `json:"created"`
	Ran       *NonNegativeInt `json:"ran,omitempty"`
	Completed *NonNegativeInt `json:"completed,omitempty"`
	Pruned    *NonNegativeInt `json:"pruned,omitempty"`
}

type SessionMessageAssistantTool struct {
	Type     string                           `json:"type"`
	ID       string                           `json:"id"`
	Name     string                           `json:"name"`
	Provider *SessionMessageAssistantProvider `json:"provider,omitempty"`
	State    SessionMessageToolState          `json:"state"`
	Time     SessionMessageAssistantToolTime  `json:"time"`
}

func (SessionMessageAssistantTool) AssistantContentType() string { return "tool" }

type SessionMessageAssistantText struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (SessionMessageAssistantText) AssistantContentType() string { return "text" }

type SessionMessageAssistantReasoning struct {
	Type             string                   `json:"type"`
	ID               string                   `json:"id"`
	Text             string                   `json:"text"`
	ProviderMetadata *ProviderMetadata        `json:"providerMetadata,omitempty"`
	Time             *SessionMessageTimeRange `json:"time,omitempty"`
}

func (SessionMessageAssistantReasoning) AssistantContentType() string { return "reasoning" }

// SessionMessageAssistantContent mirrors the tagged AssistantContent union.
type SessionMessageAssistantContent interface {
	AssistantContentType() string
}

type SessionMessageAssistantSnapshot struct {
	Start *string        `json:"start,omitempty"`
	End   *string        `json:"end,omitempty"`
	Files []RelativePath `json:"files,omitempty"`
}

type SessionMessageAssistant struct {
	ID       MessageID                        `json:"id"`
	Metadata map[string]any                   `json:"metadata,omitempty"`
	Time     SessionMessageTimeRange          `json:"time"`
	Type     string                           `json:"type"`
	Agent    string                           `json:"agent"`
	Model    ModelRef                         `json:"model"`
	Content  []SessionMessageAssistantContent `json:"content"`
	Snapshot *SessionMessageAssistantSnapshot `json:"snapshot,omitempty"`
	Finish   *string                          `json:"finish,omitempty"`
	Cost     *float64                         `json:"cost,omitempty"`
	Tokens   *SessionMessageTokens            `json:"tokens,omitempty"`
	Error    *SessionMessageUnknownError      `json:"error,omitempty"`
}

func (SessionMessageAssistant) SessionMessageType() string { return "assistant" }

type SessionMessageCompaction struct {
	Type     string             `json:"type"`
	Reason   string             `json:"reason"`
	Summary  string             `json:"summary"`
	Recent   string             `json:"recent"`
	ID       MessageID          `json:"id"`
	Metadata map[string]any     `json:"metadata,omitempty"`
	Time     SessionMessageTime `json:"time"`
}

func (SessionMessageCompaction) SessionMessageType() string { return "compaction" }

// SessionMessage mirrors the tagged Session.Message union.
type SessionMessage interface {
	SessionMessageType() string
}
