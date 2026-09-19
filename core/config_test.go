package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	v1config "github.com/user/go-agent-sdk/core/v1/config"
	"github.com/user/go-agent-sdk/schema"
)

func configStr(value string) *string {
	return &value
}

func newTestConfig(t *testing.T, directory, globalDirectory, projectDirectory string) (Config, Policy) {
	t.Helper()
	policy := NewPolicy()
	config, err := NewConfig(ConfigDeps{
		Global: NewGlobal(func(global *Global) { global.Config = globalDirectory }),
		Location: &Location{LocationInfo: schema.LocationInfo{
			Directory: schema.AbsolutePath(directory),
			Project:   schema.LocationProject{Directory: schema.AbsolutePath(projectDirectory)},
		}},
		Policy: policy,
	})
	if err != nil {
		t.Fatalf("new config: %v", err)
	}
	return config, policy
}

func configDocuments(entries []ConfigEntry) []ConfigDocument {
	var documents []ConfigDocument
	for _, entry := range entries {
		if document, ok := entry.(ConfigDocument); ok {
			documents = append(documents, document)
		}
	}
	return documents
}

func TestConfigLatest(t *testing.T) {
	first := "openrouter/openai/gpt-5"
	second := "openrouter/openai/gpt-5.5"
	entries := []ConfigEntry{
		ConfigDocument{Info: ConfigInfo{Model: &first}},
		ConfigDirectory{Path: schema.AbsolutePath("/skills")},
		ConfigDocument{Info: ConfigInfo{}},
		ConfigDocument{Info: ConfigInfo{Model: &second}},
	}
	if got := Latest(entries, func(info ConfigInfo) *string { return info.Model }); got == nil || *got != second {
		t.Fatalf("unexpected model: %v", got)
	}
	if got := Latest(entries, func(info ConfigInfo) *string { return info.DefaultAgent }); got != nil {
		t.Fatalf("expected no default agent, got %v", *got)
	}
}

func TestV1IsV1(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`{"snapshot":false}`, true},
		{`{"snapshot":false,"agents":{}}`, true},
		{`{"reference":{}}`, true},
		{`{"shell":"/bin/zsh","model":"anthropic/claude"}`, false},
		{`{"references":{}}`, false},
	}
	for _, test := range cases {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(test.input), &raw); err != nil {
			t.Fatal(err)
		}
		if got := v1config.V1IsV1(raw); got != test.want {
			t.Fatalf("V1IsV1(%s) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestV1MigrateProviderOptions(t *testing.T) {
	migrated := v1config.V1Migrate(v1config.V1Info{
		Provider: map[string]v1config.V1ProviderInfo{
			"bedrock": {
				NPM: configStr("@ai-sdk/amazon-bedrock"),
				Options: map[string]any{
					"headers": map[string]any{"x-test": "1"},
					"body":    map[string]any{"trace": true},
					"region":  "us-east-1",
					"profile": "dev",
				},
			},
		},
	})
	encoded, err := json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	var info ConfigInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		t.Fatal(err)
	}
	bedrock := info.Providers["bedrock"]
	if bedrock.API == nil || bedrock.API.Type != "aisdk" || bedrock.API.Package != "@ai-sdk/amazon-bedrock" {
		t.Fatalf("unexpected api: %#v", bedrock.API)
	}
	if bedrock.API.Settings["region"] != "us-east-1" || bedrock.API.Settings["profile"] != "dev" {
		t.Fatalf("unexpected settings: %#v", bedrock.API.Settings)
	}
	if bedrock.Request == nil || bedrock.Request.Headers["x-test"] != "1" || bedrock.Request.Body["trace"] != true {
		t.Fatalf("unexpected request: %#v", bedrock.Request)
	}
}

