package config

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/schema"
)

var v1OnlyKeys = map[string]bool{
	"logLevel":           true,
	"server":             true,
	"command":            true,
	"reference":          true,
	"snapshot":           true,
	"plugin":             true,
	"autoshare":          true,
	"disabled_providers": true,
	"enabled_providers":  true,
	"small_model":        true,
	"mode":               true,
	"agent":              true,
	"provider":           true,
	"permission":         true,
	"tools":              true,
	"attachment":         true,
	"layout":             true,
}

// V1IsV1 mirrors ConfigMigrateV1.isV1.
func V1IsV1(input map[string]json.RawMessage) bool {
	for key := range input {
		if v1OnlyKeys[key] {
			return true
		}
	}
	return false
}

// V1Migrate mirrors ConfigMigrateV1.migrate, returning the v2-shaped document.
func V1Migrate(info V1Info) map[string]any {
	out := map[string]any{}
	if info.Schema != nil {
		out["$schema"] = *info.Schema
	}
	if info.Shell != nil {
		out["shell"] = *info.Shell
	}
	if info.Model != nil {
		out["model"] = *info.Model
	}
	if info.DefaultAgent != nil {
		out["default_agent"] = *info.DefaultAgent
	}
	if len(info.Autoupdate) > 0 {
		out["autoupdate"] = info.Autoupdate
	}
	share := info.Share
	if share == nil && info.Autoshare != nil && *info.Autoshare {
		value := "auto"
		share = &value
	}
	if share != nil {
		out["share"] = *share
	}
	if info.Enterprise != nil {
		out["enterprise"] = info.Enterprise
	}
	if info.Username != nil {
		out["username"] = *info.Username
	}
	if rules := v1Permissions(info.Permission, info.Tools); len(rules) > 0 {
		out["permissions"] = rules
	}
	if agents := v1Agents(info); agents != nil {
		out["agents"] = agents
	}
	if info.Snapshot != nil {
		out["snapshots"] = *info.Snapshot
	}
	if info.Watcher != nil {
		out["watcher"] = info.Watcher
	}
	if info.Formatter.Enabled != nil || info.Formatter.Entries != nil {
		out["formatter"] = info.Formatter
	}
	if info.LSP.Enabled != nil || info.LSP.Servers != nil {
		out["lsp"] = info.LSP
	}
	if info.Attachment != nil {
		out["attachments"] = info.Attachment
	}
	if info.ToolOutput != nil {
		out["tool_output"] = info.ToolOutput
	}
	if mcp := v1MCP(info); mcp != nil {
		out["mcp"] = mcp
	}
	if info.Compaction != nil {
		out["compaction"] = map[string]any{
			"auto":  info.Compaction.Auto,
			"prune": info.Compaction.Prune,
			"keep": map[string]any{
				"tokens": info.Compaction.PreserveRecentTokens,
			},
			"buffer": info.Compaction.Reserved,
		}
	}
	if info.Skills != nil {
		skills := make([]string, 0, len(info.Skills.Paths)+len(info.Skills.URLs))
		skills = append(skills, info.Skills.Paths...)
		skills = append(skills, info.Skills.URLs...)
		out["skills"] = skills
	}
	if info.Command != nil {
		out["commands"] = info.Command
	}
	if info.Instructions != nil {
		out["instructions"] = info.Instructions
	}
	if info.References != nil {
		out["references"] = info.References
	} else if info.Reference != nil {
		out["references"] = info.Reference
	}
	if info.Plugin != nil {
		plugins := make([]any, 0, len(info.Plugin))
		for _, plugin := range info.Plugin {
			if plugin.Name != nil {
				plugins = append(plugins, *plugin.Name)
				continue
			}
			plugins = append(plugins, map[string]any{"package": plugin.Package, "options": plugin.Options})
		}
		out["plugins"] = plugins
	}
	if info.Experimental != nil && len(info.Experimental.Policies) > 0 {
		out["experimental"] = map[string]any{"policies": info.Experimental.Policies}
	}
	if providers := v1Providers(info.Provider); providers != nil {
		out["providers"] = providers
	}
	return out
}

func v1Agents(info V1Info) map[string]any {
	type agentEntry struct {
		name  string
		agent V1AgentInfo
		mode  *string
	}
	entries := make([]agentEntry, 0, len(info.Agent)+len(info.Mode))
	for name, agent := range info.Agent {
		entries = append(entries, agentEntry{name: name, agent: agent})
	}
	for name, agent := range info.Mode {
		primary := "primary"
		entries = append(entries, agentEntry{name: name, agent: agent, mode: &primary})
	}
	if len(entries) == 0 {
		return nil
	}
	result := make(map[string]any, len(entries))
	for _, entry := range entries {
		agent := entry.agent
		if entry.mode != nil {
			agent.Mode = entry.mode
		}
		result[entry.name] = V1MigrateAgent(agent)
	}
	return result
}

