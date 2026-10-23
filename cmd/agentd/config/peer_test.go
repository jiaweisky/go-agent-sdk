package config

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestValidateA2APeers(t *testing.T) {
	valid := []A2APeer{{Name: "echo", Endpoint: "http://127.0.0.1:9099"}}
	if err := ValidateA2APeers(valid); err != nil {
		t.Fatalf("valid peers rejected: %v", err)
	}
	// cardURL alone is a valid way to reach an A2A peer.
	if err := ValidateA2APeers([]A2APeer{{Name: "echo", CardURL: "http://host/card.json"}}); err != nil {
		t.Fatalf("cardURL-only A2A peer rejected: %v", err)
	}
	if err := ValidateA2APeers([]A2APeer{{Name: "echo", Mode: schema.AgentModePrimary}}); err == nil {
		t.Fatal("expected an A2A peer without endpoint/cardURL to be rejected")
	}
	if err := ValidateA2APeers([]A2APeer{{Endpoint: "http://host"}}); err == nil {
		t.Fatal("expected an A2A peer without a name to be rejected")
	}
	if err := ValidateA2APeers([]A2APeer{
		{Name: "echo", Endpoint: "http://a"},
		{Name: "echo", Endpoint: "http://b"},
	}); err == nil {
		t.Fatal("expected duplicate A2A peer names to be rejected")
	}
	if err := ValidateA2APeers([]A2APeer{{Name: "echo", Endpoint: "http://a", Mode: "bogus"}}); err == nil {
		t.Fatal("expected an unknown mode to be rejected")
	}
}

// TestValidateServerRejectsBadPeer guards that a server's peers are validated.
func TestValidateServerRejectsBadPeer(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a", Agents: []string{"build"},
			Peers: []A2APeer{{Name: "echo"}},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a server with an A2A peer missing endpoint/cardURL to be rejected")
	}
}
