package config

import (
	"os"
	"path/filepath"
	"testing"
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
