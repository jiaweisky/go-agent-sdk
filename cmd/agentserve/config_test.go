package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != "127.0.0.1:8787" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "build" {
		t.Fatalf("agents = %v", cfg.Agents)
	}
	if cfg.InteractionFallback != "reject" {
		t.Fatalf("interactionFallback = %q", cfg.InteractionFallback)
	}
}

func TestLoadExampleConfig(t *testing.T) {
	cfg := defaultConfig()
	if err := serveconfig.Load("agentserve.example.yaml", &cfg); err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate example: %v", err)
	}
	if cfg.Model != "deepseek-flash" || cfg.Protocol != "openai-compatible-chat" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(serveconfig.PermissionRuleset(cfg.InboundPermissions)) != 3 {
		t.Fatalf("permission rules = %v", cfg.InboundPermissions)
	}
}

func TestFlagsOverrideFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentserve.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:1111\nmodel: file-model\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := defaultConfig()
	if err := serveconfig.Load(path, &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	flags := newFlags()
	if err := flags.parse([]string{"--model", "flag-model", "--agent", "plan"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	flags.apply(&cfg)
	if cfg.Model != "flag-model" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "plan" {
		t.Fatalf("agents = %v", cfg.Agents)
	}
}

func TestValidateRejectsMissingRoot(t *testing.T) {
	cfg := defaultConfig()
	cfg.Model = "m"
	cfg.Protocol = "openai-responses"
	if err := cfg.validate(); err == nil {
		t.Fatal("expected missing root to be rejected")
	}
}

func TestValidateNonLoopbackNeedsAuth(t *testing.T) {
	cfg := defaultConfig()
	cfg.Root = "/tmp/x"
	cfg.Model = "m"
	cfg.Protocol = "openai-responses"
	cfg.Credentials.APIKey = "k"
	cfg.Listen = "0.0.0.0:8787"
	if err := cfg.validate(); err == nil {
		t.Fatal("expected non-loopback without auth to be rejected")
	}
}
