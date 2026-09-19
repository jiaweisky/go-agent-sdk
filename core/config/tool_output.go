package config

import "github.com/user/go-agent-sdk/schema"

// ToolOutputInfo mirrors ConfigToolOutput.Info.
type ToolOutputInfo struct {
	MaxLines *schema.PositiveInt `json:"max_lines,omitempty"`
	MaxBytes *schema.PositiveInt `json:"max_bytes,omitempty"`
}
