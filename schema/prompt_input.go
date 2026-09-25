package schema

// PromptInputFileAttachment is a file attachment as the caller supplies it.
type PromptInputFileAttachment struct {
	URI         string        `json:"uri"`
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Source      *PromptSource `json:"source,omitempty"`
}

// PromptInput is the input form of a prompt.
type PromptInput struct {
	Text   string                      `json:"text"`
	Files  []PromptInputFileAttachment `json:"files,omitempty"`
	Agents []PromptAgentAttachment     `json:"agents,omitempty"`
}
