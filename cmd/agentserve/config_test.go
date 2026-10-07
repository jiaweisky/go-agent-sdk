package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if !cfg.DropErroredTurns {
		t.Fatal("dropErroredTurns should default to true for the server")
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

func TestMCPConfig(t *testing.T) {
	enabled := true
	cfg := defaultConfig()
	cfg.Root = "/r"
	cfg.Model = "m"
	cfg.Protocol = "openai-responses"
	cfg.Credentials = serveconfig.Credentials{APIKey: "k"}
	cfg.MCP = []mcpConfig{
		{Name: "fs", Type: "local", Command: []string{"npx", "-y", "server"}, Timeout: "30s"},
		{Name: "remote", Type: "remote", URL: "https://mcp.example.com/mcp", Enabled: &enabled,
			Headers: map[string]string{"Authorization": "Bearer x"}},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	servers, err := cfg.mcpServers()
	if err != nil {
		t.Fatalf("mcpServers: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %d", len(servers))
	}
	if servers[0].Server.Type != "local" || servers[0].Server.Timeout != 30*time.Second {
		t.Fatalf("local server = %+v", servers[0].Server)
	}
	if servers[1].Server.URL != "https://mcp.example.com/mcp" || servers[1].Server.Enabled == nil || !*servers[1].Server.Enabled {
		t.Fatalf("remote server = %+v", servers[1].Server)
	}

	bad := defaultConfig()
	bad.MCP = []mcpConfig{{Name: "x", Type: "local"}}
	if err := bad.validate(); err == nil {
		t.Fatal("expected local without command to be rejected")
	}
}

func TestReferenceSources(t *testing.T) {
	cfg := defaultConfig()
	cfg.References = []referenceConfig{
		{Name: "docs", Type: "local", Path: "/srv/docs", Description: "d"},
		{Name: "lib", Type: "git", Repository: "https://github.com/example/lib", Branch: "main"},
	}
	sources, err := cfg.referenceSources()
	if err != nil {
		t.Fatalf("referenceSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources["docs"].ReferenceSourceType() != "local" {
		t.Fatalf("docs type = %q", sources["docs"].ReferenceSourceType())
	}
	if sources["lib"].ReferenceSourceType() != "git" {
		t.Fatalf("lib type = %q", sources["lib"].ReferenceSourceType())
	}

	bad := defaultConfig()
	bad.References = []referenceConfig{{Name: "x", Type: "local"}}
	if _, err := bad.referenceSources(); err == nil {
		t.Fatal("expected local without path to be rejected")
	}
}

func TestFlagsApplyAll(t *testing.T) {
	cfg := defaultConfig()
	flags := newFlags()
	if err := flags.parse([]string{
		"--listen", "127.0.0.1:1", "--root", "/r", "--working-dir", "/w",
		"--model", "m", "--protocol", "p", "--model-base-url", "https://x",
		"--subagent-depth", "2", "--title-auto",
		"--config-dir", "/a", "--config-dir", "/b", "--agent", "build", "--agent", "plan",
		"--interaction-timeout", "5m", "--interaction-fallback", "allow",
		"--public-route", "/api/health", "--cors", "https://x",
		"--subscriber-capacity", "10", "--keep-alive", "30s", "--allow-insecure",
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	flags.apply(&cfg)

	switch {
	case cfg.Listen != "127.0.0.1:1":
		t.Fatalf("listen = %q", cfg.Listen)
	case cfg.Root != "/r" || cfg.WorkingDirectory != "/w":
		t.Fatalf("root/workdir = %q/%q", cfg.Root, cfg.WorkingDirectory)
	case cfg.Model != "m" || cfg.Protocol != "p" || cfg.ModelBaseURL != "https://x":
		t.Fatalf("model = %+v", cfg)
	case cfg.SubagentDepth != 2 || !cfg.TitleAuto:
		t.Fatalf("subagentDepth/titleAuto = %d/%v", cfg.SubagentDepth, cfg.TitleAuto)
	case len(cfg.ConfigDirs) != 2 || len(cfg.Agents) != 2:
		t.Fatalf("configDirs/agents = %v/%v", cfg.ConfigDirs, cfg.Agents)
	case cfg.InteractionTimeout != "5m" || cfg.InteractionFallback != "allow":
		t.Fatalf("interaction = %q/%q", cfg.InteractionTimeout, cfg.InteractionFallback)
	case len(cfg.PublicRoutes) != 1 || len(cfg.CORS) != 1:
		t.Fatalf("publicRoutes/cors = %v/%v", cfg.PublicRoutes, cfg.CORS)
	case cfg.SubscriberCapacity != 10 || cfg.KeepAlive != "30s" || !cfg.AllowInsecure:
		t.Fatalf("capacity/keepAlive/allowInsecure = %d/%q/%v", cfg.SubscriberCapacity, cfg.KeepAlive, cfg.AllowInsecure)
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

func TestInputModalitiesValidation(t *testing.T) {
	cfg := defaultConfig()
	cfg.InputModalities = []string{"text", "image"}
	if err := cfg.validateModalities(); err != nil {
		t.Fatalf("valid modalities: %v", err)
	}
	cfg.InputModalities = []string{"hologram"}
	if err := cfg.validateModalities(); err == nil {
		t.Fatal("an unknown modality should be rejected")
	}
}
