package config

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

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
	// Tools is the tool-name allowlist (Go-side extension; see
	// schema.AgentInfo): nil means every tool, an explicit empty list means
	// none.
	Tools []string `json:"tools,omitempty"`
	// Backend declares how the agent executes: a local LLM loop (unset) or a
	// remote A2A agent. It lets a markdown agent file describe a peer reached
	// over the wire, instead of requiring RegisterAgent.
	Backend *schema.AgentBackend `json:"backend,omitempty"`
}
