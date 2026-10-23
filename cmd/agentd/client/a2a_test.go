package client

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestModelModes(t *testing.T) {
	modes := ModelModes([]schema.ModelInfo{{
		ID: "m",
		Capabilities: schema.ModelCapabilities{
			Input: []string{"image", "pdf"},
		},
	}})
	got := modes["m"]
	for _, want := range []string{"text/plain", "application/json", "image/png", "application/pdf"} {
		if !containsString(got, want) {
			t.Fatalf("modes %v missing %q", got, want)
		}
	}
	if modes := ModelModes([]schema.ModelInfo{{ID: "text-only"}}); modes["text-only"] != nil {
		t.Fatalf("a model without modalities = %v, want nil", modes["text-only"])
	}
}

func TestApplyCard(t *testing.T) {
	expose := &schema.AgentExpose{}
	applyCard(expose, config.CardConfig{
		Name:        "Build",
		Description: "builds things",
		Version:     "1.2.3",
		Provider:    &config.ProviderConfig{Organization: "org", URL: "https://example"},
		Skills: []config.SkillConfig{{
			ID: "review", Name: "Review", Description: "reviews code",
		}},
	})
	if expose.Name != "Build" || expose.Description != "builds things" || expose.Version != "1.2.3" {
		t.Fatalf("expose = %+v", expose)
	}
	if expose.Provider == nil || expose.Provider.Organization != "org" {
		t.Fatalf("provider = %+v", expose.Provider)
	}
	if len(expose.Skills) != 1 || expose.Skills[0].ID != "review" {
		t.Fatalf("skills = %+v", expose.Skills)
	}
}

func TestExposeAgents(t *testing.T) {
	client, err := New(Params{
		Root:    t.TempDir(),
		Model:   "test-model",
		Catalog: testCatalog(t),
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	defer client.Close()

	infos, err := ExposeAgents(client, []string{"build"}, map[string]config.CardConfig{
		"build": {Name: "Build", Description: "builds things"},
	}, nil, "test-model")
	if err != nil {
		t.Fatalf("expose: %v", err)
	}
	if len(infos) != 1 || infos[0].Expose == nil {
		t.Fatalf("infos = %+v", infos)
	}
	if infos[0].Expose.Name != "Build" || infos[0].Expose.Description != "builds things" {
		t.Fatalf("expose = %+v", infos[0].Expose)
	}
}

func TestExposeAgentsRejectsUnknown(t *testing.T) {
	client, err := New(Params{
		Root:    t.TempDir(),
		Model:   "test-model",
		Catalog: testCatalog(t),
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	defer client.Close()

	if _, err := ExposeAgents(client, []string{"no-such-agent"}, nil, nil, "test-model"); err == nil {
		t.Fatal("expected an unknown agent to be rejected")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
