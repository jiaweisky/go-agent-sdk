package client

import (
	"fmt"
	"slices"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// ExposeAgents marks each named agent as exposed (registering its AgentExpose)
// and returns the resulting agent infos. cards optionally overrides an agent's
// card; configured skills are validated as well formed.
func ExposeAgents(
	client *agent.Client,
	names []string,
	cards map[string]config.CardConfig,
	inputModes map[string][]string,
	defaultModel string,
) ([]schema.AgentInfo, error) {
	type plan struct {
		name   string
		expose *schema.AgentExpose
	}

	plans := make([]plan, 0, len(names))
	for _, name := range names {
		info := client.Agents().Get(schema.AgentID(name))
		if info == nil {
			return nil, fmt.Errorf("unknown agent %q", name)
		}

		expose := &schema.AgentExpose{}
		if override, ok := cards[name]; ok {
			if err := config.ValidateCardSkills(name, override.Skills); err != nil {
				return nil, err
			}
			applyCard(expose, override)
		}

		// CardConfig has no inputModes override, so derive them from the model.
		if len(expose.InputModes) == 0 {
			modelID := defaultModel
			if info.Model != nil {
				modelID = string(info.Model.ID)
			}
			if agentModes := inputModes[modelID]; len(agentModes) > 0 {
				expose.InputModes = agentModes
			}
		}

		plans = append(plans, plan{name: name, expose: expose})
	}

	exposed := make([]schema.AgentInfo, 0, len(plans))
	for _, p := range plans {
		if err := client.RegisterAgent(p.name, agent.AgentConfig{Expose: p.expose}); err != nil {
			return nil, fmt.Errorf("expose agent %q: %w", p.name, err)
		}

		info := client.Agents().Get(schema.AgentID(p.name))
		if info == nil {
			return nil, fmt.Errorf("expose agent %q: agent disappeared after registration", p.name)
		}

		exposed = append(exposed, *info)
	}

	return exposed, nil
}

// applyCard copies a card override into an AgentExpose.
func applyCard(expose *schema.AgentExpose, card config.CardConfig) {
	expose.Path = card.CardPath
	expose.Name = card.Name
	expose.Description = card.Description
	expose.Version = card.Version
	expose.DocumentationURL = card.DocumentationURL
	expose.IconURL = card.IconURL
	expose.InputModes = slices.Clone(card.DefaultInputModes)
	expose.OutputModes = slices.Clone(card.DefaultOutputModes)

	if card.Provider != nil {
		expose.Provider = &schema.AgentProvider{
			Organization: card.Provider.Organization,
			URL:          card.Provider.URL,
		}
	}

	expose.Skills = cardSkills(card.Skills)
}

// ExtendedExposeFrom applies an extended-card override to an already-resolved
// public expose (including model-derived input modes).
func ExtendedExposeFrom(base schema.AgentExpose, card config.CardConfig) schema.AgentExpose {
	base.Path = ""

	if card.Extended != nil {
		applyExtendedCard(&base, *card.Extended)
	}

	return base
}

// applyExtendedCard merges non-empty extended-card fields over the public card.
func applyExtendedCard(expose *schema.AgentExpose, extended config.ExtendedCardConfig) {
	if extended.Name != "" {
		expose.Name = extended.Name
	}

	if extended.Description != "" {
		expose.Description = extended.Description
	}

	if extended.Version != "" {
		expose.Version = extended.Version
	}

	if extended.DocumentationURL != "" {
		expose.DocumentationURL = extended.DocumentationURL
	}

	if extended.IconURL != "" {
		expose.IconURL = extended.IconURL
	}

	if extended.Provider != nil {
		expose.Provider = &schema.AgentProvider{
			Organization: extended.Provider.Organization,
			URL:          extended.Provider.URL,
		}
	}

	if extended.DefaultInputModes != nil {
		expose.InputModes = slices.Clone(extended.DefaultInputModes)
	}

	if extended.DefaultOutputModes != nil {
		expose.OutputModes = slices.Clone(extended.DefaultOutputModes)
	}

	if extended.Skills != nil {
		expose.Skills = cardSkills(extended.Skills)
	}
}

// cardSkills maps configured skill labels to SDK card skills.
func cardSkills(skills []config.SkillConfig) []schema.AgentSkill {
	out := make([]schema.AgentSkill, 0, len(skills))

	for _, skill := range skills {
		out = append(out, schema.AgentSkill{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        slices.Clone(skill.Tags),
			Examples:    slices.Clone(skill.Examples),
			InputModes:  slices.Clone(skill.InputModes),
			OutputModes: slices.Clone(skill.OutputModes),
		})
	}

	return out
}

// ModelModes maps each registry model id to the A2A input MIME modes its
// modalities imply.
func ModelModes(models []schema.ModelInfo) map[string][]string {
	out := make(map[string][]string, len(models))
	for _, model := range models {
		out[string(model.ID)] = modalityMIMEs(model.Capabilities.Input)
	}

	return out
}

// modalityMIMEs maps declared modalities to A2A input MIME modes. The result
// always includes the text/plain and application/json baseline.
func modalityMIMEs(modalities []string) []string {
	if len(modalities) == 0 {
		return nil
	}

	modes := []string{"text/plain", "application/json"}
	for _, modality := range modalities {
		switch modality {
		case "image":
			modes = append(modes, "image/png", "image/jpeg", "image/gif", "image/webp")
		case "audio":
			modes = append(modes, "audio/wav", "audio/mp3")
		case "video":
			modes = append(modes, "video/mp4", "video/webm", "video/quicktime")
		case "pdf":
			modes = append(modes, "application/pdf")
		}
	}

	return modes
}

// registerA2APeers registers each configured A2A peer as an agent with an A2A
// backend. A peer whose name collides with an existing agent (a built-in or a
// ConfigDirs agent) is rejected rather than silently rewriting that agent's
// backend.
func registerA2APeers(client *agent.Client, peers []config.A2APeer) error {
	for _, peer := range peers {
		if client.Agents().Get(schema.AgentID(peer.Name)) != nil {
			return fmt.Errorf("peer %s: an agent with that name already exists", peer.Name)
		}

		if err := client.RegisterAgent(peer.Name, a2aPeerAgentConfig(peer)); err != nil {
			return fmt.Errorf("peer %s: %w", peer.Name, err)
		}
	}

	return nil
}

// a2aPeerAgentConfig maps a configured peer to the agent config that registers
// it with an A2A backend.
func a2aPeerAgentConfig(peer config.A2APeer) agent.AgentConfig {
	mode := peer.ModeOrDefault()

	return agent.AgentConfig{
		Description: peer.Description,
		Mode:        &mode,
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A: &schema.AgentA2ABackend{
				Endpoint: peer.Endpoint,
				CardURL:  peer.CardURL,
				Headers:  peer.Headers,
			},
		},
	}
}
