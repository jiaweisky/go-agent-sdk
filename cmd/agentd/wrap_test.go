package main

import (
	"slices"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestWrapRoots(t *testing.T) {
	cfg := config.Config{Servers: []config.Server{
		{
			Root:             "/state/a",
			WorkingDirectory: "/work/a",
			ConfigDirs:       []string{"/cfg"},
			Isolation:        &config.IsolationConfig{BaseDir: "/views"},
		},
		{Root: "/state/a"}, // duplicate state root across servers
	}}
	roots := wrapRoots(cfg)
	for _, want := range []string{"/state/a", "/work/a", "/views"} {
		if !slices.Contains(roots, want) {
			t.Fatalf("roots %v missing %q", roots, want)
		}
	}
	// Config dirs are read-only for the agent, so they are not write roots.
	if slices.Contains(roots, "/cfg") {
		t.Fatalf("config dirs must not be write roots: %v", roots)
	}
	count := 0
	for _, root := range roots {
		if root == "/state/a" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("/state/a appears %d times, want 1", count)
	}
}

func TestWrapProcessNoop(t *testing.T) {
	if err := wrapProcess(config.Config{}); err != nil {
		t.Fatalf("no wrap config must be a no-op: %v", err)
	}
	if err := wrapProcess(config.Config{Wrap: &config.WrapConfig{Process: "none"}}); err != nil {
		t.Fatalf("wrap.process=none must be a no-op: %v", err)
	}
	t.Setenv(wrappedEnv, "seatbelt")
	if err := wrapProcess(config.Config{Wrap: &config.WrapConfig{Process: "seatbelt"}}); err != nil {
		t.Fatalf("an already-wrapped process must not re-exec: %v", err)
	}
}

func TestWrapProcessFailurePolicy(t *testing.T) {
	// An unknown wrapper name never resolves, so fail-closed refuses to start...
	err := wrapProcess(config.Config{Wrap: &config.WrapConfig{Process: "no-such-wrapper"}})
	if err == nil {
		t.Fatal("expected fail-closed to reject an unknown whole-process wrapper")
	}
	// ...and fail-open warns and runs unwrapped.
	err = wrapProcess(config.Config{
		Wrap: &config.WrapConfig{Process: "no-such-wrapper", FailurePolicy: "fail-open"},
	})
	if err != nil {
		t.Fatalf("fail-open must not fail: %v", err)
	}
}
