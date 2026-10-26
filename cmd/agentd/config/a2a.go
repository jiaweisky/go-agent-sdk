package config

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"
)

// CardConfig overrides an exposed agent's AgentCard.
type CardConfig struct {
	Name             string          `yaml:"name"`
	Description      string          `yaml:"description"`
	Version          string          `yaml:"version"`
	DocumentationURL string          `yaml:"documentationUrl"`
	IconURL          string          `yaml:"iconUrl"`
	Provider         *ProviderConfig `yaml:"provider"`
	Skills           []SkillConfig   `yaml:"skills"`

	// CardPath is relative to the server's pathPrefix (do not repeat it).
	CardPath           string              `yaml:"cardPath"`
	DefaultInputModes  []string            `yaml:"defaultInputModes"`
	DefaultOutputModes []string            `yaml:"defaultOutputModes"`
	Extended           *ExtendedCardConfig `yaml:"extended"`
}

// validate checks one card override's descriptive fields.
func (c CardConfig) validate(agentID string) error {
	if err := validateSkills(agentID, c.Skills); err != nil {
		return err
	}
	if err := validateProvider(agentID, c.Provider); err != nil {
		return err
	}
	if err := validateModes(agentID, "defaultInputModes", c.DefaultInputModes); err != nil {
		return err
	}
	if err := validateModes(agentID, "defaultOutputModes", c.DefaultOutputModes); err != nil {
		return err
	}
	if err := validateCardPath(agentID, c.CardPath); err != nil {
		return err
	}

	if c.Extended != nil {
		if err := validateSkills(agentID, c.Extended.Skills); err != nil {
			return err
		}
		if err := validateProvider(agentID, c.Extended.Provider); err != nil {
			return err
		}
		if err := validateModes(agentID, "extended.defaultInputModes", c.Extended.DefaultInputModes); err != nil {
			return err
		}
		if err := validateModes(agentID, "extended.defaultOutputModes", c.Extended.DefaultOutputModes); err != nil {
			return err
		}
	}
	return nil
}

// ExtendedCardConfig overrides fields of the authenticated extended card. Empty
// fields inherit the public card's values.
type ExtendedCardConfig struct {
	Name               string          `yaml:"name"`
	Description        string          `yaml:"description"`
	Version            string          `yaml:"version"`
	DocumentationURL   string          `yaml:"documentationUrl"`
	IconURL            string          `yaml:"iconUrl"`
	Provider           *ProviderConfig `yaml:"provider"`
	Skills             []SkillConfig   `yaml:"skills"`
	DefaultInputModes  []string        `yaml:"defaultInputModes"`
	DefaultOutputModes []string        `yaml:"defaultOutputModes"`
}

// ProviderConfig identifies the organization offering an agent.
type ProviderConfig struct {
	Organization string `yaml:"organization"`
	URL          string `yaml:"url"`
}

// SkillConfig advertises one capability as an operator-authored descriptive
// label (id/name/description are required).
type SkillConfig struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Examples    []string `yaml:"examples"`
	InputModes  []string `yaml:"inputModes"`
	OutputModes []string `yaml:"outputModes"`
}

// PushNotificationsConfig enables A2A push notifications. The client supplies
// the per-task webhook configuration at runtime; this block only enables the
// server-side capability and configures its sender. Configurations use an
// in-memory store (they are lost on restart).
type PushNotificationsConfig struct {
	Enabled              bool   `yaml:"enabled"`
	Timeout              string `yaml:"timeout"`
	AllowPrivateNetworks bool   `yaml:"allowPrivateNetworks"`
	FailOnError          bool   `yaml:"failOnError"`
}

// Validate checks a push notification block.
func (p *PushNotificationsConfig) Validate() error {
	if p == nil || !p.Enabled {
		return nil
	}
	if _, err := ParseOptionalDuration(p.Timeout); err != nil {
		return fmt.Errorf("pushNotifications.timeout: %w", err)
	}
	return nil
}

// IsEnabled reports whether push notifications are turned on.
func (p *PushNotificationsConfig) IsEnabled() bool {
	return p != nil && p.Enabled
}

// SigningConfig configures AgentCard signing with one private key. The card's
// signatures are computed; they are never authored in YAML.
type SigningConfig struct {
	KeyFile   string `yaml:"keyFile"`
	KeyID     string `yaml:"keyID"`
	Algorithm string `yaml:"algorithm"`
	JWKSURL   string `yaml:"jwksURL"`
}

// Validate checks a signing block.
func (s *SigningConfig) Validate() error {
	if s == nil {
		return nil
	}
	if s.KeyFile == "" {
		return errors.New("signing.keyFile is required")
	}
	return nil
}

// A2ATransport is one protocol binding an A2A server exposes. gRPC is
// intentionally not supported: its fixed service path cannot be mounted per
// agent without tenant dispatch or a port per agent.
type A2ATransport string

const (
	// A2ATransportJSONRPC is the JSON-RPC binding (the default).
	A2ATransportJSONRPC A2ATransport = "jsonrpc"
	// A2ATransportREST is the HTTP+JSON/REST binding.
	A2ATransportREST A2ATransport = "rest"
)

// Valid reports whether t is a supported A2A transport.
func (t A2ATransport) Valid() bool {
	return t == A2ATransportJSONRPC || t == A2ATransportREST
}

// ValidateCardSkills checks one agent card's skill entries.
func ValidateCardSkills(agentID string, skills []SkillConfig) error {
	return validateSkills(agentID, skills)
}

