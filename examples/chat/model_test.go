package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/schema"
)

func activeSession(t *testing.T, c *chat) {
	t.Helper()
	session, err := c.client.CreateSession(context.Background(), &agent.SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	c.switchSession(session)
}

// onlyDeepseekKeys makes the catalog's availability deterministic: the DeepSeek
// models are available, the other providers are not.
func onlyDeepseekKeys(t *testing.T) {
	t.Helper()
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
	for _, want := range []string{
		"Models (5):",
		"deepseek-v4-flash",
		"(deepseek · openai-compatible-chat)",
		"deepseek-flash-responses",
		"(deepseek · openai-responses)",
		"deepseek-flash-anthropic",
		"(deepseek · anthropic-messages)",
		"unknown or unavailable model: nope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestResolveDefaultModel guards the default priority: the persisted choice,
// then the catalog default, both limited to available models.
func TestResolveDefaultModel(t *testing.T) {
	onlyDeepseekKeys(t)

	// The catalog default is used when nothing is persisted.
	if got, err := resolveDefaultModel(""); err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("default = %q, %v; want deepseek-v4-flash", got, err)
	}
	// The persisted selection wins when available.
	if got, err := resolveDefaultModel("deepseek-v4-pro"); err != nil || got != "deepseek-v4-pro" {
		t.Fatalf("persisted = %q, %v; want deepseek-v4-pro", got, err)
	}
	// An unavailable persisted selection falls back to the catalog default.
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "test")
	if got, err := resolveDefaultModel("deepseek-v4-pro"); err != nil || got != "gpt-5" {
		t.Fatalf("fallback = %q, %v; want gpt-5", got, err)
	}
}

// TestCatalogUsesEntryCredentials guards that each entry uses its own APIKeyEnv
// and BaseURL: only the providers whose key is set are available, and the
// credential and endpoint come from the entry.
func TestCatalogUsesEntryCredentials(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "secret")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	if !models.Available("deepseek-v4-flash") || !models.Available("deepseek-flash") {
		t.Fatal("deepseek models should be available via DEEPSEEK_API_KEY")
	}
	if models.Available("gpt-5") || models.Available("claude-sonnet-4") {
		t.Fatal("other providers should be unavailable")
	}
	if got, err := resolveDefaultModel(""); err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("default = %q, %v; want deepseek-v4-flash", got, err)
	}
	info, err := models.ModelInfo("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("modelInfo: %v", err)
	}
	if info.API.URL == nil || *info.API.URL != "https://api.deepseek.com" {
		t.Fatalf("base url = %v, want the entry's BaseURL", info.API.URL)
	}
	cred, err := models.Credentials("deepseek")
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if cred.APIKey != "secret" {
		t.Fatalf("api key = %q, want secret", cred.APIKey)
	}
}

// TestModelEntryProtocols guards that one underlying model can be offered under
// protocol-specific catalog ids: the id selects the entry, Model is what the
// provider is asked for, and the endpoint differs per protocol.
func TestModelEntryProtocols(t *testing.T) {
	cases := []struct {
		id       string
		protocol string
		url      string
	}{
		{"deepseek-flash", "openai-compatible-chat", "https://api.deepseek.com"},
		{"deepseek-flash-responses", "openai-responses", "https://api.deepseek.com"},
		{"deepseek-flash-anthropic", "anthropic-messages", "https://api.deepseek.com/anthropic"},
	}
	for _, tc := range cases {
		info, err := models.ModelInfo(tc.id)
		if err != nil {
			t.Fatalf("modelInfo(%q): %v", tc.id, err)
		}
		if info.API.Protocol != schema.ProtocolID(tc.protocol) {
			t.Errorf("%s protocol = %q, want %q", tc.id, info.API.Protocol, tc.protocol)
		}
		if info.API.URL == nil || *info.API.URL != tc.url {
			t.Errorf("%s url = %v, want %q", tc.id, info.API.URL, tc.url)
		}
		if info.API.ID == nil || string(*info.API.ID) != "deepseek-flash" {
			t.Errorf("%s api id = %v, want deepseek-flash", tc.id, info.API.ID)
		}
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
