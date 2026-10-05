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

// Valid reports whether the color is a #rrggbb hex string or a known color name.
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
	// Backend selects how the agent executes: a local LLM loop (nil) or a
	// remote A2A agent.
	Backend *AgentBackend `json:"backend,omitempty"`
	// Expose declares that this agent is exposed over A2A (one card/endpoint).
	Expose *AgentExpose `json:"expose,omitempty"`
}

// AgentBackendKind identifies the execution backend for an agent.
type AgentBackendKind string

const (
	// AgentBackendLocal is the default in-process LLM loop.
	AgentBackendLocal AgentBackendKind = "local"
	// AgentBackendA2A delegates execution to a remote A2A agent.
	AgentBackendA2A AgentBackendKind = "a2a"
)

// AgentBackend declares how an agent executes.
type AgentBackend struct {
	Type AgentBackendKind `json:"type"`
	A2A  *AgentA2ABackend `json:"a2a,omitempty"`
}

// AgentA2ABackend describes a remote A2A agent.
type AgentA2ABackend struct {
	// Endpoint is the base URL of the remote A2A agent (card discovery root).
	Endpoint string `json:"endpoint,omitempty"`
	// CardURL overrides the well-known agent card URL.
	CardURL string `json:"cardURL,omitempty"`
	// Headers are extra request headers (for example authorization).
	Headers map[string]string `json:"headers,omitempty"`
}

// AgentExpose declares an agent's inbound A2A exposure.
type AgentExpose struct {
	// Path is the URL path this agent's card is served at. Defaults to the
	// well-known path.
	Path string `json:"path,omitempty"`
	// Name overrides the card name.
	Name string `json:"name,omitempty"`
	// Description overrides the card description.
	Description string `json:"description,omitempty"`
}

// AgentInfoEmpty is an empty agent info, with its maps initialized.
func AgentInfoEmpty(id AgentID) AgentInfo {
	return AgentInfo{
		ID:          id,
		Request:     ProviderRequest{Headers: map[string]string{}, Body: map[string]any{}},
		Mode:        AgentModeAll,
		Hidden:      false,
		Permissions: PermissionRuleset{},
	}
}
