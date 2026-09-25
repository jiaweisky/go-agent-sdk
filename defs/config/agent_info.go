package config

import "github.com/user/go-agent-sdk/schema"

// AgentInfo is the agent definition decoded from a markdown file's frontmatter.
type AgentInfo struct {
	Model       *string                   `json:"model,omitempty"`
	Variant     *string                   `json:"variant,omitempty"`
	Request     *schema.ProviderRequest   `json:"request,omitempty"`
	System      *string                   `json:"system,omitempty"`
	Description *string                   `json:"description,omitempty"`
	Mode        *string                   `json:"mode,omitempty"`
	Hidden      *bool                     `json:"hidden,omitempty"`
	Color       *string                   `json:"color,omitempty"`
	Steps       *schema.PositiveInt       `json:"steps,omitempty"`
	Disabled    *bool                     `json:"disabled,omitempty"`
	Permissions *schema.PermissionRuleset `json:"permissions,omitempty"`
}
