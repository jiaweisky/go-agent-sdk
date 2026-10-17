package config

import (
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestBuildClientRegistersPeers exercises the real client assembly: a server's
// peers end up registered as agents carrying the A2A backend, so the local model
// can delegate to them with the task tool.
func TestBuildClientRegistersPeers(t *testing.T) {
	catalog, err := NewCatalog(testModel(), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	client, err := BuildClient(ClientParams{
		Root:    t.TempDir(),
		Model:   "test-model",
		Catalog: catalog,
		Peers: []Peer{{
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
		t.Fatal("peer agent not registered")
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

// TestBuildClientRejectsPeerCollision guards that a peer cannot silently rewrite
// an existing agent (here the built-in build agent) into an A2A backend.
func TestBuildClientRejectsPeerCollision(t *testing.T) {
	catalog, err := NewCatalog(testModel(), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	_, err = BuildClient(ClientParams{
		Root:    t.TempDir(),
		Model:   "test-model",
		Catalog: catalog,
		Peers:   []Peer{{Name: "build", Endpoint: "http://127.0.0.1:9099"}},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected the colliding peer to be rejected, got %v", err)
	}
}

func TestValidatePeers(t *testing.T) {
	valid := []Peer{{Name: "echo", Endpoint: "http://127.0.0.1:9099"}}
	if err := ValidatePeers(valid); err != nil {
		t.Fatalf("valid peers rejected: %v", err)
	}
	// cardURL alone is a valid way to reach a peer.
	if err := ValidatePeers([]Peer{{Name: "echo", CardURL: "http://host/card.json"}}); err != nil {
		t.Fatalf("cardURL-only peer rejected: %v", err)
	}
	if err := ValidatePeers([]Peer{{Name: "echo", Mode: schema.AgentModePrimary}}); err == nil {
		t.Fatal("expected a peer without endpoint/cardURL to be rejected")
	}
	if err := ValidatePeers([]Peer{{Endpoint: "http://host"}}); err == nil {
		t.Fatal("expected a peer without a name to be rejected")
	}
	if err := ValidatePeers([]Peer{
		{Name: "echo", Endpoint: "http://a"},
		{Name: "echo", Endpoint: "http://b"},
	}); err == nil {
		t.Fatal("expected duplicate peer names to be rejected")
	}
	if err := ValidatePeers([]Peer{{Name: "echo", Endpoint: "http://a", Mode: "bogus"}}); err == nil {
		t.Fatal("expected an unknown mode to be rejected")
	}
}

// TestValidateServerRejectsBadPeer guards that a server's peers are validated.
func TestValidateServerRejectsBadPeer(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
			Peers: []Peer{{Name: "echo"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a server with a peer missing endpoint/cardURL to be rejected")
	}
}

func TestPeerAgentConfig(t *testing.T) {
	cfg := peerAgentConfig(Peer{
		Name:        "echo",
		Endpoint:    "http://127.0.0.1:9099",
		Headers:     map[string]string{"Authorization": "Bearer x"},
		Description: "echoes",
	})
	if cfg.Mode == nil || *cfg.Mode != schema.AgentModeSubagent {
		t.Fatalf("default mode = %v, want subagent", cfg.Mode)
	}
	if cfg.Description != "echoes" {
		t.Fatalf("description = %q", cfg.Description)
	}
	if cfg.Backend == nil || cfg.Backend.Type != schema.AgentBackendA2A || cfg.Backend.A2A == nil {
		t.Fatalf("backend = %+v", cfg.Backend)
	}
	if cfg.Backend.A2A.Endpoint != "http://127.0.0.1:9099" {
		t.Fatalf("endpoint = %q", cfg.Backend.A2A.Endpoint)
	}
	if cfg.Backend.A2A.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("headers = %v", cfg.Backend.A2A.Headers)
	}

	primary := peerAgentConfig(Peer{Name: "p", Endpoint: "http://a", Mode: schema.AgentModePrimary})
	if primary.Mode == nil || *primary.Mode != schema.AgentModePrimary {
		t.Fatalf("explicit mode = %v, want primary", primary.Mode)
	}
}
