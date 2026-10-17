package config

import (
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestApplyDirAgents(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "review.md"), "---\ndescription: reviewer\nmode: subagent\n---\nYou review.\n")
	writeFile(t, filepath.Join(dir, "mode", "plan.md"), "---\ndescription: planner\n---\nPlan.\n")

	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
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

// TestApplyDirAgentsMergesBase guards that a configured agent with no rules of
// its own inherits the base ruleset instead of asking for everything.
func TestApplyDirAgentsMergesBase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mode", "planner.md"), "---\ndescription: planner\n---\nPlan.\n")

	base := core.PermissionRuleset{{Action: "*", Resource: "*", Effect: schema.PermissionEffectAllow}}
	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, base); err != nil {
		t.Fatalf("apply: %v", err)
	}

	planner := agents.Get(schema.AgentID("planner"))
	if planner == nil {
		t.Fatal("planner agent missing")
	}
	if got := core.PermissionEvaluate("count_to", "*", planner.Permissions).Effect; got != schema.PermissionEffectAllow {
		t.Fatalf("count_to permission = %v, want allow from the base", got)
	}
}

func TestApplyDirAgentsV2Markdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "build.md"),
		"---\ndescription: v2 agent\nmode: primary\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: deny\n---\nV2 body.\n")

	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	build := agents.Get(schema.AgentID("build"))
	if build == nil {
		t.Fatal("build agent missing")
	}
	if build.System == nil || *build.System != "V2 body." {
		t.Fatalf("system = %v", build.System)
	}
	if got := core.PermissionEvaluate("edit", "*", build.Permissions).Effect; got != schema.PermissionEffectDeny {
		t.Fatalf("edit permission = %v", got)
	}
}

// TestApplyDirAgentsA2ABackend guards that a markdown agent file can declare a
// remote A2A backend, so a peer reached over the wire needs no RegisterAgent.
func TestApplyDirAgentsA2ABackend(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "echo.md"),
		"---\ndescription: remote echo\nmode: subagent\nbackend:\n  type: a2a\n  a2a:\n    endpoint: http://127.0.0.1:9099\n---\n")

	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	echo := agents.Get(schema.AgentID("echo"))
	if echo == nil || echo.Backend == nil {
		t.Fatalf("echo = %+v", echo)
	}
	if echo.Backend.Type != schema.AgentBackendA2A {
		t.Fatalf("backend type = %q", echo.Backend.Type)
	}
	if echo.Backend.A2A == nil || echo.Backend.A2A.Endpoint != "http://127.0.0.1:9099" {
		t.Fatalf("backend a2a = %+v", echo.Backend.A2A)
	}
}

func TestApplyDirAgentsExpandsHome(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "a.md"),
		"---\ndescription: a\npermissions:\n  - action: read\n    resource: \"~/secret\"\n    effect: ask\n---\nA.\n")

	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	a := agents.Get(schema.AgentID("a"))
	if a == nil {
		t.Fatal("a agent missing")
	}
	if got := core.PermissionEvaluate("read", "/home/u/secret", a.Permissions).Effect; got != schema.PermissionEffectAsk {
		t.Fatalf("read permission = %v", got)
	}
}

func TestApplyDirAgentsDisabled(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "temp.md"), "---\ndescription: temporary\n---\nTemp.\n")
	agents := core.NewAgent()
	if err := ApplyDirAgents(agents, []string{dir}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if agents.Get(schema.AgentID("temp")) == nil {
		t.Fatal("temp agent missing")
	}

	remove := t.TempDir()
	writeFile(t, filepath.Join(remove, "agent", "temp.md"), "---\ndescription: temporary\ndisabled: true\n---\nTemp.\n")
	if err := ApplyDirAgents(agents, []string{remove}, core.Global{Home: "/home/u"}, core.PermissionRuleset{}); err != nil {
		t.Fatalf("apply remove: %v", err)
	}
	if agents.Get(schema.AgentID("temp")) != nil {
		t.Fatal("temp agent should be removed")
	}
}
