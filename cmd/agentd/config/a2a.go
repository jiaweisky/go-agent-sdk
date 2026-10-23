package config

import "fmt"

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

// ValidateCardSkills checks one agent card's skill entries.
func ValidateCardSkills(agentID string, skills []SkillConfig) error {
	return validateSkills(agentID, skills)
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