// V1MigrateAgent mirrors ConfigMigrateV1.migrateAgent.
func V1MigrateAgent(info V1AgentInfo) map[string]any {
	body := map[string]any{}
	for key, value := range info.Options {
		body[key] = value
	}
	if info.Temperature != nil {
		body["temperature"] = *info.Temperature
	}
	if info.TopP != nil {
		body["top_p"] = *info.TopP
	}
	out := map[string]any{}
	if info.Model != nil {
		out["model"] = *info.Model
	}
	if info.Variant != nil {
		out["variant"] = *info.Variant
	}
	if len(body) > 0 {
		out["request"] = map[string]any{"body": body}
	}
	if info.Prompt != nil {
		out["system"] = *info.Prompt
	}
	if info.Description != nil {
		out["description"] = *info.Description
	}
	if info.Mode != nil {
		out["mode"] = *info.Mode
	}
	if info.Hidden != nil {
		out["hidden"] = *info.Hidden
	}
	if info.Color != nil {
		out["color"] = *info.Color
	}
	if steps := info.EffectiveSteps(); steps != nil {
		out["steps"] = *steps
	}
	if info.Disable != nil {
		out["disabled"] = *info.Disable
	}
	if rules := v1AgentPermissions(info); len(rules) > 0 {
		out["permissions"] = rules
	}
	return out
}

func v1AgentPermissions(info V1AgentInfo) []schema.PermissionRule {
	var rules []schema.PermissionRule
	upsert := func(action, resource string, effect schema.PermissionEffect) {
		for i := range rules {
			if rules[i].Action == action && rules[i].Resource == resource {
				rules[i].Effect = effect
				return
			}
		}
		rules = append(rules, schema.PermissionRule{Action: action, Resource: resource, Effect: effect})
	}
	if len(info.Tools) > 0 {
		if entries, err := parseOrderedObject(info.Tools); err == nil {
			for _, entry := range entries {
				var enabled bool
				if err := json.Unmarshal(entry.Raw, &enabled); err != nil {
					continue
				}
				effect := schema.PermissionEffectDeny
				if enabled {
					effect = schema.PermissionEffectAllow
				}
				if entry.Key == "write" || entry.Key == "edit" || entry.Key == "patch" {
					upsert("edit", "*", effect)
					continue
				}
				upsert(entry.Key, "*", effect)
			}
		}
	}
	for _, rule := range v1Permissions(info.Permission, nil) {
		upsert(rule.Action, rule.Resource, rule.Effect)
	}
	return rules
}

func v1MCP(info V1Info) map[string]any {
	servers := map[string]any{}
	for name, server := range info.MCP {
		if server.Type == nil {
			continue
		}
		servers[name] = v1MigrateMCP(server)
	}
	var timeout *schema.PositiveInt
	if info.Experimental != nil {
		timeout = info.Experimental.MCPTimeout
	}
	if timeout == nil && len(servers) == 0 {
		return nil
	}
	out := map[string]any{"servers": servers}
	if timeout != nil {
		out["timeout"] = map[string]any{"request": *timeout}
	}
	return out
}

func v1MigrateMCP(info V1MCPServer) map[string]any {
	out := map[string]any{"type": *info.Type}
	if info.Enabled != nil {
		out["disabled"] = !*info.Enabled
	}
	if info.Timeout != nil {
		out["timeout"] = map[string]any{"request": *info.Timeout}
	}
	if *info.Type == "local" {
		out["command"] = info.Command
		if info.Cwd != nil {
			out["cwd"] = *info.Cwd
		}
		if info.Environment != nil {
			out["environment"] = info.Environment
		}
		return out
	}
	if info.URL != nil {
		out["url"] = *info.URL
	}
	if info.Headers != nil {
		out["headers"] = info.Headers
	}
	if info.OAuth != nil {
		oauth := map[string]any{}
		if info.OAuth.ClientID != nil {
			oauth["client_id"] = *info.OAuth.ClientID
		}
		if info.OAuth.ClientSecret != nil {
			oauth["client_secret"] = *info.OAuth.ClientSecret
		}
		if info.OAuth.Scope != nil {
			oauth["scope"] = *info.OAuth.Scope
		}
		if info.OAuth.CallbackPort != nil {
			oauth["callback_port"] = *info.OAuth.CallbackPort
		}
		if info.OAuth.RedirectURI != nil {
			oauth["redirect_uri"] = *info.OAuth.RedirectURI
		}
		out["oauth"] = oauth
	}
	return out
}

func v1Providers(info map[string]V1ProviderInfo) map[string]any {
	if info == nil {
		return nil
	}
	result := make(map[string]any, len(info))
	for name, provider := range info {
		result[name] = v1MigrateProvider(provider)
	}
	return result
}

