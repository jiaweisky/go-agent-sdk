package schema

import "regexp"

type AgentID string

func (id AgentID) String() string {
	return string(id)
}

const (
	AgentModeSubagent = "subagent"
	AgentModePrimary  = "primary"
	AgentModeAll      = "all"
)

type AgentColor string

const (
	AgentColorPrimary   AgentColor = "primary"
	AgentColorSecondary AgentColor = "secondary"
	AgentColorAccent    AgentColor = "accent"
	AgentColorSuccess   AgentColor = "success"
	AgentColorWarning   AgentColor = "warning"
	AgentColorError     AgentColor = "error"
	AgentColorInfo      AgentColor = "info"
)

var agentHexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Valid mirrors the Agent.Color union: a #rrggbb hex string or a named color.
func (c AgentColor) Valid() bool {
	switch c {
	case AgentColorPrimary, AgentColorSecondary, AgentColorAccent, AgentColorSuccess,
		AgentColorWarning, AgentColorError, AgentColorInfo:
		return true
	}
	return agentHexColorPattern.MatchString(string(c))
}

type AgentInfo struct {
	ID          AgentID           `json:"id"`
	Model       *ModelRef         `json:"model,omitempty"`
	Request     ProviderRequest   `json:"request"`
	System      *string           `json:"system,omitempty"`
	Description *string           `json:"description,omitempty"`
	Mode        string            `json:"mode"`
	Hidden      bool              `json:"hidden"`
	Color       *AgentColor       `json:"color,omitempty"`
	Steps       *PositiveInt      `json:"steps,omitempty"`
	Permissions PermissionRuleset `json:"permissions"`
}

// AgentInfoEmpty mirrors Agent.Info.empty.
func AgentInfoEmpty(id AgentID) AgentInfo {
	return AgentInfo{
		ID:          id,
		Request:     ProviderRequest{Headers: map[string]string{}, Body: map[string]any{}},
		Mode:        AgentModeAll,
		Hidden:      false,
		Permissions: PermissionRuleset{},
	}
}
