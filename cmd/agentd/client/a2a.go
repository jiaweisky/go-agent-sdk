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

// ModelModes maps each catalog model id to the A2A input MIME modes its
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

// applyCard copies a card override into an AgentExpose.
func applyCard(expose *schema.AgentExpose, card config.CardConfig) {
	expose.Name = card.Name
	expose.Description = card.Description
	expose.Version = card.Version
	expose.DocumentationURL = card.DocumentationURL
	expose.IconURL = card.IconURL
	if card.Provider != nil {
		expose.Provider = &schema.AgentProvider{
			Organization: card.Provider.Organization,
			URL:          card.Provider.URL,
		}
	}

	expose.Skills = make([]schema.AgentSkill, 0, len(card.Skills))
	for _, skill := range card.Skills {
		expose.Skills = append(expose.Skills, schema.AgentSkill{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        slices.Clone(skill.Tags),
			Examples:    slices.Clone(skill.Examples),
			InputModes:  slices.Clone(skill.InputModes),
			OutputModes: slices.Clone(skill.OutputModes),
		})
	}
}
