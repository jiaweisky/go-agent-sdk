package server

import (
	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// Transport is one protocol binding an A2A server exposes.
type Transport string

const (
	// TransportJSONRPC is the JSON-RPC binding.
	TransportJSONRPC Transport = "jsonrpc"
	// TransportREST is the HTTP+JSON/REST binding.
	TransportREST Transport = "rest"
)

// cardConfig is the resolved input for one card.
type cardConfig struct {
	expose       *schema.AgentExpose
	endpoint     string
	security     *Security
	capabilities a2a.AgentCapabilities
	transports   []Transport
}

// buildCard builds the default streaming/JSON-RPC card for tests and callers
// that do not need the server's configured capabilities.
func buildCard(info schema.AgentInfo, endpoint string, security *Security) *a2a.AgentCard {
	return buildCardWith(info, cardConfig{
		expose:       info.Expose,
		endpoint:     endpoint,
		security:     security,
		capabilities: a2a.AgentCapabilities{Streaming: true},
		transports:   []Transport{TransportJSONRPC},
	})
}

// buildCardWith builds one AgentCard. A card describes a single agent (one card
// = one agent); its URLs are where the matching bindings are mounted.
func buildCardWith(info schema.AgentInfo, cfg cardConfig) *a2a.AgentCard {
	name := string(info.ID)
	description := ""
	if info.Description != nil {
		description = *info.Description
	}
	version := "1.0.0"
	var documentationURL, iconURL string
	var provider *a2a.AgentProvider
	// Non-nil empty by default so the serialized card always carries a "skills"
	// field (an empty array, never null) when the agent configures none.
	skills := []a2a.AgentSkill{}
	inputModes := []string{"text/plain", "application/json", "image/png", "image/jpeg", "image/gif", "image/webp"}
	outputModes := []string{"text/plain", "application/json"}
	if cfg.expose != nil {
		expose := cfg.expose
		if expose.Name != "" {
			name = expose.Name
		}
		if expose.Description != "" {
			description = expose.Description
		}
		if expose.Version != "" {
			version = expose.Version
		}
		documentationURL = expose.DocumentationURL
		iconURL = expose.IconURL
		if expose.Provider != nil {
			provider = &a2a.AgentProvider{Org: expose.Provider.Organization, URL: expose.Provider.URL}
		}
		if len(expose.InputModes) > 0 {
			inputModes = expose.InputModes
		}
		if len(expose.OutputModes) > 0 {
			outputModes = expose.OutputModes
		}
		skills = cardSkills(expose.Skills)
	}
	card := &a2a.AgentCard{
		Name:             name,
		Description:      description,
		Version:          version,
		DocumentationURL: documentationURL,
		IconURL:          iconURL,
		Provider:         provider,
		Capabilities:     cfg.capabilities,
		// Input: text prompts, structured data parts, and the image types the read
		// tool can ingest. Output: assistant text and structured (data) parts.
		DefaultInputModes:  inputModes,
		DefaultOutputModes: outputModes,
		Skills:             skills,
	}
	for _, transport := range cfg.transports {
		switch transport {
		case TransportJSONRPC:
			if cfg.endpoint != "" {
				card.SupportedInterfaces = append(card.SupportedInterfaces,
					a2a.NewAgentInterface(cfg.endpoint, a2a.TransportProtocolJSONRPC))
			}
		case TransportREST:
			if cfg.endpoint != "" {
				card.SupportedInterfaces = append(card.SupportedInterfaces,
					a2a.NewAgentInterface(cfg.endpoint+"rest", a2a.TransportProtocolHTTPJSON))
			}
		}
	}
	if cfg.security != nil {
		scheme := a2a.SecuritySchemeName(cfg.security.securitySchemeName())
		card.SecuritySchemes = a2a.NamedSecuritySchemes{
			scheme: a2a.HTTPAuthSecurityScheme{Scheme: "Bearer"},
		}
		card.SecurityRequirements = a2a.SecurityRequirementsOptions{
			{scheme: a2a.SecuritySchemeScopes{}},
		}
	}
	return card
}

// cardSkills maps configured skills to card skills, defaulting a name to its ID.
// It returns a non-nil (possibly empty) slice so the serialized card always
// carries a "skills" field (an empty array, never null). An agent that
// configures no skills therefore advertises none, rather than a synthetic
// self-skill that merely repeats the card's name/description.
func cardSkills(skills []schema.AgentSkill) []a2a.AgentSkill {
	out := make([]a2a.AgentSkill, 0, len(skills))
	for _, skill := range skills {
		name := skill.Name
		if name == "" {
			name = skill.ID
		}
		out = append(out, a2a.AgentSkill{
			ID:          skill.ID,
			Name:        name,
			Description: skill.Description,
			Tags:        append([]string{}, skill.Tags...), // non-nil so tags serialize as [], not null
			Examples:    skill.Examples,
			InputModes:  skill.InputModes,
			OutputModes: skill.OutputModes,
		})
	}
	return out
}
