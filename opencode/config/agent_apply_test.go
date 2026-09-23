package config

import (
	"path/filepath"
	"testing"

	"github.com/user/go-agent-sdk/core"
	coreconfig "github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/schema"
)

func TestApplyConfigAgentsLegacyDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "review.md"), "---\ndescription: reviewer\nmode: subagent\n---\nYou review.\n")
	writeFile(t, filepath.Join(dir, "mode", "plan.md"), "---\ndescription: planner\n---\nPlan.\n")

	agents := core.NewAgent()
	global := core.Global{Home: "/home/u"}
	err := ApplyConfigAgents(agents, []core.ConfigEntry{
		core.ConfigDirectory{Path: schema.AbsolutePath(dir)},
	}, global)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	review := agents.Get(schema.AgentID("review"))
	if review == nil || review.Description == nil || *review.Description != "reviewer" {
		t.Fatalf("review = %+v", review)
	}
	if review.System == nil || *review.System != "You review." {
		t.Fatalf("review system = %v", review.System)
	}
	if review.Mode != schema.AgentModeSubagent {
		t.Fatalf("review mode = %v", review.Mode)
	}

	plan := agents.Get(schema.AgentID("plan"))
	if plan == nil || plan.Mode != schema.AgentModePrimary {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestApplyConfigAgentsDocument(t *testing.T) {
	model := "openai/gpt-4o"
	description := "custom agent"
	mode := "primary"
	steps := schema.PositiveInt(3)
	agentRules := schema.PermissionRuleset{{Action: "edit", Resource: "~/project", Effect: schema.PermissionEffectAllow}}

	document := core.ConfigDocument{Info: core.ConfigInfo{
		DefaultAgent: stringPtr("custom"),
		Permissions:  schema.PermissionRuleset{{Action: "read", Resource: "~/secret", Effect: schema.PermissionEffectAsk}},
		Agents: map[string]coreconfig.AgentInfo{
			"custom": {
				Model: &model, Description: &description, Mode: &mode,
				Steps: &steps, Permissions: &agentRules,
			},
		},
	}}

	agents := core.NewAgent()
	global := core.Global{Home: "/home/u"}
	if err := ApplyConfigAgents(agents, []core.ConfigEntry{document}, global); err != nil {
		t.Fatalf("apply: %v", err)
	}

	custom := agents.Get(schema.AgentID("custom"))
	if custom == nil {
		t.Fatal("custom agent missing")
	}
	if custom.Model == nil || custom.Model.ProviderID != "openai" || custom.Model.ID != "gpt-4o" {
		t.Fatalf("model = %+v", custom.Model)
	}
	if custom.Mode != schema.AgentModePrimary || custom.Hidden || custom.Steps == nil || *custom.Steps != 3 {
		t.Fatalf("custom = %+v", custom)
	}
	if got := PermissionEvaluateForTest("read", "/home/u/secret", custom.Permissions); got != schema.PermissionEffectAsk {
		t.Fatalf("read permission = %v", got)
	}
	if got := PermissionEvaluateForTest("edit", "/home/u/project", custom.Permissions); got != schema.PermissionEffectAllow {
		t.Fatalf("edit permission = %v", got)
	}
	if agents.Default() == nil || agents.Default().ID != schema.AgentID("custom") {
		t.Fatalf("default = %+v", agents.Default())
	}
}

func TestApplyConfigAgentsV2Markdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "build.md"),
		"---\ndescription: v2 agent\nmode: primary\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: deny\n---\nV2 body.\n")

	agents := core.NewAgent()
	if err := ApplyConfigAgents(agents, []core.ConfigEntry{
		core.ConfigDirectory{Path: schema.AbsolutePath(dir)},
	}, core.Global{Home: "/home/u"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	build := agents.Get(schema.AgentID("build"))
	if build == nil {
		t.Fatal("build agent missing")
	}
	if build.System == nil || *build.System != "V2 body." {
		t.Fatalf("system = %v", build.System)
	}
	if got := PermissionEvaluateForTest("edit", "*", build.Permissions); got != schema.PermissionEffectDeny {
		t.Fatalf("edit permission = %v", got)
	}
}

func TestApplyConfigAgentsDisabled(t *testing.T) {
	description := "temporary"
	document := core.ConfigDocument{Info: core.ConfigInfo{
		Agents: map[string]coreconfig.AgentInfo{"temp": {Description: &description}},
	}}
	agents := core.NewAgent()
	if err := ApplyConfigAgents(agents, []core.ConfigEntry{document}, core.Global{Home: "/home/u"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if agents.Get(schema.AgentID("temp")) == nil {
		t.Fatal("temp agent missing")
	}

	disabled := true
	remove := core.ConfigDocument{Info: core.ConfigInfo{
		Agents: map[string]coreconfig.AgentInfo{"temp": {Disabled: &disabled}},
	}}
	if err := ApplyConfigAgents(agents, []core.ConfigEntry{remove}, core.Global{Home: "/home/u"}); err != nil {
		t.Fatalf("apply remove: %v", err)
	}
	if agents.Get(schema.AgentID("temp")) != nil {
		t.Fatal("temp agent should be removed")
	}
}

func stringPtr(value string) *string { return &value }

// PermissionEvaluateForTest avoids importing core in assertions.
func PermissionEvaluateForTest(action, resource string, rules schema.PermissionRuleset) schema.PermissionEffect {
	return core.PermissionEvaluate(action, resource, rules).Effect
}
