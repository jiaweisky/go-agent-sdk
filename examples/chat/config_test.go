package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/core"
	defsconfig "github.com/jiaweisky/go-agent-sdk/defs/config"
	"github.com/jiaweisky/go-agent-sdk/schema"
	"golang.org/x/text/language"
)

// writeAgentFile writes a markdown agent file under dir/sub/name.md.
func writeAgentFile(t *testing.T, dir, sub, name, frontmatter, body string) {
	t.Helper()
	path := filepath.Join(dir, sub)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(path, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestModeFileRegistersPrimaryAgent guards that a {mode,modes}/*.md file becomes
// a selectable primary agent (in /agent), not a subagent.
func TestModeFileRegistersPrimaryAgent(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "mode", "planner", "description: Plan work.\n", "You plan.")
	out := runCommand(t, []string{dir}, func(c *chat) {
		c.agentCommand("/agent")
	})
	if !strings.Contains(out, "planner") {
		t.Fatalf("out = %q, want planner listed as a primary agent", out)
	}
}

// TestModeAgentInheritsDefaultPermissions guards that a configured primary agent
// gets the default ruleset, so its tools don't ask for everything (TS merges the
// defaults into every agent).
func TestModeAgentInheritsDefaultPermissions(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "mode", "planner", "description: Plan work.\n", "You plan.")
	runCommand(t, []string{dir}, func(c *chat) {
		planner := c.client.Agents().Get(schema.AgentID("planner"))
		if planner == nil {
			t.Fatal("planner agent missing")
		}
		if got := core.PermissionEvaluate("count_to", "*", planner.Permissions).Effect; got != schema.PermissionEffectAllow {
			t.Fatalf("count_to permission = %v, want allow from the default ruleset", got)
		}
	})
}

// TestSubagentLocaleOrdering guards the Locale option's effect: locale-aware
// collation reorders mixed-script subagent names.
func TestSubagentLocaleOrdering(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"翻译", "摘要"} {
		writeAgentFile(t, dir, "agent", name, "description: helper\nmode: subagent\n", "help")
	}
	agents := core.NewAgent()
	if err := defsconfig.ApplyDirAgents(agents, []string{dir}, core.Global{Home: t.TempDir()}, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	allowAll := schema.PermissionRuleset{{Action: "*", Resource: "*", Effect: schema.PermissionEffectAllow}}
	zh := agent.SubagentTypes(agents, allowAll, language.MustParse("zh-Hans"))
	en := agent.SubagentTypes(agents, allowAll, language.MustParse("en-US"))
	if len(zh) != 2 || len(en) != 2 {
		t.Fatalf("zh=%v en=%v, want both subagents", ids(zh), ids(en))
	}
	if zh[0].ID == en[0].ID {
		t.Fatalf("locale did not reorder: zh=%v en=%v", ids(zh), ids(en))
	}
}

func ids(items []schema.AgentInfo) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item.ID)
	}
	return out
}
