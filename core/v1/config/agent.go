package config

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/schema"
)

// V1AgentInfo mirrors ConfigAgentV1.Info (after its normalize transform).
type V1AgentInfo struct {
	Model       *string             `json:"model,omitempty"`
	Variant     *string             `json:"variant,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	Prompt      *string             `json:"prompt,omitempty"`
	Tools       json.RawMessage     `json:"tools,omitempty"`
	Disable     *bool               `json:"disable,omitempty"`
	Description *string             `json:"description,omitempty"`
	Mode        *string             `json:"mode,omitempty"`
	Hidden      *bool               `json:"hidden,omitempty"`
	Options     map[string]any      `json:"options,omitempty"`
	Color       *string             `json:"color,omitempty"`
	Steps       *schema.PositiveInt `json:"steps,omitempty"`
	MaxSteps    *schema.PositiveInt `json:"maxSteps,omitempty"`
	Permission  V1PermissionInfo    `json:"permission,omitempty"`
}

var v1KnownAgentKeys = map[string]bool{
	"name":        true,
	"model":       true,
	"variant":     true,
	"prompt":      true,
	"description": true,
	"temperature": true,
	"top_p":       true,
	"mode":        true,
	"hidden":      true,
	"color":       true,
	"steps":       true,
	"maxSteps":    true,
	"options":     true,
	"permission":  true,
	"disable":     true,
	"tools":       true,
}

// UnmarshalJSON mirrors ConfigAgentV1's normalize: unknown keys move into
// `options`.
func (a *V1AgentInfo) UnmarshalJSON(data []byte) error {
	type plain V1AgentInfo
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*a = V1AgentInfo(value)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, entry := range raw {
		if v1KnownAgentKeys[key] {
			continue
		}
		var decoded any
		if err := json.Unmarshal(entry, &decoded); err != nil {
			continue
		}
		if a.Options == nil {
			a.Options = map[string]any{}
		}
		a.Options[key] = decoded
	}
	return nil
}

// EffectiveSteps mirrors `steps = agent.steps ?? agent.maxSteps`.
func (a V1AgentInfo) EffectiveSteps() *schema.PositiveInt {
	if a.Steps != nil {
		return a.Steps
	}
	return a.MaxSteps
}