func TestV1MigrateCommand(t *testing.T) {
	migrated := v1config.V1Migrate(v1config.V1Info{
		Command: map[string]v1config.V1CommandInfo{
			"review": {
				Template:    "Review changes",
				Description: configStr("Review code"),
				Agent:       configStr("reviewer"),
				Model:       configStr("anthropic/claude"),
				Variant:     configStr("high"),
				Subtask:     boolPtr(true),
			},
		},
	})
	encoded, err := json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	var info ConfigInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		t.Fatal(err)
	}
	review := info.Commands["review"]
	if review.Template != "Review changes" || *review.Description != "Review code" ||
		*review.Agent != "reviewer" || *review.Model != "anthropic/claude" ||
		*review.Variant != "high" || *review.Subtask != true {
		t.Fatalf("unexpected command: %#v", review)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func TestConfigEmpty(t *testing.T) {
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	config, _ := newTestConfig(t, tmp, global, tmp)
	entries := config.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	directory, ok := entries[0].(ConfigDirectory)
	if !ok || directory.Path != schema.AbsolutePath(global) {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

const testProviderJSON = `{"api":{"type":"native","settings":{}},"request":{"headers":{},"body":{}},"models":{}}`

func TestConfigLoadsJSONAndJSONC(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(tmp, "opencode.json"),
		[]byte(`{"$schema":"base","providers":{"base":`+testProviderJSON+`}}`),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	jsonc := `{
		// Later global files override scalar fields while retaining providers.
		"$schema": "last",
		"providers": { "last": ` + testProviderJSON + ` },
	}`
	if err := os.WriteFile(filepath.Join(tmp, "opencode.jsonc"), []byte(jsonc), 0644); err != nil {
		t.Fatal(err)
	}

	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(documents))
	}
	if *documents[0].Info.Schema != "base" || *documents[1].Info.Schema != "last" {
		t.Fatalf("unexpected schemas: %v %v", *documents[0].Info.Schema, *documents[1].Info.Schema)
	}
	if *documents[0].Path != filepath.Join(tmp, "opencode.json") {
		t.Fatalf("unexpected path: %v", *documents[0].Path)
	}
	if _, ok := documents[1].Info.Providers["last"]; !ok {
		t.Fatalf("expected providers.last")
	}

	// Configuration is read once when the location opens.
	if err := os.WriteFile(filepath.Join(tmp, "opencode.jsonc"), []byte(`{"$schema":"changed"}`), 0644); err != nil {
		t.Fatal(err)
	}
	again := configDocuments(config.Entries())
	if len(again) != 2 || *again[1].Info.Schema != "last" {
		t.Fatalf("expected cached entries, got %#v", again)
	}
}

func TestConfigDoesNotLoadLegacy(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.json"), []byte(`{"$schema":"legacy"}`), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	if documents := configDocuments(config.Entries()); len(documents) != 0 {
		t.Fatalf("expected no documents, got %d", len(documents))
	}
}

func TestConfigAcceptsSchemaMetadata(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "opencode.json")
	contents := `{"shell":"/bin/zsh","experimental":{"policies":[{"effect":"deny","action":"provider.use","resource":"openai"}]},"providers":{"local":` + testProviderJSON + `}}`
	if err := os.WriteFile(file, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(documents))
	}
	if documents[0].Info.Schema != nil {
		t.Fatalf("expected no $schema, got %v", *documents[0].Info.Schema)
	}
	if *documents[0].Info.Shell != "/bin/zsh" {
		t.Fatalf("unexpected shell: %v", *documents[0].Info.Shell)
	}
	policies := documents[0].Info.Experimental.Policies
	if len(policies) != 1 || policies[0].Effect != "deny" || policies[0].Action != "provider.use" || policies[0].Resource != "openai" {
		t.Fatalf("unexpected policies: %#v", policies)
	}
	onDisk, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != contents {
		t.Fatalf("config file was rewritten: %s", onDisk)
	}
}

