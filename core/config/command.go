package config

// CommandInfo mirrors ConfigCommand.Info.
type CommandInfo struct {
	Template    string  `json:"template"`
	Description *string `json:"description,omitempty"`
	Agent       *string `json:"agent,omitempty"`
	Model       *string `json:"model,omitempty"`
	Variant     *string `json:"variant,omitempty"`
	Subtask     *bool   `json:"subtask,omitempty"`
}
