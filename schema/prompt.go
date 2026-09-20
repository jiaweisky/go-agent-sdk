package schema

// PromptSource mirrors Prompt.Source.
type PromptSource struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// PromptFileAttachment mirrors Prompt.FileAttachment.
type PromptFileAttachment struct {
	URI         string        `json:"uri"`
	Mime        string        `json:"mime"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Source      *PromptSource `json:"source,omitempty"`
}

// PromptAgentAttachment mirrors Prompt.AgentAttachment.
type PromptAgentAttachment struct {
	Name   string        `json:"name"`
	Source *PromptSource `json:"source,omitempty"`
}

// Prompt mirrors Prompt.Prompt.
type Prompt struct {
	Text   string                  `json:"text"`
	Files  []PromptFileAttachment  `json:"files,omitempty"`
	Agents []PromptAgentAttachment `json:"agents,omitempty"`
}

// PromptFromUserMessage mirrors Prompt.Prompt.fromUserMessage.
func PromptFromUserMessage(text string, files []PromptFileAttachment, agents []PromptAgentAttachment) Prompt {
	return Prompt{Text: text, Files: files, Agents: agents}
}
