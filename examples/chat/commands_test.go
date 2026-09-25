package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/examples/internal/util"
	"github.com/user/go-agent-sdk/schema"
)

func runCommand(t *testing.T, configDirs []string, fn func(*chat)) string {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model:      "test-model",
		ConfigDirs: configDirs,
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var out strings.Builder
	console := &console{out: &out, lineStart: true}
	fn(newChat(console, client, "ses_1", "test-model"))
	return out.String()
}

func TestSkillsCommandEmpty(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) { c.skillsCommand() })
	if !strings.Contains(out, "no skills") {
		t.Fatalf("out = %q, want the empty-skills hint", out)
	}
}

func TestSkillsCommandListsConfiguredSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skill", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo\ndescription: A demo skill.\n---\nDo the demo.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCommand(t, []string{dir}, func(c *chat) { c.skillsCommand() })
	if !strings.Contains(out, "demo") || !strings.Contains(out, "A demo skill.") {
		t.Fatalf("out = %q, want the skill from ConfigDirs", out)
	}
}

func TestAgentsCommandListsSubagents(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) { c.agentsCommand() })
	if !strings.Contains(out, "Subagents") || !strings.Contains(out, "explore") {
		t.Fatalf("out = %q, want a subagent list containing explore", out)
	}
}

func TestMCPsCommandWithoutServers(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) { c.mcpsCommand() })
	if !strings.Contains(out, "no MCP servers") {
		t.Fatalf("out = %q, want the no-MCP hint", out)
	}
}

// TestSkillsCommandWithPaddedInput guards that the prompt's whitespace does not
// stop a command from being recognised.
func TestSkillsCommandWithPaddedInput(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "  /skills  ") {
			t.Error("a padded command was not recognised")
		}
	})
	if !strings.Contains(out, "no skills") {
		t.Fatalf("out = %q, want the empty-skills hint", out)
	}
}

// TestTaskCommands covers the task lifecycle lines and /task: the list, the
// drill-down by handle, and an unknown argument.
func TestTaskCommands(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.handle(agent.ToolProgress{
			SessionID: "ses_1", CallID: "call_1",
			Structured: map[string]any{
				"sessionId":    "ses_child",
				"subagentType": "explore",
				"description":  "explore core",
			},
		})
		c.handle(agent.ToolSuccess{SessionID: "ses_1", CallID: "call_1"})

		c.console.write("--list--\n")
		c.taskCommand("/task")
		c.console.write("--detail--\n")
		c.taskCommand("/task 1")
		c.console.write("--missing--\n")
		c.taskCommand("/task nope")
	})
	for _, want := range []string{
		"┌ #1 Explore Task — explore core", // start line
		"└ #1 ✓",                           // finish line
		"(/task #1 for the full text)",
		"#1 · done · Explore Task — explore core", // list
		"task not found: nope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestDemoConfigDiscovered loads the demo config from the package's .config
// directory (go test runs with the package directory as the working directory).
func TestDemoConfigDiscovered(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model:      "test-model",
		ConfigDirs: []string{".config"},
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	skills, err := client.Skills().List()
	if err != nil {
		t.Fatalf("skills: %v", err)
	}
	names := map[string]bool{}
	for _, skill := range skills {
		names[skill.Name] = true
	}
	for _, want := range []string{"code-review", "release-notes"} {
		if !names[want] {
			t.Fatalf("skill %q not discovered (got %v)", want, names)
		}
	}

	reviewer := client.Agents().Get(schema.AgentID("reviewer"))
	if reviewer == nil {
		t.Fatal("reviewer subagent not discovered")
	}
	if got := core.PermissionEvaluate("edit", "main.go", reviewer.Permissions).Effect; got != schema.PermissionEffectDeny {
		t.Fatalf("reviewer edit = %q, want deny", got)
	}
	if got := core.PermissionEvaluate("bash", "ls", reviewer.Permissions).Effect; got != schema.PermissionEffectAllow {
		t.Fatalf("reviewer bash = %q, want allow", got)
	}

	docs := client.Agents().Get(schema.AgentID("docs-writer"))
	if docs == nil {
		t.Fatal("docs-writer subagent not discovered")
	}
	if got := core.PermissionEvaluate("edit", "README.md", docs.Permissions).Effect; got != schema.PermissionEffectAllow {
		t.Fatalf("docs-writer edit README.md = %q, want allow", got)
	}
	if got := core.PermissionEvaluate("edit", "main.go", docs.Permissions).Effect; got != schema.PermissionEffectDeny {
		t.Fatalf("docs-writer edit main.go = %q, want deny", got)
	}
}

// TestExampleDirPointsAtTheExample: ExampleDir resolves to the example's own
// directory, which is where Client reads .config from and writes .data to.
func TestExampleDirPointsAtTheExample(t *testing.T) {
	dir := util.ExampleDir()
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Fatalf("example dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "skill", "code-review", "SKILL.md")); err != nil {
		t.Fatalf("sample skill next to the example: %v", err)
	}
}