func v1MigrateProvider(info V1ProviderInfo) map[string]any {
	lowerer := V1ProviderOptionsGet(info.NPM)
	options := info.Options
	if options == nil {
		options = map[string]any{}
	}
	lowered := lowerer.Provider(options)
	url := info.API
	if url == nil {
		url = lowered.URL
	}
	out := map[string]any{}
	if info.Name != nil {
		out["name"] = *info.Name
	}
	if info.Env != nil {
		out["env"] = info.Env
	}
	if info.NPM != nil {
		api := map[string]any{"type": "aisdk", "package": *info.NPM}
		if url != nil {
			api["url"] = *url
		}
		settings := lowered.Settings
		if settings == nil {
			settings = map[string]any{}
		}
		api["settings"] = settings
		out["api"] = api
	}
	if info.Options != nil {
		request := map[string]any{}
		if lowered.Headers != nil {
			request["headers"] = lowered.Headers
		}
		if lowered.Body != nil {
			request["body"] = lowered.Body
		}
		out["request"] = request
	}
	if info.Models != nil {
		models := make(map[string]any, len(info.Models))
		for name, model := range info.Models {
			models[name] = v1MigrateModel(model, info.NPM)
		}
		out["models"] = models
	}
	return out
}

func v1MigrateModel(info V1ProviderModel, packageName *string) map[string]any {
	packageID := packageName
	if info.Provider != nil && info.Provider.NPM != nil {
		packageID = info.Provider.NPM
	}
	lowerer := V1ProviderOptionsGet(packageID)
	var request map[string]any
	if info.Options != nil {
		request = lowerer.Request(info.Options)
	}
	var costs []any
	if info.Cost != nil {
		costs = append(costs, map[string]any{
			"input":  info.Cost.Input,
			"output": info.Cost.Output,
			"cache":  v1CostCache(info.Cost.CacheRead, info.Cost.CacheWrite),
		})
		if info.Cost.ContextOver200k != nil {
			over := info.Cost.ContextOver200k
			costs = append(costs, map[string]any{
				"tier":   map[string]any{"type": "context", "size": 200000},
				"input":  over.Input,
				"output": over.Output,
				"cache":  v1CostCache(over.CacheRead, over.CacheWrite),
			})
		}
	}
	var capabilities map[string]any
	modalitiesInputSet := info.Modalities != nil && info.Modalities.Input != nil
	modalitiesOutputSet := info.Modalities != nil && info.Modalities.Output != nil
	if info.ToolCall != nil || modalitiesInputSet || modalitiesOutputSet {
		tools := false
		if info.ToolCall != nil {
			tools = *info.ToolCall
		}
		input := []string{}
		output := []string{}
		if info.Modalities != nil {
			if info.Modalities.Input != nil {
				input = info.Modalities.Input
			}
			if info.Modalities.Output != nil {
				output = info.Modalities.Output
			}
		}
		capabilities = map[string]any{"tools": tools, "input": input, "output": output}
	}
	out := map[string]any{}
	if info.Family != nil {
		out["family"] = *info.Family
	}
	if info.Name != nil {
		out["name"] = *info.Name
	}
	if info.Provider != nil && info.Provider.NPM != nil {
		api := map[string]any{"type": "aisdk", "package": *info.Provider.NPM, "settings": map[string]any{}}
		if info.ID != nil {
			api["id"] = *info.ID
		}
		if info.Provider.API != nil {
			api["url"] = *info.Provider.API
		}
		out["api"] = api
	} else if info.ID != nil {
		out["api"] = map[string]any{"id": *info.ID}
	}
	if capabilities != nil {
		out["capabilities"] = capabilities
	}
	if info.Headers != nil || request != nil {
		value := map[string]any{}
		if info.Headers != nil {
			value["headers"] = info.Headers
		}
		if request != nil {
			value["body"] = request
		}
		out["request"] = value
	}
	if info.Variants != nil {
		variants := make([]any, 0, len(info.Variants))
		for id, raw := range info.Variants {
			var options map[string]any
			_ = json.Unmarshal(raw, &options)
			variants = append(variants, map[string]any{"id": id, "body": lowerer.Request(options)})
		}
		out["variants"] = variants
	}
	if costs != nil {
		out["cost"] = costs
	}
	if info.Status != nil && *info.Status == "deprecated" {
		out["disabled"] = true
	}
	if info.Limit != nil {
		limit := map[string]any{
			"context": int(info.Limit.Context),
			"output":  int(info.Limit.Output),
		}
		if info.Limit.Input != nil {
			limit["input"] = int(*info.Limit.Input)
		}
		out["limit"] = limit
	}
	return out
}

func v1CostCache(read, write *float64) map[string]any {
	cache := map[string]any{}
	if read != nil {
		cache["read"] = *read
	}
	if write != nil {
		cache["write"] = *write
	}
	return cache
}
