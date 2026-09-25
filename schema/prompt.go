package schema

// PromptSource says where a prompt came from.
type PromptSource struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// PromptFileAttachment is a file attached to a prompt.
type PromptFileAttachment struct {
	URI         string        `json:"uri"`
	Mime        string        `json:"mime"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Source      *PromptSource `json:"source,omitempty"`
}

// PromptAgentAttachment is an agent mentioned in a prompt.
type PromptAgentAttachment struct {
	Name   string        `json:"name"`
	Source *PromptSource `json:"source,omitempty"`
}

// Prompt is a user prompt: its text and any attachments.
type Prompt struct {
	Text   string                  `json:"text"`
	Files  []PromptFileAttachment  `json:"files,omitempty"`
	Agents []PromptAgentAttachment `json:"agents,omitempty"`
}

// PromptFromUserMessage builds a prompt from a user message.
func PromptFromUserMessage(text string, files []PromptFileAttachment, agents []PromptAgentAttachment) Prompt {
	return Prompt{Text: text, Files: files, Agents: agents}
}