// validateSkills requires each configured card skill to carry id, name and
// description — the fields A2A marks required. Skills are operator-authored
// descriptive labels advertised on the card; they are not tied to the agent's
// internal tools/subagents, and an agent with none advertises an empty list.
func validateSkills(agentID string, skills []SkillConfig) error {
	for i, skill := range skills {
		if skill.ID == "" || skill.Name == "" || skill.Description == "" {
			return fmt.Errorf("agent %q: skills[%d] requires id, name, and description", agentID, i)
		}
	}
	return nil
}

// validateCards checks every agent card override.
func validateCards(cards map[string]CardConfig) error {
	for agentID, card := range cards {
		if err := card.validate(agentID); err != nil {
			return err
		}
	}
	return nil
}

// validateProvider checks the optional provider block.
func validateProvider(agentID string, provider *ProviderConfig) error {
	if provider == nil {
		return nil
	}
	if provider.Organization == "" || provider.URL == "" {
		return fmt.Errorf("agent %q: provider requires organization and url", agentID)
	}
	parsed, err := url.Parse(provider.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("agent %q: provider.url must be an absolute URL, got %q", agentID, provider.URL)
	}
	return nil
}

// validateModes checks card-level media type lists.
func validateModes(agentID, field string, modes []string) error {
	for _, mode := range modes {
		if mode == "" {
			return fmt.Errorf("agent %q: %s contains an empty media type", agentID, field)
		}
		mediaType, _, err := mime.ParseMediaType(mode)
		if err != nil || strings.Count(mediaType, "/") != 1 {
			return fmt.Errorf("agent %q: %s contains invalid media type %q", agentID, field, mode)
		}
	}
	return nil
}

// validateCardPath checks a custom card path. Collisions with other routes are
// checked separately once every agent's effective path is known.
func validateCardPath(agentID, cardPath string) error {
	if cardPath == "" {
		return nil
	}
	if !strings.HasPrefix(cardPath, "/") {
		return fmt.Errorf("agent %q: cardPath must start with /", agentID)
	}
	if cardPath == "/" || strings.HasSuffix(cardPath, "/") {
		return fmt.Errorf("agent %q: cardPath must not be the root or end with /", agentID)
	}
	if path.Clean(cardPath) != cardPath {
		return fmt.Errorf("agent %q: cardPath %q is not a clean path", agentID, cardPath)
	}
	if strings.ContainsAny(cardPath, "{}`?#%\\ \t\r\n") {
		return fmt.Errorf("agent %q: cardPath %q contains unsupported characters", agentID, cardPath)
	}
	switch cardPath {
	case "/.well-known/agent-card.json", "/.well-known/agents.json":
		return fmt.Errorf("agent %q: cardPath %q is reserved", agentID, cardPath)
	}
	return nil
}

// agentEndpointPath is the JSON-RPC route for one agent.
func agentEndpointPath(agent string) string {
	return "/agents/" + agent + "/"
}

// agentCardPath is the default card route for one agent.
func agentCardPath(agent string) string {
	return agentEndpointPath(agent) + ".well-known/agent-card.json"
}

// validateCardPaths rejects duplicate or conflicting mux patterns. http.ServeMux
// panics on duplicate registration, so this must fail at startup.
func validateCardPaths(s Server) error {
	seen := map[string]string{
		"/.well-known/agent-card.json": "the origin-root default card",
		"/.well-known/agents.json":     "the aggregate discovery endpoint",
	}
	for _, agent := range s.Agents {
		seen[agentEndpointPath(agent)] = "agent " + agent + " endpoint"
	}

	restEnabled := false
	for _, transport := range s.EffectiveTransports() {
		if transport == A2ATransportREST {
			restEnabled = true
		}
	}

	for _, agent := range s.Agents {
		card, ok := s.Cards[agent]
		cardPath := agentCardPath(agent)
		if ok && card.CardPath != "" {
			cardPath = card.CardPath
		}
		// Introspection is served before host/pathPrefix dispatch, so a root-mounted
		// card path is shadowed regardless of whether the server is host-routed.
		if s.PathPrefix == "" && (cardPath == "/api/config" || cardPath == "/api/info") {
			return fmt.Errorf("server %q: agent %q cardPath %q collides with an introspection endpoint", s.Name, agent, cardPath)
		}
		if restEnabled {
			for _, other := range s.Agents {
				restPrefix := agentEndpointPath(other) + "rest"
				if strings.HasPrefix(cardPath, restPrefix+"/") {
					return fmt.Errorf("server %q: agent %q cardPath %q collides with the REST binding of agent %q", s.Name, agent, cardPath, other)
				}
			}
		}
		if prev, dup := seen[cardPath]; dup {
			return fmt.Errorf("server %q: agent %q cardPath %q collides with %s", s.Name, agent, cardPath, prev)
		}
		seen[cardPath] = "agent " + agent + " card"
	}
	return nil
}

// validateTransports checks the optional transport list.
func validateTransports(transports []A2ATransport) error {
	if len(transports) == 0 {
		return nil
	}
	seen := make(map[A2ATransport]bool, len(transports))
	for _, transport := range transports {
		if !transport.Valid() {
			return fmt.Errorf("transports: unknown transport %q (want jsonrpc or rest)", transport)
		}
		if seen[transport] {
			return fmt.Errorf("transports: duplicate %q", transport)
		}
		seen[transport] = true
	}
	return nil
}