func TestConfigLoadsSupportedFields(t *testing.T) {
	tmp := t.TempDir()
	contents := `{
		"shell": "/bin/bash",
		"model": "anthropic/claude",
		"default_agent": "reviewer",
		"autoupdate": "notify",
		"share": "disabled",
		"enterprise": { "url": "https://share.example.com" },
		"username": "test-user",
		"permissions": [
			{ "action": "bash", "resource": "*", "effect": "ask" },
			{ "action": "bash", "resource": "git status", "effect": "allow" }
		],
		"agents": {
			"reviewer": {
				"model": "openrouter/openai/gpt-5",
				"variant": "high",
				"request": { "headers": { "x-agent": "reviewer" }, "body": { "reasoningEffort": "high" } },
				"description": "Review changes for correctness",
				"system": "Find regressions.",
				"mode": "subagent",
				"hidden": false,
				"color": "warning",
				"steps": 12,
				"disabled": false,
				"permissions": [{ "action": "edit", "resource": "*", "effect": "deny" }]
			}
		},
		"snapshots": false,
		"watcher": { "ignore": ["node_modules/**", "dist/**", ".git"] },
		"formatter": {
			"prettier": { "disabled": true },
			"custom": { "command": ["custom-fmt", "$FILE"], "extensions": [".foo"] }
		},
		"lsp": { "typescript": { "disabled": true }, "custom": { "command": ["custom-lsp"], "extensions": [".foo"] } },
		"attachments": {
			"image": { "auto_resize": false, "max_width": 1200, "max_height": 900, "max_base64_bytes": 1048576 }
		},
		"tool_output": { "max_lines": 1000, "max_bytes": 32768 },
		"mcp": {
			"timeout": { "startup": 5000, "request": 60000 },
			"servers": {
				"local": {
					"type": "local",
					"command": ["node", "./mcp/server.js"],
					"environment": { "API_KEY": "secret" },
					"disabled": false,
					"timeout": { "request": 10000 }
				},
				"remote": {
					"type": "remote",
					"url": "https://mcp.example.com/mcp",
					"headers": { "Authorization": "Bearer token" },
					"oauth": { "client_id": "client", "scope": "read write", "callback_port": 19876 },
					"disabled": true,
					"timeout": { "startup": 15000 }
				}
			}
		},
		"compaction": { "auto": true, "prune": false, "keep": { "tokens": 2000 }, "buffer": 10000 },
		"skills": ["./skills", "~/shared-skills", "https://example.com/.well-known/skills/"],
		"instructions": ["CONTRIBUTING.md", ".cursor/rules/*.md", "https://example.com/shared-rules.md"],
		"references": {
			"local": { "path": "../library" },
			"sdk": { "repository": "github.com/example/sdk", "branch": "main" },
			"shorthand": "github.com/example/docs"
		},
		"plugins": [
			"opencode-helicone-session",
			{ "package": "@my-org/audit-plugin", "options": { "endpoint": "https://audit.example.com" } }
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(documents))
	}
	info := documents[0].Info
	if *info.Shell != "/bin/bash" || *info.Model != "anthropic/claude" || *info.DefaultAgent != "reviewer" {
		t.Fatalf("unexpected scalars: %#v", info)
	}
	if info.Autoupdate == nil || !info.Autoupdate.Notify {
		t.Fatalf("unexpected autoupdate: %#v", info.Autoupdate)
	}
	if *info.Share != "disabled" || info.Enterprise == nil || *info.Enterprise.URL != "https://share.example.com" {
		t.Fatalf("unexpected share/enterprise: %#v", info)
	}
	if *info.Username != "test-user" {
		t.Fatalf("unexpected username: %v", *info.Username)
	}
	if len(info.Permissions) != 2 || info.Permissions[1].Action != "bash" || info.Permissions[1].Resource != "git status" || info.Permissions[1].Effect != "allow" {
		t.Fatalf("unexpected permissions: %#v", info.Permissions)
	}
	reviewer := info.Agents["reviewer"]
	if *reviewer.Model != "openrouter/openai/gpt-5" || *reviewer.Variant != "high" ||
		*reviewer.Description != "Review changes for correctness" || *reviewer.System != "Find regressions." ||
		*reviewer.Mode != "subagent" || *reviewer.Hidden != false || *reviewer.Color != "warning" ||
		int(*reviewer.Steps) != 12 || *reviewer.Disabled != false {
		t.Fatalf("unexpected reviewer: %#v", reviewer)
	}
	if reviewer.Request.Headers["x-agent"] != "reviewer" || reviewer.Request.Body["reasoningEffort"] != "high" {
		t.Fatalf("unexpected reviewer request: %#v", reviewer.Request)
	}
	if len(*reviewer.Permissions) != 1 || (*reviewer.Permissions)[0].Action != "edit" {
		t.Fatalf("unexpected reviewer permissions: %#v", reviewer.Permissions)
	}
	if *info.Snapshots != false {
		t.Fatalf("unexpected snapshots: %v", *info.Snapshots)
	}
	if info.Watcher == nil || len(info.Watcher.Ignore) != 3 {
		t.Fatalf("unexpected watcher: %#v", info.Watcher)
	}
	if info.Formatter.Entries["prettier"].Disabled == nil || *info.Formatter.Entries["prettier"].Disabled != true {
		t.Fatalf("unexpected formatter prettier: %#v", info.Formatter.Entries["prettier"])
	}
	customFormatter := info.Formatter.Entries["custom"]
	if len(customFormatter.Command) != 2 || customFormatter.Command[0] != "custom-fmt" || len(customFormatter.Extensions) != 1 {
		t.Fatalf("unexpected formatter custom: %#v", customFormatter)
	}
	if info.LSP.Servers["typescript"].Disabled == nil || *info.LSP.Servers["typescript"].Disabled != true {
		t.Fatalf("unexpected lsp typescript: %#v", info.LSP.Servers["typescript"])
	}
	if len(info.LSP.Servers["custom"].Command) != 1 || info.LSP.Servers["custom"].Command[0] != "custom-lsp" {
		t.Fatalf("unexpected lsp custom: %#v", info.LSP.Servers["custom"])
	}
	if info.Attachments == nil || info.Attachments.Image == nil || *info.Attachments.Image.MaxWidth != 1200 {
		t.Fatalf("unexpected attachments: %#v", info.Attachments)
	}
	if info.ToolOutput == nil || *info.ToolOutput.MaxLines != 1000 || *info.ToolOutput.MaxBytes != 32768 {
		t.Fatalf("unexpected tool_output: %#v", info.ToolOutput)
	}
	if info.MCP == nil || *info.MCP.Timeout.Startup != 5000 || *info.MCP.Timeout.Request != 60000 {
		t.Fatalf("unexpected mcp timeout: %#v", info.MCP)
	}
	local := info.MCP.Servers["local"]
	if local.Type != "local" || len(local.Command) != 2 || *local.Disabled != false || *local.Timeout.Request != 10000 {
		t.Fatalf("unexpected mcp local: %#v", local)
	}
	remote := info.MCP.Servers["remote"]
	if remote.Type != "remote" || *remote.URL != "https://mcp.example.com/mcp" || *remote.Disabled != true || *remote.Timeout.Startup != 15000 {
		t.Fatalf("unexpected mcp remote: %#v", remote)
	}
	if info.Compaction == nil || *info.Compaction.Auto != true || *info.Compaction.Prune != false ||
		*info.Compaction.Keep.Tokens != 2000 || *info.Compaction.Buffer != 10000 {
		t.Fatalf("unexpected compaction: %#v", info.Compaction)
	}
	if len(info.Skills) != 3 || info.Skills[0] != "./skills" {
		t.Fatalf("unexpected skills: %#v", info.Skills)
	}
	if len(info.Instructions) != 3 {
		t.Fatalf("unexpected instructions: %#v", info.Instructions)
	}
	if info.References["local"].Local == nil || info.References["local"].Local.Path != "../library" {
		t.Fatalf("unexpected reference local: %#v", info.References["local"])
	}
	if info.References["sdk"].Git == nil || info.References["sdk"].Git.Repository != "github.com/example/sdk" {
		t.Fatalf("unexpected reference sdk: %#v", info.References["sdk"])
	}
	if info.References["shorthand"].Shorthand == nil || *info.References["shorthand"].Shorthand != "github.com/example/docs" {
		t.Fatalf("unexpected reference shorthand: %#v", info.References["shorthand"])
	}
	if len(info.Plugins) != 2 || info.Plugins[0].Name == nil || *info.Plugins[0].Name != "opencode-helicone-session" {
		t.Fatalf("unexpected plugins: %#v", info.Plugins)
	}
	if info.Plugins[1].Entry == nil || info.Plugins[1].Entry.Package != "@my-org/audit-plugin" {
		t.Fatalf("unexpected plugin entry: %#v", info.Plugins[1])
	}
}

func TestConfigMigratesDeprecatedReference(t *testing.T) {
	tmp := t.TempDir()
	contents := `{"reference":{"local":{"path":"../library"},"sdk":{"repository":"github.com/example/sdk","branch":"main"},"shorthand":"github.com/example/docs"}}`
	if err := os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(documents))
	}
	references := documents[0].Info.References
	if references["local"].Local == nil || references["local"].Local.Path != "../library" {
		t.Fatalf("unexpected local reference: %#v", references["local"])
	}
	if references["sdk"].Git == nil || references["sdk"].Git.Repository != "github.com/example/sdk" {
		t.Fatalf("unexpected sdk reference: %#v", references["sdk"])
	}
	if references["shorthand"].Shorthand == nil || *references["shorthand"].Shorthand != "github.com/example/docs" {
		t.Fatalf("unexpected shorthand reference: %#v", references["shorthand"])
	}
}

func TestConfigMigratesV1(t *testing.T) {
	tmp := t.TempDir()
	contents := `{
		"shell": "/bin/zsh",
		"default_agent": "reviewer",
		"snapshot": false,
		"autoshare": true,
		"permission": {
			"bash": "ask",
			"edit": { "*.md": "allow", "*": "deny" },
			"question": "deny"
		},
		"agent": {
			"reviewer": {
				"prompt": "Review changes.",
				"disable": true,
				"temperature": 0.2,
				"permission": { "read": "allow" }
			}
		},
		"plugin": [
			"opencode-helicone-session",
			["@my-org/audit-plugin", { "endpoint": "https://audit.example.com" }]
		],
		"skills": { "paths": ["./skills"], "urls": ["https://example.com/.well-known/skills/"] },
		"references": {
			"docs": { "path": "../docs", "description": "Use for product documentation", "hidden": true }
		},
		"attachment": { "image": { "auto_resize": false, "max_width": 1200 } },
		"provider": {
			"custom": {
				"options": { "apiKey": "secret" },
				"models": {
					"model": {
						"options": { "reasoningEffort": "high" },
						"variants": { "fast": { "temperature": 0.2 } }
					}
				}
			},
			"openai": {
				"npm": "@ai-sdk/openai",
				"options": { "apiKey": "secret", "organization": "org" },
				"models": {
					"model": {
						"options": { "temperature": 0.3, "reasoningEffort": "high", "serviceTier": "priority" },
						"variants": { "high": { "reasoningEffort": "high", "reasoningSummary": "auto" } }
					}
				}
			},
			"anthropic": {
				"npm": "@ai-sdk/anthropic",
				"models": {
					"model": {
						"options": {
							"effort": "high",
							"taskBudget": 4096,
							"metadata": { "userId": "user-1" }
						}
					}
				}
			}
		},
		"compaction": { "auto": true, "tail_turns": 3, "preserve_recent_tokens": 2000, "reserved": 10000 },
		"experimental": { "mcp_timeout": 5000 },
		"mcp": {
			"local": { "type": "local", "command": ["node", "server.js"], "enabled": false, "timeout": 10000 },
			"remote": {
				"type": "remote",
				"url": "https://mcp.example.com",
				"oauth": { "clientId": "client", "callbackPort": 19876 },
				"timeout": 20000
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(documents))
	}
	info := documents[0].Info
	if *info.Shell != "/bin/zsh" || *info.DefaultAgent != "reviewer" {
		t.Fatalf("unexpected scalars: %#v", info)
	}
	if *info.Snapshots != false || *info.Share != "auto" {
		t.Fatalf("unexpected snapshot/share: %#v", info)
	}
	wantRules := []schema.PermissionRule{
		{Action: "bash", Resource: "*", Effect: "ask"},
		{Action: "edit", Resource: "*.md", Effect: "allow"},
		{Action: "edit", Resource: "*", Effect: "deny"},
		{Action: "question", Resource: "*", Effect: "deny"},
	}
	if len(info.Permissions) != len(wantRules) {
		t.Fatalf("unexpected permissions: %#v", info.Permissions)
	}
	for i, rule := range wantRules {
		if info.Permissions[i] != rule {
			t.Fatalf("permission %d = %#v, want %#v", i, info.Permissions[i], rule)
		}
	}
	reviewer := info.Agents["reviewer"]
	if *reviewer.System != "Review changes." || *reviewer.Disabled != true {
		t.Fatalf("unexpected reviewer: %#v", reviewer)
	}
	if reviewer.Request == nil || reviewer.Request.Body["temperature"] != float64(0.2) {
		t.Fatalf("unexpected reviewer request: %#v", reviewer.Request)
	}
	if len(*reviewer.Permissions) != 1 || (*reviewer.Permissions)[0].Action != "read" {
		t.Fatalf("unexpected reviewer permissions: %#v", reviewer.Permissions)
	}
	if len(info.Plugins) != 2 || info.Plugins[0].Name == nil || info.Plugins[1].Entry == nil {
		t.Fatalf("unexpected plugins: %#v", info.Plugins)
	}
	if len(info.Skills) != 2 || info.Skills[0] != "./skills" {
		t.Fatalf("unexpected skills: %#v", info.Skills)
	}
	if info.References["docs"].Local == nil || *info.References["docs"].Local.Hidden != true {
		t.Fatalf("unexpected references: %#v", info.References["docs"])
	}
	if info.Attachments == nil || info.Attachments.Image == nil || *info.Attachments.Image.MaxWidth != 1200 {
		t.Fatalf("unexpected attachments: %#v", info.Attachments)
	}
	custom := info.Providers["custom"]
	if custom.Request == nil || custom.Request.Body["apiKey"] != "secret" {
		t.Fatalf("unexpected custom provider: %#v", custom)
	}
	if custom.Models["model"].Request == nil || custom.Models["model"].Request.Body["reasoningEffort"] != "high" {
		t.Fatalf("unexpected custom model: %#v", custom.Models["model"])
	}
	if len(custom.Models["model"].Variants) != 1 || custom.Models["model"].Variants[0].Body["temperature"] != float64(0.2) {
		t.Fatalf("unexpected custom variants: %#v", custom.Models["model"].Variants)
	}
	openai := info.Providers["openai"]
	if openai.API == nil || openai.API.Settings == nil {
		t.Fatalf("unexpected openai api: %#v", openai.API)
	}
	if openai.Request.Headers["Authorization"] != "Bearer secret" || openai.Request.Headers["OpenAI-Organization"] != "org" {
		t.Fatalf("unexpected openai request: %#v", openai.Request)
	}
	openaiModel := openai.Models["model"]
	if openaiModel.Request.Body["temperature"] != float64(0.3) || openaiModel.Request.Body["service_tier"] != "priority" {
		t.Fatalf("unexpected openai model body: %#v", openaiModel.Request.Body)
	}
	reasoning, _ := openaiModel.Request.Body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("unexpected openai reasoning: %#v", openaiModel.Request.Body)
	}
	if len(openaiModel.Variants) != 1 {
		t.Fatalf("unexpected openai variants: %#v", openaiModel.Variants)
	}
	variantBody, _ := openaiModel.Variants[0].Body["reasoning"].(map[string]any)
	if variantBody["effort"] != "high" || variantBody["summary"] != "auto" {
		t.Fatalf("unexpected openai variant body: %#v", openaiModel.Variants[0].Body)
	}
	anthropicModel := info.Providers["anthropic"].Models["model"]
	outputConfig, _ := anthropicModel.Request.Body["output_config"].(map[string]any)
	if outputConfig["effort"] != "high" || outputConfig["task_budget"] != float64(4096) {
		t.Fatalf("unexpected anthropic output_config: %#v", anthropicModel.Request.Body)
	}
	metadata, _ := anthropicModel.Request.Body["metadata"].(map[string]any)
	if metadata["user_id"] != "user-1" {
		t.Fatalf("unexpected anthropic metadata: %#v", anthropicModel.Request.Body)
	}
	if info.Compaction == nil || info.Compaction.Prune != nil || *info.Compaction.Keep.Tokens != 2000 || *info.Compaction.Buffer != 10000 {
		t.Fatalf("unexpected compaction: %#v", info.Compaction)
	}
	if info.MCP == nil || *info.MCP.Timeout.Request != 5000 {
		t.Fatalf("unexpected mcp: %#v", info.MCP)
	}
	if *info.MCP.Servers["local"].Disabled != true || *info.MCP.Servers["local"].Timeout.Request != 10000 {
		t.Fatalf("unexpected mcp local: %#v", info.MCP.Servers["local"])
	}
	if info.MCP.Servers["remote"].OAuth == nil {
		t.Fatalf("unexpected mcp remote oauth: %#v", info.MCP.Servers["remote"])
	}
	var oauth struct {
		CallbackPort *int `json:"callback_port"`
	}
	if err := json.Unmarshal(info.MCP.Servers["remote"].OAuth, &oauth); err != nil {
		t.Fatal(err)
	}
	if oauth.CallbackPort == nil || *oauth.CallbackPort != 19876 {
		t.Fatalf("unexpected mcp remote oauth: %#v", info.MCP.Servers["remote"])
	}
}

func TestConfigIgnoresInvalidFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte(`{"$schema":"base"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "opencode.jsonc"), []byte(`{ invalid`), 0644); err != nil {
		t.Fatal(err)
	}
	config, _ := newTestConfig(t, tmp, filepath.Join(tmp, "global"), tmp)
	documents := configDocuments(config.Entries())
	if len(documents) != 1 || *documents[0].Info.Schema != "base" {
		t.Fatalf("unexpected documents: %#v", documents)
	}
}

func TestConfigLoadsPolicyReverseOrder(t *testing.T) {
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	if err := os.MkdirAll(global, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(global, "opencode.json"),
		[]byte(`{"experimental":{"policies":[{"effect":"deny","action":"provider.use","resource":"openai"}]}}`),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(tmp, "opencode.json"),
		[]byte(`{"experimental":{"policies":[{"effect":"allow","action":"provider.use","resource":"openai"}]}}`),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	_, policy := newTestConfig(t, tmp, global, tmp)
	if got := policy.Evaluate("provider.use", "openai", PolicyEffectAllow); got != PolicyEffectDeny {
		t.Fatalf("expected deny, got %v", got)
	}
}

func TestConfigDiscovery(t *testing.T) {
	tmp := t.TempDir()
	global := filepath.Join(tmp, "global")
	root := filepath.Join(tmp, "repo")
	parent := filepath.Join(root, "packages")
	directory := filepath.Join(parent, "app")
	for _, dir := range []string{global, directory, filepath.Join(root, ".opencode"), filepath.Join(directory, ".opencode")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(tmp, "opencode.json"):                     `{"$schema":"outside"}`,
		filepath.Join(global, "opencode.json"):                  `{"$schema":"global"}`,
		filepath.Join(root, "opencode.json"):                    `{"$schema":"root"}`,
		filepath.Join(parent, "opencode.jsonc"):                 `{"$schema":"parent"}`,
		filepath.Join(directory, "opencode.json"):               `{"$schema":"directory"}`,
		filepath.Join(root, ".opencode", "opencode.json"):       `{"$schema":"root-dot"}`,
		filepath.Join(directory, ".opencode", "opencode.jsonc"): `{"$schema":"directory-dot"}`,
	}
	for file, content := range files {
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	config, _ := newTestConfig(t, directory, global, root)
	entries := config.Entries()
	var directories []string
	for _, entry := range entries {
		if dir, ok := entry.(ConfigDirectory); ok {
			directories = append(directories, string(dir.Path))
		}
	}
	wantDirectories := []string{global, filepath.Join(root, ".opencode"), filepath.Join(directory, ".opencode")}
	if len(directories) != len(wantDirectories) {
		t.Fatalf("unexpected directories: %#v", directories)
	}
	for i := range wantDirectories {
		if directories[i] != wantDirectories[i] {
			t.Fatalf("directory %d = %q, want %q", i, directories[i], wantDirectories[i])
		}
	}

	var order []string
	for _, entry := range entries {
		switch typed := entry.(type) {
		case ConfigDocument:
			order = append(order, *typed.Info.Schema)
		case ConfigDirectory:
			order = append(order, string(typed.Path))
		}
	}
	want := []string{
		"global", global,
		"root", "parent", "directory", "root-dot",
		filepath.Join(root, ".opencode"),
		"directory-dot",
		filepath.Join(directory, ".opencode"),
	}
	if len(order) != len(want) {
		t.Fatalf("unexpected order: %#v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (full: %#v)", i, order[i], want[i], order)
		}
	}
}
