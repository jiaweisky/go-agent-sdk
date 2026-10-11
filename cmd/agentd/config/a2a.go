package config

import (
	"fmt"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
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

// ExposeAgents marks each named agent as exposed (registering its AgentExpose)
// and returns the resulting agent infos. cards optionally overrides an agent's
// card; configured skills are validated as well formed.
func ExposeAgents(client *agent.Client, names []string, cards map[string]CardConfig, modes map[string][]string, defaultModel string) ([]schema.AgentInfo, error) {
	exposed := make([]schema.AgentInfo, 0, len(names))
	for _, name := range names {
		info := client.Agents().Get(schema.AgentID(name))
		if info == nil {
			return nil, fmt.Errorf("unknown agent %q", name)
		}
		expose := &schema.AgentExpose{}
		if override, ok := cards[name]; ok {
			if err := validateSkills(name, override.Skills); err != nil {
				return nil, err
			}
			applyCard(expose, override)
		}
		// Advertise the agent's model media modes on the card when the card
		// config did not set them.
		if len(expose.InputModes) == 0 {
			modelID := defaultModel
			if info.Model != nil {
				modelID = string(info.Model.ID)
			}
			if agentModes := modes[modelID]; len(agentModes) > 0 {
				expose.InputModes = agentModes
			}
		}
		if err := client.RegisterAgent(name, agent.AgentConfig{Expose: expose}); err != nil {
			return nil, fmt.Errorf("expose agent %q: %w", name, err)
		}
		exposed = append(exposed, *client.Agents().Get(schema.AgentID(name)))
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

// modalityMIMEs maps declared modalities to A2A input MIME modes.
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
func applyCard(expose *schema.AgentExpose, card CardConfig) {
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
			Tags:        skill.Tags,
			Examples:    skill.Examples,
			InputModes:  skill.InputModes,
			OutputModes: skill.OutputModes,
		})
	}
}

// validateCards checks the skill entries of every agent card override.
func validateCards(cards map[string]CardConfig) error {
	for agentID, card := range cards {
		if err := validateSkills(agentID, card.Skills); err != nil {
			return err
		}
	}
	return nil
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
