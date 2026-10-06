package server

import (
	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// buildCard builds the AgentCard for one exposed agent. A card describes a
// single agent (one card = one agent); its URL is where the JSON-RPC endpoint
// is mounted. When security is set, the card declares the bearer scheme.
func buildCard(info schema.AgentInfo, endpoint string, security *Security) *a2a.AgentCard {
	name := string(info.ID)
	description := ""
	if info.Description != nil {
		description = *info.Description
	}
	version := "1.0.0"
	var documentationURL, iconURL string
	var provider *a2a.AgentProvider
	var skills []a2a.AgentSkill
	if info.Expose != nil {
		expose := info.Expose
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
		skills = cardSkills(expose.Skills)
	}
	if len(skills) == 0 {
		skills = []a2a.AgentSkill{{ID: string(info.ID), Name: name, Description: description, Tags: []string{"agent"}}}
	}
	card := &a2a.AgentCard{
		Name:             name,
		Description:      description,
		Version:          version,
		DocumentationURL: documentationURL,
		IconURL:          iconURL,
		Provider:         provider,
		Capabilities: a2a.AgentCapabilities{
			Streaming: true,
		},
		// Input: text prompts, structured data parts, and the image types the read
		// tool can ingest. Output: assistant text and structured (data) parts.
		DefaultInputModes:  []string{"text/plain", "application/json", "image/png", "image/jpeg", "image/gif", "image/webp"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills:             skills,
	}
	if endpoint != "" {
		card.SupportedInterfaces = []*a2a.AgentInterface{
			a2a.NewAgentInterface(endpoint, a2a.TransportProtocolJSONRPC),
		}
	}
	if security != nil {
		scheme := a2a.SecuritySchemeName(security.securitySchemeName())
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
func cardSkills(skills []schema.AgentSkill) []a2a.AgentSkill {
	if len(skills) == 0 {
		return nil
	}
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
			Tags:        skill.Tags,
			Examples:    skill.Examples,
			InputModes:  skill.InputModes,
			OutputModes: skill.OutputModes,
		})
	}
	return out
}
