package config_test

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/modelinfo"
)

func TestCatalogResolvesPerModel(t *testing.T) {
	def := config.ModelConfig{
		ID: "deepseek-flash", Model: "deepseek-flash", Protocol: "openai-compatible-chat",
		ModelBaseURL: "https://api.deepseek.com", ProviderID: "deepseek",
		Media:        &config.MediaConfig{Input: []string{"text", "image"}, Forms: map[string][]string{"image": {"inline", "url", "file"}}},
		Credentials:  config.Credentials{APIKey: "ds"},
		ContextLimit: 1000, OutputLimit: 100,
	}
	models := []config.ModelConfig{{
		ID: "gemini-2.5-flash", Model: "gemini-2.5-flash", Protocol: "gemini", ProviderID: "google",
		Media:       &config.MediaConfig{Input: []string{"text", "image", "audio", "video"}, Forms: map[string][]string{"image": {"inline", "file"}, "audio": {"inline", "file"}, "video": {"inline", "file"}}},
		Credentials: config.Credentials{APIKey: "goog"},
	}}
	catalog, err := config.NewCatalog(def, models)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if catalog.DefaultID() != "deepseek-flash" {
		t.Fatalf("defaultID = %q", catalog.DefaultID())
	}

	info, err := modelinfo.Resolve(catalog, "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.ProviderID != "google" {
		t.Fatalf("providerID = %q, want google", info.ProviderID)
	}
	// The catalog id is surfaced as the model id; the wire name lives in API.ID.
	if info.ID != "gemini-2.5-flash" {
		t.Fatalf("id = %q, want the catalog id", info.ID)
	}
	if info.API.ID == nil || *info.API.ID != "gemini-2.5-flash" {
		t.Fatalf("api.id = %v, want the wire model name", info.API.ID)
	}
	if len(info.Capabilities.Input) != 4 {
		t.Fatalf("input = %v", info.Capabilities.Input)
	}
	if info.Capabilities.Media == nil || len(info.Capabilities.Media.Forms["image"]) != 2 {
		t.Fatalf("media = %v", info.Capabilities.Media)
	}

	// An unknown id falls back to the default entry.
	unknown, err := modelinfo.Resolve(catalog, "something-else")
	if err != nil {
		t.Fatalf("unknown Resolve: %v", err)
	}
	if unknown.ProviderID != "deepseek" || unknown.Limit.Context != 1000 {
		t.Fatalf("unknown = %+v", unknown)
	}

	if _, err := catalog.Credentials("google"); err != nil {
		t.Fatalf("Credentials(google): %v", err)
	}
	if _, err := catalog.Credentials("nope"); err == nil {
		t.Fatal("expected an unknown-provider error")
	}
}

func TestCatalogSeparatesIDFromWireModel(t *testing.T) {
	// Two catalog entries can share one wire model name (e.g. the same model
	// over different protocols); the catalog ids must stay distinct.
	chat := config.ModelConfig{ID: "deepseek-flash", Model: "deepseek-flash", Protocol: "openai-compatible-chat", Credentials: config.Credentials{APIKey: "k"}}
	anthropic := config.ModelConfig{ID: "deepseek-flash-anthropic", Model: "deepseek-flash", Protocol: "anthropic-messages", Credentials: config.Credentials{APIKey: "k"}}
	catalog, err := config.NewCatalog(chat, []config.ModelConfig{anthropic})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := modelinfo.Resolve(catalog, "deepseek-flash-anthropic")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.ID != "deepseek-flash-anthropic" {
		t.Fatalf("id = %q, want the catalog id", info.ID)
	}
	if info.API.ID == nil || *info.API.ID != "deepseek-flash" {
		t.Fatalf("api.id = %v, want the wire model name", info.API.ID)
	}
}

func TestCatalogProviderIDDefaultsToProtocol(t *testing.T) {
	catalog, err := config.NewCatalog(config.ModelConfig{
		ID: "m", Model: "m", Protocol: "gemini", Credentials: config.Credentials{APIKey: "k"},
	}, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := modelinfo.Resolve(catalog, "m")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.ProviderID != "gemini" {
		t.Fatalf("providerID = %q, want gemini", info.ProviderID)
	}
}

func TestNewCatalogErrors(t *testing.T) {
	if _, err := config.NewCatalog(config.ModelConfig{Protocol: "gemini"}, nil); err == nil {
		t.Fatal("expected a missing-id error")
	}
	if _, err := config.NewCatalog(config.ModelConfig{ID: "a", Protocol: "gemini"}, nil); err == nil {
		t.Fatal("expected a missing-model error")
	}
	if _, err := config.NewCatalog(config.ModelConfig{ID: "a", Model: "a", Protocol: "gemini"}, []config.ModelConfig{{ID: "a", Model: "a", Protocol: "gemini"}}); err == nil {
		t.Fatal("expected a duplicate-id error")
	}
}

func TestModelConfigValidate(t *testing.T) {
	ok := config.ModelConfig{ID: "m", Model: "m", Protocol: "gemini", Media: &config.MediaConfig{Forms: map[string][]string{"image": {"inline", "url"}}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	missingModel := config.ModelConfig{ID: "m", Protocol: "gemini"}
	if err := missingModel.Validate(); err == nil {
		t.Fatal("expected a missing-model error")
	}
	badSource := config.ModelConfig{ID: "m", Model: "m", Protocol: "gemini", Media: &config.MediaConfig{Forms: map[string][]string{"image": {"carrier-pigeon"}}}}
	if err := badSource.Validate(); err == nil {
		t.Fatal("expected an unknown-source error")
	}
	badModality := config.ModelConfig{ID: "m", Model: "m", Protocol: "gemini", InputModalities: []string{"hologram"}}
	if err := badModality.Validate(); err == nil {
		t.Fatal("expected an unknown-modality error")
	}
	badProtocol := config.ModelConfig{ID: "m", Model: "m", Protocol: "nope"}
	if err := badProtocol.Validate(); err == nil {
		t.Fatal("expected an unknown-protocol error")
	}
}

// TestModelInfoCarriesHeaders: a model's extra request headers reach the
// resolved ModelInfo (e.g. the anthropic-beta header for Files API references).
func TestModelInfoCarriesHeaders(t *testing.T) {
	entry := config.ModelConfig{
		ID: "claude", Model: "claude-3", Protocol: "anthropic-messages",
		Headers: map[string]string{"anthropic-beta": "files-api-2025-04-14"},
	}
	catalog, err := config.NewCatalog(entry, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := modelinfo.Resolve(catalog, "claude")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := info.Request.Headers["anthropic-beta"]; got != "files-api-2025-04-14" {
		t.Fatalf("headers = %v", info.Request.Headers)
	}
}
