package config

import (
	"os"
	"path/filepath"
	"reflect"
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
      apiKey: ${TEST_API_KEY}
servers:
  - name: a2a
    kind: a2a
    listen: 127.0.0.1:1
    root: /tmp/x
    workingDirectory: ${TEST_WORKDIR}
    agents: [build]
    cards:
      build:
        name: ${TEST_NAME:-Build}
        description: "cost $$5 for ${TEST_API_KEY}"
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
	card := cfg.Servers[0].Cards["build"]
	if card.Name != "Build" {
		t.Errorf("card name = %q, want the default", card.Name)
	}
	if card.Description != "cost $5 for sk-123" {
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

func TestLoadWithEnvCollectsNames(t *testing.T) {
	t.Setenv("AGENTD_TEST_KEY", "secret-value")
	body := `
models:
  - id: m
    model: ${AGENTD_TEST_KEY}
    protocol: openai-compatible-chat
    credentials:
      apiKey: ${AGENTD_TEST_KEY:-fallback}
`
	var cfg Config
	names, err := LoadWithEnv(writeTempConfig(t, body), &cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"AGENTD_TEST_KEY"}) {
		t.Fatalf("names = %v, want [AGENTD_TEST_KEY]", names)
	}
	if len(cfg.Models) != 1 || cfg.Models[0].Model != "secret-value" {
		t.Fatalf("model = %+v", cfg.Models)
	}
}

func TestLoadWithEnvNoRefs(t *testing.T) {
	body := `
models:
  - id: m
    model: m
    protocol: openai-compatible-chat
`
	var cfg Config
	names, err := LoadWithEnv(writeTempConfig(t, body), &cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("names = %v, want none", names)
	}
}
