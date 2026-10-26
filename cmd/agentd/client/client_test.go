package client

import (
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func testModelRegistry(t *testing.T) config.ModelRegistry {
	t.Helper()
	registry, err := config.NewModelRegistry(config.ModelConfig{
		ID: "test-model", Model: "test-model", Protocol: "openai-responses",
		Credentials: config.Credentials{APIKey: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return registry
}

// TestNewRejectsEmptyModelRegistry guards the fail-fast check: a client without
// models would otherwise only fail on the first session.
func TestNewRejectsEmptyModelRegistry(t *testing.T) {
	_, err := New(Params{Root: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "model registry") {
		t.Fatalf("expected an empty model registry to be rejected, got %v", err)
	}
}

// TestNewRegistersPeers exercises the real client assembly: a server's peers end
// up registered as agents carrying the A2A backend, so the local model can
// delegate to them with the task tool.
func TestNewRegistersPeers(t *testing.T) {
	client, err := New(Params{
		Root:          t.TempDir(),
		Model:         "test-model",
		ModelRegistry: testModelRegistry(t),
		Peers: []config.A2APeer{{
			Name:        "echo",
			Endpoint:    "http://127.0.0.1:9099",
			Description: "echoes",
		}},
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	defer client.Close()

	info := client.Agents().Get(schema.AgentID("echo"))
	if info == nil {
		t.Fatal("A2A peer agent not registered")
	}
	if info.Backend == nil || info.Backend.Type != schema.AgentBackendA2A || info.Backend.A2A == nil {
		t.Fatalf("backend = %+v", info.Backend)
	}
	if info.Backend.A2A.Endpoint != "http://127.0.0.1:9099" {
		t.Fatalf("endpoint = %q", info.Backend.A2A.Endpoint)
	}
	if info.Mode != schema.AgentModeSubagent {
		t.Fatalf("mode = %q, want subagent (delegatable via task)", info.Mode)
	}
}

// TestNewRejectsPeerCollision guards that an A2A peer cannot silently rewrite an
// existing agent (here the built-in build agent) into an A2A backend.
func TestNewRejectsPeerCollision(t *testing.T) {
	_, err := New(Params{
		Root:          t.TempDir(),
		Model:         "test-model",
		ModelRegistry: testModelRegistry(t),
		Peers:         []config.A2APeer{{Name: "build", Endpoint: "http://127.0.0.1:9099"}},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected the colliding A2A peer to be rejected, got %v", err)
	}
}

func TestMCPConfigs(t *testing.T) {
	enabled := false
	configs, err := MCPConfigs([]config.MCPServer{
		{Name: "local", Type: config.MCPLocal, Command: []string{"srv", "arg"}, Timeout: "5s"},
		{Name: "remote", Type: config.MCPRemote, URL: "https://mcp.example", Headers: map[string]string{"Authorization": "Bearer x"}, Enabled: &enabled},
	})
	if err != nil {
		t.Fatalf("MCPConfigs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("len = %d, want 2", len(configs))
	}
	local := configs[0]
	if local.Name != "local" || local.Server.Type != "local" || local.Server.Timeout != 5*time.Second {
		t.Fatalf("local = %+v", local)
	}
	if len(local.Server.Command) != 2 || local.Server.Command[0] != "srv" {
		t.Fatalf("local command = %v", local.Server.Command)
	}
	remote := configs[1]
	if remote.Name != "remote" || remote.Server.Type != "remote" || remote.Server.URL != "https://mcp.example" {
		t.Fatalf("remote = %+v", remote)
	}
	if remote.Server.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("remote headers = %v", remote.Server.Headers)
	}
	if remote.Server.Enabled == nil || *remote.Server.Enabled {
		t.Fatalf("remote enabled = %v, want false", remote.Server.Enabled)
	}
}

func TestMCPConfigsRejectsBadTimeout(t *testing.T) {
	_, err := MCPConfigs([]config.MCPServer{{Name: "local", Type: config.MCPLocal, Command: []string{"srv"}, Timeout: "soon"}})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
}

func TestReferenceSources(t *testing.T) {
	description := "docs"
	hidden := true
	sources, err := ReferenceSources([]config.Reference{
		{Name: "local", Type: "local", Path: "/src/docs", Description: description, Hidden: &hidden},
		{Name: "git", Type: "git", Repository: "https://example/repo", Branch: "main"},
	})
	if err != nil {
		t.Fatalf("ReferenceSources: %v", err)
	}
	local, ok := sources["local"].(schema.ReferenceLocalSource)
	if !ok {
		t.Fatalf("local source = %T, want schema.ReferenceLocalSource", sources["local"])
	}
	if local.Path != "/src/docs" || local.Description == nil || *local.Description != description {
		t.Fatalf("local = %+v", local)
	}
	if local.Hidden == nil || !*local.Hidden {
		t.Fatalf("local hidden = %v", local.Hidden)
	}
	git, ok := sources["git"].(schema.ReferenceGitSource)
	if !ok {
		t.Fatalf("git source = %T, want schema.ReferenceGitSource", sources["git"])
	}
	if git.Repository != "https://example/repo" || git.Branch == nil || *git.Branch != "main" {
		t.Fatalf("git = %+v", git)
	}
	if git.Description != nil {
		t.Fatalf("git description = %v, want nil", git.Description)
	}
}

func TestReferenceSourcesRejectsInvalidType(t *testing.T) {
	_, err := ReferenceSources([]config.Reference{{Name: "bad", Type: "ftp"}})
	if err == nil || !strings.Contains(err.Error(), "must be local or git") {
		t.Fatalf("expected an unknown-type error, got %v", err)
	}
}
