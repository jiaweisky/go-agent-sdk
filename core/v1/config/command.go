package config

// V1CommandInfo mirrors ConfigCommandV1.Info.
type V1CommandInfo struct {
	Template    string  `json:"template"`
	Description *string `json:"description,omitempty"`
	Agent       *string `json:"agent,omitempty"`
	Model       *string `json:"model,omitempty"`
	Variant     *string `json:"variant,omitempty"`
	Subtask     *bool   `json:"subtask,omitempty"`
}
