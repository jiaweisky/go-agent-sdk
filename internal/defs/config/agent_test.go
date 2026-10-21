package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigEntryNameFromPath(t *testing.T) {
	if got := ConfigEntryNameFromPath("agents/nested/build.md", []string{"agent/", "agents/"}); got != "nested/build" {
		t.Fatalf("name = %q", got)
	}
	if got := ConfigEntryNameFromPath("other/x.md", []string{"agent/", "agents/"}); got != "x" {
		t.Fatalf("fallback name = %q", got)
	}
}

func TestApplyAgentInfoTools(t *testing.T) {
	// The allowlist replaces any target list; a later agent file wins as a whole.
	target := &schema.AgentInfo{Tools: []string{"bash"}}
	applyAgentInfo(target, AgentInfo{Tools: []string{"read", "grep"}}, "/home", nil)
	if len(target.Tools) != 2 || target.Tools[0] != "read" || target.Tools[1] != "grep" {
		t.Fatalf("tools = %+v", target.Tools)
	}

	// An explicit empty list means no tools.
	applyAgentInfo(target, AgentInfo{Tools: []string{}}, "/home", nil)
	if target.Tools == nil || len(target.Tools) != 0 {
		t.Fatalf("tools = %+v", target.Tools)
	}

	// A file without tools leaves the target's list untouched.
	applyAgentInfo(target, AgentInfo{}, "/home", nil)
	if target.Tools == nil || len(target.Tools) != 0 {
		t.Fatalf("tools = %+v", target.Tools)
	}
}

func TestDecodeAgentDocumentTools(t *testing.T) {
	info, ok := decodeAgentDocument(AgentEntry{
		Name: "ops",
		Data: map[string]any{
			"description": "ops",
			"tools":       []any{"bash", "read"},
		},
		Content: "You operate.",
	})
	if !ok {
		t.Fatal("decode failed")
	}
	if len(info.Tools) != 2 || info.Tools[0] != "bash" || info.Tools[1] != "read" {
		t.Fatalf("tools = %+v", info.Tools)
	}

	// An explicit empty list decodes as non-nil empty, so `tools: []` means no
	// tools (distinct from the field being absent).
	empty, ok := decodeAgentDocument(AgentEntry{
		Name: "none",
		Data: map[string]any{"tools": []any{}},
	})
	if !ok {
		t.Fatal("decode failed")
	}
	if empty.Tools == nil || len(empty.Tools) != 0 {
		t.Fatalf("tools = %+v", empty.Tools)
	}
}

func TestLoadAgentEntries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "review.md"), "---\ndescription: reviewer\nmode: subagent\n---\nYou review code.\n")
	writeFile(t, filepath.Join(dir, "agents", "nested", "build.md"), "---\ndescription: builder\n---\nYou build.\n")
	writeFile(t, filepath.Join(dir, "mode", "plan.md"), "---\ndescription: planning\n---\nPlan.\n")

	entries, err := LoadAgentEntries(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byName := map[string]AgentEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	review, ok := byName["review"]
	if !ok || review.Primary || review.Content != "You review code." {
		t.Fatalf("review = %+v", review)
	}
	if _, ok := byName["nested/build"]; !ok {
		t.Fatalf("missing nested agent: %+v", byName)
	}
	plan, ok := byName["plan"]
	if !ok || !plan.Primary {
		t.Fatalf("plan = %+v", plan)
	}
}
