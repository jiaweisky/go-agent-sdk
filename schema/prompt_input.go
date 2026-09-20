package schema

// PromptInputFileAttachment mirrors PromptInput.FileAttachment.
type PromptInputFileAttachment struct {
	URI         string        `json:"uri"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Source      *PromptSource `json:"source,omitempty"`
}

// PromptInput mirrors PromptInput.Prompt.
type PromptInput struct {
	Text   string                      `json:"text"`
	Files  []PromptInputFileAttachment `json:"files,omitempty"`
	Agents []PromptAgentAttachment     `json:"agents,omitempty"`
}
