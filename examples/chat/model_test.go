package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

func activeSession(t *testing.T, c *chat) {
	t.Helper()
	session, err := c.client.CreateSession(context.Background(), &agent.SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	c.switchSession(session)
}

// onlyDeepseekKeys makes the catalog's availability deterministic: the two
// DeepSeek models are available, the other providers are not.
func onlyDeepseekKeys(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_PROVIDER", "")
	t.Setenv("AGENT_PROVIDER_ID", "")
	t.Setenv("AGENT_API_KEY", "")
	t.Setenv("AGENT_BASE_URL", "")
	t.Setenv("DEEPSEEK_API_KEY", "test")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
}

// TestModelCommandSwitches guards that /model switches the active session's
// model in place.
func TestModelCommandSwitches(t *testing.T) {
	onlyDeepseekKeys(t)
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/model deepseek-v4-pro") {
			t.Error("/model was not recognised")
		}
	})
	if !strings.Contains(out, "model set to deepseek-v4-pro") {
		t.Fatalf("out = %q, want the switch notice", out)
	}
}

// TestModelCommandListAndUnknown guards the list (only available models) and the
// unknown/unavailable error.
func TestModelCommandListAndUnknown(t *testing.T) {
	onlyDeepseekKeys(t)
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		handleCommand(c, "/model")
		c.console.write("--unknown--\n")
		handleCommand(c, "/model nope")
	})
	for _, want := range []string{"Models (2):", "deepseek-v4-flash", "unknown or unavailable model: nope"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestResolveDefaultModel guards the default priority: the persisted choice,
// then AGENT_MODEL, then the catalog default, all limited to available models.
func TestResolveDefaultModel(t *testing.T) {
	onlyDeepseekKeys(t)
	t.Setenv("AGENT_MODEL", "")

	// The catalog default is used when nothing else is set.
	if got := resolveDefaultModel(""); got != "deepseek-v4-flash" {
		t.Fatalf("default = %q, want deepseek-v4-flash", got)
	}
	// AGENT_MODEL is used when there is no persisted selection.
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "test")
	t.Setenv("AGENT_MODEL", "gpt-5")
	if got := resolveDefaultModel(""); got != "gpt-5" {
		t.Fatalf("env = %q, want gpt-5", got)
	}
	// The persisted selection wins over AGENT_MODEL when available.
	t.Setenv("ANTHROPIC_API_KEY", "test")
	if got := resolveDefaultModel("claude-sonnet-4"); got != "claude-sonnet-4" {
		t.Fatalf("persisted = %q, want claude-sonnet-4", got)
	}
	// An unavailable persisted selection falls back to AGENT_MODEL.
	if got := resolveDefaultModel("deepseek-v4-pro"); got != "gpt-5" {
		t.Fatalf("fallback = %q, want gpt-5", got)
	}
}

// TestCatalogFollowsConfiguredProvider guards that the AGENT_* configuration
// authenticates the matching catalog provider (so the example keeps working
// with a generic AGENT_API_KEY/AGENT_BASE_URL) and only that provider's models
// are available.
func TestCatalogFollowsConfiguredProvider(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "openai-compatible")
	t.Setenv("AGENT_PROVIDER_ID", "deepseek")
	t.Setenv("AGENT_BASE_URL", "https://example.test/v1")
	t.Setenv("AGENT_API_KEY", "secret")
	t.Setenv("AGENT_MODEL", "deepseek-v4-flash")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	if !models.available("deepseek-v4-flash") || !models.available("deepseek-v4-pro") {
		t.Fatal("the configured provider's models should be available via AGENT_API_KEY")
	}
	if models.available("gpt-5") || models.available("claude-sonnet-4") {
		t.Fatal("other providers should be unavailable")
	}
	if got := resolveDefaultModel(""); got != "deepseek-v4-flash" {
		t.Fatalf("default = %q, want deepseek-v4-flash", got)
	}
	info, err := models.modelInfo("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("modelInfo: %v", err)
	}
	if info.API.URL == nil || *info.API.URL != "https://example.test/v1" {
		t.Fatalf("base url = %v, want AGENT_BASE_URL", info.API.URL)
	}
}

// TestStateRoundTrip guards the persisted selection file.
func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if got := loadState(path); got != (chatState{}) {
		t.Fatalf("missing file = %+v, want zero", got)
	}
	want := chatState{Model: "m", Agent: "a"}
	saveState(path, want)
	if got := loadState(path); got != want {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
}
