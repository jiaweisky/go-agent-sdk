package config

import "github.com/user/go-agent-sdk/schema"

// AgentInfo mirrors ConfigAgent.Info.
type AgentInfo struct {
	Model       *string                   `json:"model,omitempty"`
	Variant     *string                   `json:"variant,omitempty"`
	Request     *Request                  `json:"request,omitempty"`
	System      *string                   `json:"system,omitempty"`
	Description *string                   `json:"description,omitempty"`
	Mode        *string                   `json:"mode,omitempty"`
	Hidden      *bool                     `json:"hidden,omitempty"`
	Color       *string                   `json:"color,omitempty"`
	Steps       *schema.PositiveInt       `json:"steps,omitempty"`
	Disabled    *bool                     `json:"disabled,omitempty"`
	Permissions *schema.PermissionRuleset `json:"permissions,omitempty"`
}
