package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentd.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadExpandsEnvironment(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-123")
	t.Setenv("TEST_WORKDIR", "/srv/work")
	body := `
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
    modelBaseURL: ${TEST_BASE:-https://default.example}
    credentials:
      apiKey: $TEST_API_KEY
servers:
  - name: a2a
    kind: a2a
    listen: 127.0.0.1:1
    root: ${TEST_ROOT:=/tmp/x}
    workingDirectory: ${TEST_WORKDIR}
    agents: [build]
    cards:
      build:
        name: ${TEST_NAME:-Build}
        description: "cost $$5 for ${TEST_API_KEY}, $1 literal"
`
	var cfg Config
	if err := Load(writeTempConfig(t, body), &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Models[0].Credentials.APIKey; got != "sk-123" {
		t.Errorf("apiKey = %q, want sk-123", got)
	}
	if got := cfg.Models[0].ModelBaseURL; got != "https://default.example" {
		t.Errorf("modelBaseURL = %q, want the default", got)
	}
	if got := cfg.Servers[0].WorkingDirectory; got != "/srv/work" {
		t.Errorf("workingDirectory = %q, want /srv/work", got)
	}
	if got := cfg.Servers[0].Root; got != "/tmp/x" {
		t.Errorf("root = %q, want the :=default", got)
	}
	card := cfg.Servers[0].Cards["build"]
	if card.Name != "Build" {
		t.Errorf("card name = %q, want the default", card.Name)
	}
	if card.Description != "cost $5 for sk-123, $1 literal" {
		t.Errorf("card description = %q, want the escaped+expanded text", card.Description)
	}
}

func TestLoadErrorsOnUndefinedEnvironment(t *testing.T) {
	const name = "AGENTD_TEST_UNSET_VAR"
	os.Unsetenv(name)
	body := `
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
    credentials:
      apiKey: ${` + name + `}
`
	var cfg Config
	if err := Load(writeTempConfig(t, body), &cfg); err == nil {
		t.Fatal("expected an undefined-variable error")
	}
}

func TestLoadErrorsOnUnsetVariableInComment(t *testing.T) {
	const name = "AGENTD_TEST_COMMENT_UNSET"
	os.Unsetenv(name)
	body := "# ${" + name + "}\n" + `
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
`
	var cfg Config
	if err := Load(writeTempConfig(t, body), &cfg); err == nil {
		t.Fatal("expected the whole file, comments included, to be expanded")
	}
}

func TestLoadEmptyFileKeepsConfig(t *testing.T) {
	cfg := Config{Models: []ModelConfig{{ID: "kept", Model: "m", Protocol: "openai-compatible-chat"}}}
	if err := Load(writeTempConfig(t, ""), &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Models) != 1 || cfg.Models[0].ID != "kept" {
		t.Fatalf("empty file must keep the caller's values: %+v", cfg.Models)
	}
}

func TestLoadQuotedPlaceholderKeepsYAMLSpecialChars(t *testing.T) {
	t.Setenv("AGENTD_TEST_QUOTED", "abc # def")
	body := `
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
    credentials:
      apiKey: "${AGENTD_TEST_QUOTED}"
`
	var cfg Config
	if err := Load(writeTempConfig(t, body), &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Models[0].Credentials.APIKey; got != "abc # def" {
		t.Fatalf("apiKey = %q, want the quoted value", got)
	}
}
