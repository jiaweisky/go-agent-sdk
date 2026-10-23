package modelinfo

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestResolveEmptyCatalog(t *testing.T) {
	if _, err := Resolve(config.Catalog{}, "missing"); err == nil {
		t.Fatal("expected an empty catalog to be rejected")
	}
}

func TestResolveFallsBackToDefault(t *testing.T) {
	catalog, err := config.NewCatalog(config.ModelConfig{
		ID: "default", Model: "default", Protocol: "gemini",
	}, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := Resolve(catalog, "unknown")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.ID != "default" {
		t.Fatalf("id = %q, want the default entry", info.ID)
	}
}

func TestList(t *testing.T) {
	catalog, err := config.NewCatalog(config.ModelConfig{
		ID: "one", Model: "one", Protocol: "gemini",
	}, []config.ModelConfig{{ID: "two", Model: "two", Protocol: "gemini"}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	infos, err := List(catalog)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 || infos[0].ID != "one" || infos[1].ID != "two" {
		t.Fatalf("infos = %+v", infos)
	}
}
func TestListEmptyCatalog(t *testing.T) {
	if _, err := List(config.Catalog{}); err == nil {
		t.Fatal("expected an empty catalog to be rejected")
	}
}
