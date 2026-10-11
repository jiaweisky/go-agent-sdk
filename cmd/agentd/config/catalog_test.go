package config

import "testing"

func TestCatalogResolvesPerModel(t *testing.T) {
	def := ModelConfig{
		ID: "deepseek-flash", Model: "deepseek-flash", Protocol: "openai-compatible-chat",
		ModelBaseURL: "https://api.deepseek.com", ProviderID: "deepseek",
		Media:        &MediaConfig{Input: []string{"text", "image"}, Sources: []string{"inline", "url", "file"}},
		Credentials:  Credentials{APIKey: "ds"},
		ContextLimit: 1000, OutputLimit: 100,
	}
	models := []ModelConfig{{
		ID: "gemini-2.5-flash", Model: "gemini-2.5-flash", Protocol: "gemini", ProviderID: "google",
		Media:       &MediaConfig{Input: []string{"text", "image", "audio", "video"}, Sources: []string{"inline", "file"}},
		Credentials: Credentials{APIKey: "goog"},
	}}
	catalog, err := NewCatalog(def, models)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if catalog.DefaultID() != "deepseek-flash" {
		t.Fatalf("defaultID = %q", catalog.DefaultID())
	}

	info, err := catalog.ModelInfo("gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
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
	if info.Capabilities.Media == nil || len(info.Capabilities.Media.Sources) != 2 {
		t.Fatalf("media = %v", info.Capabilities.Media)
	}

	// An unknown id falls back to the default entry.
	unknown, err := catalog.ModelInfo("something-else")
	if err != nil {
		t.Fatalf("unknown ModelInfo: %v", err)
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
	chat := ModelConfig{ID: "deepseek-flash", Model: "deepseek-flash", Protocol: "openai-compatible-chat", Credentials: Credentials{APIKey: "k"}}
	anthropic := ModelConfig{ID: "deepseek-flash-anthropic", Model: "deepseek-flash", Protocol: "anthropic-messages", Credentials: Credentials{APIKey: "k"}}
	catalog, err := NewCatalog(chat, []ModelConfig{anthropic})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := catalog.ModelInfo("deepseek-flash-anthropic")
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
	}
	if info.ID != "deepseek-flash-anthropic" {
		t.Fatalf("id = %q, want the catalog id", info.ID)
	}
	if info.API.ID == nil || *info.API.ID != "deepseek-flash" {
		t.Fatalf("api.id = %v, want the wire model name", info.API.ID)
	}
}

func TestCatalogProviderIDDefaultsToProtocol(t *testing.T) {
	catalog, err := NewCatalog(ModelConfig{
		ID: "m", Model: "m", Protocol: "gemini", Credentials: Credentials{APIKey: "k"},
	}, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	info, err := catalog.ModelInfo("m")
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
	}
	if info.ProviderID != "gemini" {
		t.Fatalf("providerID = %q, want gemini", info.ProviderID)
	}
}

func TestNewCatalogErrors(t *testing.T) {
	if _, err := NewCatalog(ModelConfig{Protocol: "x"}, nil); err == nil {
		t.Fatal("expected a missing-id error")
	}
	if _, err := NewCatalog(ModelConfig{ID: "a", Protocol: "x"}, nil); err == nil {
		t.Fatal("expected a missing-model error")
	}
	if _, err := NewCatalog(ModelConfig{ID: "a", Model: "a", Protocol: "x"}, []ModelConfig{{ID: "a", Model: "a", Protocol: "y"}}); err == nil {
		t.Fatal("expected a duplicate-id error")
	}
}

func TestModelConfigValidate(t *testing.T) {
	ok := ModelConfig{ID: "m", Model: "m", Protocol: "gemini", Media: &MediaConfig{Sources: []string{"inline", "url"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	missingModel := ModelConfig{ID: "m", Protocol: "gemini"}
	if err := missingModel.Validate(); err == nil {
		t.Fatal("expected a missing-model error")
	}
	badSource := ModelConfig{ID: "m", Model: "m", Protocol: "gemini", Media: &MediaConfig{Sources: []string{"carrier-pigeon"}}}
	if err := badSource.Validate(); err == nil {
		t.Fatal("expected an unknown-source error")
	}
	badModality := ModelConfig{ID: "m", Model: "m", Protocol: "gemini", InputModalities: []string{"hologram"}}
	if err := badModality.Validate(); err == nil {
		t.Fatal("expected an unknown-modality error")
	}
}
