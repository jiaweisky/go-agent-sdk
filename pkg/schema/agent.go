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
	ID          AgentID         `json:"id"`
	Model       *ModelRef       `json:"model,omitempty"`
	Request     ProviderRequest `json:"request"`
	System      *string         `json:"system,omitempty"`
	Description *string         `json:"description,omitempty"`
	Mode        string          `json:"mode"`
	Hidden      bool            `json:"hidden"`
	Color       *AgentColor     `json:"color,omitempty"`
	Steps       *PositiveInt    `json:"steps,omitempty"`
	// Permissions gate tool execution at call time (and a deny rule with
	// resource "*" even removes the tool from the list — see PermissionRule).
	// For an explicit toolset, prefer Tools below.
	Permissions PermissionRuleset `json:"permissions"`
	// Tools is a Go-side extension (not in opencode v2 core): a tool-name
	// allowlist for the agent. When set, only these tools are materialized for
	// the agent's sessions — both the model-facing definitions and execution —
	// and calls to anything else are rejected as unknown. Nil (the field
	// absent) means every registered tool; an explicitly empty list means no
	// tools at all.
	Tools []string `json:"tools,omitempty"`
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
	// Version is the card version. Defaults to "1.0.0".
	Version string `json:"version,omitempty"`
	// DocumentationURL and IconURL are optional card links.
	DocumentationURL string `json:"documentationUrl,omitempty"`
	IconURL          string `json:"iconUrl,omitempty"`
	// Provider identifies the organization offering the agent.
	Provider *AgentProvider `json:"provider,omitempty"`
	// InputModes / OutputModes override the card's media modes (MIME strings),
	// so a multimodal agent advertises audio/video, not just text+image.
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
	// Skills are the capabilities advertised on the card. Each ID must name a
	// real capability of the agent (a tool, subagent or skill); when empty the
	// card falls back to a single skill derived from the agent.
	Skills []AgentSkill `json:"skills,omitempty"`
}

// AgentProvider identifies the organization that provides an exposed agent.
type AgentProvider struct {
	Organization string `json:"organization,omitempty"`
	URL          string `json:"url,omitempty"`
}

// AgentSkill is a capability an exposed agent advertises on its card.
type AgentSkill struct {
	// ID names the underlying capability (a tool, subagent or skill ID).
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
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
