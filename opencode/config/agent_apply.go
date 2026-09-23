package config

import (
	"encoding/json"
	"strings"

	"github.com/user/go-agent-sdk/core"
	coreconfig "github.com/user/go-agent-sdk/core/config"
	v1config "github.com/user/go-agent-sdk/core/v1/config"
	"github.com/user/go-agent-sdk/schema"
)

// ApplyConfigAgents mirrors ConfigAgentPlugin (core/src/config/plugin/agent.ts):
// it merges agents and permissions from config documents and legacy markdown
// agent/mode files into the agent service.
func ApplyConfigAgents(agents *core.Agent, entries []core.ConfigEntry, global core.Global) error {
	var documents []core.ConfigInfo
	for _, entry := range entries {
		switch item := entry.(type) {
		case core.ConfigDocument:
			documents = append(documents, item.Info)
		case core.ConfigDirectory:
			legacy, err := loadDirectoryAgents(string(item.Path))
			if err != nil {
				return err
			}
			documents = append(documents, legacy...)
		}
	}

	var permissions schema.PermissionRuleset
	for _, document := range documents {
		permissions = append(permissions, expandPermissions(document.Permissions, global.Home)...)
	}

	configuredDefault := core.Latest(entries, func(info core.ConfigInfo) *string { return info.DefaultAgent })

	_, err := agents.Transform(func(draft core.AgentDraft) error {
		if configuredDefault != nil {
			id := schema.AgentID(*configuredDefault)
			draft.Default(&id)
		}
		for _, current := range draft.List() {
			draft.Update(current.ID, func(agent *schema.AgentInfo) {
				agent.Permissions = append(agent.Permissions, permissions...)
			})
		}

		for _, document := range documents {
			for id, item := range document.Agents {
				agentID := schema.AgentID(id)
				if item.Disabled != nil && *item.Disabled {
					draft.Remove(agentID)
					continue
				}
				exists := draft.Get(agentID) != nil
				draft.Update(agentID, func(agent *schema.AgentInfo) {
					applyAgentInfo(agent, item, global.Home, exists, permissions)
				})
			}
		}
		return nil
	})
	return err
}

// loadDirectoryAgents expands a config directory's legacy markdown files into
// synthesized documents, one per file, in scan order.
func loadDirectoryAgents(dir string) ([]core.ConfigInfo, error) {
	entries, err := LoadAgentEntries(dir)
	if err != nil {
		return nil, err
	}
	documents := make([]core.ConfigInfo, 0, len(entries))
	for _, entry := range entries {
		info, ok := decodeAgentDocument(entry)
		if !ok {
			continue
		}
		documents = append(documents, core.ConfigInfo{
			Agents: map[string]coreconfig.AgentInfo{entry.Name: info},
		})
	}
	return documents, nil
}

// configAgentKeys mirrors ConfigAgentPlugin's agentKeys: when every frontmatter
// key is known here the file is decoded as a v2 agent, otherwise as legacy v1.
var configAgentKeys = map[string]bool{
	"model": true, "variant": true, "request": true, "system": true,
	"description": true, "mode": true, "hidden": true, "color": true,
	"steps": true, "disabled": true, "permissions": true,
}

// decodeAgentDocument mirrors ConfigAgentPlugin.decode: it chooses the v2 or
// legacy decode based on the frontmatter keys and applies the body as the
// system prompt (v2) or prompt (legacy).
func decodeAgentDocument(entry AgentEntry) (coreconfig.AgentInfo, bool) {
	legacy := false
	for key := range entry.Data {
		if !configAgentKeys[key] {
			legacy = true
			break
		}
	}
	if legacy {
		return migrateAgentInfo(entry.Info)
	}
	merged := make(map[string]any, len(entry.Data)+1)
	for key, value := range entry.Data {
		merged[key] = value
	}
	merged["system"] = entry.Content
	encoded, err := json.Marshal(merged)
	if err != nil {
		return coreconfig.AgentInfo{}, false
	}
	var info coreconfig.AgentInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		return coreconfig.AgentInfo{}, false
	}
	if entry.Primary {
		mode := "primary"
		info.Mode = &mode
	}
	return info, true
}

func migrateAgentInfo(info v1config.V1AgentInfo) (coreconfig.AgentInfo, bool) {
	encoded, err := json.Marshal(v1config.V1MigrateAgent(info))
	if err != nil {
		return coreconfig.AgentInfo{}, false
	}
	var result coreconfig.AgentInfo
	if err := json.Unmarshal(encoded, &result); err != nil {
		return coreconfig.AgentInfo{}, false
	}
	return result, true
}

func applyAgentInfo(
	target *schema.AgentInfo,
	item coreconfig.AgentInfo,
	home string,
	exists bool,
	permissions schema.PermissionRuleset,
) {
	if !exists {
		target.Permissions = append(target.Permissions, permissions...)
	}
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
