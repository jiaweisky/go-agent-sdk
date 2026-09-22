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

func TestLoadAgents(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent", "review.md"), "---\ndescription: reviewer\nmode: subagent\n---\nYou review code.\n")
	writeFile(t, filepath.Join(dir, "agents", "nested", "build.md"), "---\ndescription: builder\n---\nYou build.\n")

	agents, err := LoadAgents(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %+v", agents)
	}
	review := agents["review"]
	if review.Description == nil || *review.Description != "reviewer" {
		t.Fatalf("review = %+v", review)
	}
	if review.Mode == nil || *review.Mode != "subagent" {
		t.Fatalf("review mode = %v", review.Mode)
	}
	if review.Prompt == nil || *review.Prompt != "You review code." {
		t.Fatalf("review prompt = %v", review.Prompt)
	}
	if _, ok := agents["nested/build"]; !ok {
		t.Fatalf("missing nested agent: %+v", agents)
	}
}

func TestLoadModes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mode", "plan.md"), "---\ndescription: planning\n---\nPlan.\n")
	modes, err := LoadModes(dir)
	if err != nil {
		t.Fatalf("load modes: %v", err)
	}
	plan, ok := modes["plan"]
	if !ok {
		t.Fatalf("modes = %+v", modes)
	}
	if plan.Mode == nil || *plan.Mode != "primary" {
		t.Fatalf("mode = %v", plan.Mode)
	}
}
