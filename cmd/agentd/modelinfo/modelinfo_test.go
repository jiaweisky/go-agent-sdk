package modelinfo

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestResolveEmptyModelRegistry(t *testing.T) {
	if _, err := Resolve(config.ModelRegistry{}, "missing"); err == nil {
		t.Fatal("expected an empty registry to be rejected")
	}
}

func TestResolveFallsBackToDefault(t *testing.T) {
	registry, err := config.NewModelRegistry(config.ModelConfig{
		ID: "default", Model: "default", Protocol: "gemini",
	}, nil)
	if err != nil {
		t.Fatalf("NewModelRegistry: %v", err)
	}
	info, err := Resolve(registry, "unknown")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.ID != "default" {
		t.Fatalf("id = %q, want the default entry", info.ID)
	}
}

func TestList(t *testing.T) {
	registry, err := config.NewModelRegistry(config.ModelConfig{
		ID: "one", Model: "one", Protocol: "gemini",
	}, []config.ModelConfig{{ID: "two", Model: "two", Protocol: "gemini"}})
	if err != nil {
		t.Fatalf("NewModelRegistry: %v", err)
	}
	infos, err := List(registry)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 || infos[0].ID != "one" || infos[1].ID != "two" {
		t.Fatalf("infos = %+v", infos)
	}
}
func TestListEmptyModelRegistry(t *testing.T) {
	if _, err := List(config.ModelRegistry{}); err == nil {
		t.Fatal("expected an empty registry to be rejected")
	}
}
