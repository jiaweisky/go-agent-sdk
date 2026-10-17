package config

import (
	"encoding/json"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// ApplyDirAgents merges agents from each directory's {agent,agents}/**/*.md and
// {mode,modes}/*.md into the agent service. Later directories and files win.
// base is merged under each agent's own rules (see core.DefaultAgentPermissions),
// so a configured agent inherits the defaults like a built-in one.
func ApplyDirAgents(agents *core.Agent, dirs []string, global core.Global, base core.PermissionRuleset) error {
	_, err := agents.Transform(func(draft core.AgentDraft) error {
		for _, dir := range dirs {
			entries, err := LoadAgentEntries(dir)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				info, ok := decodeAgentDocument(entry)
				if !ok {
					continue
				}
				id := schema.AgentID(entry.Name)
				if info.Disabled != nil && *info.Disabled {
					draft.Remove(id)
					continue
				}
				draft.Update(id, func(agent *schema.AgentInfo) {
					applyAgentInfo(agent, info, global.Home, base)
				})
			}
		}
		return nil
	})
	return err
}

// decodeAgentDocument decodes an agent file as a v2 agent: the frontmatter keys
// plus the body as the system prompt.
func decodeAgentDocument(entry AgentEntry) (AgentInfo, bool) {
	merged := make(map[string]any, len(entry.Data)+1)
	for key, value := range entry.Data {
		merged[key] = value
	}
	merged["system"] = entry.Content
	encoded, err := json.Marshal(merged)
	if err != nil {
		return AgentInfo{}, false
	}
	var info AgentInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		return AgentInfo{}, false
	}
	if entry.Primary {
		mode := "primary"
		info.Mode = &mode
	}
	return info, true
}

func applyAgentInfo(target *schema.AgentInfo, item AgentInfo, home string, base core.PermissionRuleset) {
	if item.Model != nil {
		parts := core.ParseModel(*item.Model)
		var variant *schema.ModelVariantID
		if target.Model != nil {
			variant = target.Model.Variant
		}
		target.Model = &schema.ModelRef{ID: parts.ModelID, ProviderID: parts.ProviderID, Variant: variant}
	}
	if item.Variant != nil && target.Model != nil {
		variant := schema.ModelVariantID(*item.Variant)
		target.Model.Variant = &variant
	}
	if item.Request != nil {
		if target.Request.Headers == nil {
			target.Request.Headers = map[string]string{}
		}
		if target.Request.Body == nil {
			target.Request.Body = map[string]any{}
		}
		for key, value := range item.Request.Headers {
			target.Request.Headers[key] = value
		}
		for key, value := range item.Request.Body {
			target.Request.Body[key] = value
		}
	}
	if item.System != nil {
		target.System = item.System
	}
	if item.Description != nil {
		target.Description = item.Description
	}
	if item.Mode != nil {
		target.Mode = *item.Mode
	}
	if item.Hidden != nil {
		target.Hidden = *item.Hidden
	}
	if item.Color != nil {
		color := schema.AgentColor(*item.Color)
		target.Color = &color
	}
	if item.Steps != nil {
		target.Steps = item.Steps
	}
	if item.Permissions != nil {
		target.Permissions = append(target.Permissions, expandPermissions(*item.Permissions, home)...)
	}
	if item.Backend != nil {
		target.Backend = item.Backend
	}
	// The base goes first so the agent's own rules win on conflicts.
	target.Permissions = core.PermissionMerge(base, target.Permissions)
}

var agentPathActions = map[string]bool{
	"external_directory": true,
	"read":               true,
	"edit":               true,
}

func expandPermissions(rules schema.PermissionRuleset, home string) schema.PermissionRuleset {
	result := make(schema.PermissionRuleset, 0, len(rules))
	for _, rule := range rules {
		if agentPathActions[rule.Action] {
			rule.Resource = expandHome(rule.Resource, home)
		}
		result = append(result, rule)
	}
	return result
}

func expandHome(resource, home string) string {
	switch {
	case strings.HasPrefix(resource, "~/"):
		return home + resource[1:]
	case resource == "~":
		return home
	case resource == "$HOME":
		return home
	case strings.HasPrefix(resource, "$HOME/"):
		return home + resource[5:]
	case strings.HasPrefix(resource, "$HOME\\"):
		return home + resource[5:]
	}
	return resource
}
