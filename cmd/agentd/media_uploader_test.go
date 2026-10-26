package main

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func testProviderFileAPI() *config.ProviderFileAPI {
	return &config.ProviderFileAPI{
		Kind:   "multipart-json",
		URL:    "https://files.example/upload",
		Auth:   config.ProviderFileAuth{Headers: map[string]string{"Authorization": "Bearer x"}},
		Expiry: config.ProviderFileExpiry{Style: "none"},
		MIME:   config.ProviderFileMIME{Allowed: []string{"image/png"}},
		Limits: config.ProviderFileLimits{MaxBytes: 1024, Timeout: "1m"},
	}
}

func TestMediaUploaderForSelectsExplicitProfiles(t *testing.T) {
	models := []config.ModelConfig{
		{ID: "a", Model: "a", Protocol: "deepseek-chat", ProviderID: "p", ProviderFileAPI: "pa"},
		{ID: "b", Model: "b", Protocol: "deepseek-chat", ProviderID: "q", ProviderFileAPI: "pb"},
	}
	profiles := map[string]*config.ProviderFileAPI{"pa": testProviderFileAPI(), "pb": testProviderFileAPI()}
	factory, err := mediaUploaderFor(config.Server{Name: "s", Models: []string{"a", "b"}}, models, profiles)
	if err != nil {
		t.Fatalf("mediaUploaderFor: %v", err)
	}
	if factory == nil || factory(schema.ProviderID("p")) == nil || factory(schema.ProviderID("q")) == nil {
		t.Fatal("each explicit providerID should resolve to its uploader")
	}
	if factory(schema.ProviderID("other")) != nil {
		t.Fatal("an unmapped providerID must resolve to nil")
	}
}

func TestMediaUploaderForRejectsProviderIDConflict(t *testing.T) {
	models := []config.ModelConfig{
		{ID: "a", Model: "a", Protocol: "deepseek-chat", ProviderID: "p", ProviderFileAPI: "pa"},
		{ID: "b", Model: "b", Protocol: "deepseek-chat", ProviderID: "p", ProviderFileAPI: "pb"},
	}
	profiles := map[string]*config.ProviderFileAPI{"pa": testProviderFileAPI(), "pb": testProviderFileAPI()}
	if _, err := mediaUploaderFor(config.Server{Name: "s", Models: []string{"a", "b"}}, models, profiles); err == nil {
		t.Fatal("one providerID mapped to two profiles must fail")
	}
}

func TestMediaFileHeadersForSelectsProfileHeaders(t *testing.T) {
	profile := testProviderFileAPI()
	profile.ModelHeaders = map[string]string{"anthropic-beta": "files"}
	models := []config.ModelConfig{{
		ID: "a", Model: "a", Protocol: "anthropic-messages", ProviderID: "p", ProviderFileAPI: "pa",
	}}
	headers := mediaFileHeadersFor(config.Server{Name: "s", Models: []string{"a"}}, models, map[string]*config.ProviderFileAPI{"pa": profile})
	if headers[schema.ProviderID("p")]["anthropic-beta"] != "files" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers := mediaFileHeadersFor(config.Server{Name: "s", Models: []string{"a"}}, models, map[string]*config.ProviderFileAPI{"pa": testProviderFileAPI()}); headers != nil {
		t.Fatalf("profile without model headers = %#v, want nil", headers)
	}
}

func TestMediaUploaderForSkipsIncompatibleProtocolOrMIME(t *testing.T) {
	profile := testProviderFileAPI()
	models := []config.ModelConfig{{
		ID: "a", Model: "a", Protocol: "openai-compatible-chat", ProviderID: "p", ProviderFileAPI: "pa",
	}}
	if factory, err := mediaUploaderFor(config.Server{Name: "s", Models: []string{"a"}}, models, map[string]*config.ProviderFileAPI{"pa": profile}); err != nil || factory != nil {
		t.Fatalf("protocol without file source = %v, %v; want nil, nil", factory, err)
	}

	models[0].Protocol = "openai-chat"
	if factory, err := mediaUploaderFor(config.Server{Name: "s", Models: []string{"a"}}, models, map[string]*config.ProviderFileAPI{"pa": profile}); err != nil || factory != nil {
		t.Fatalf("openai-chat with an image-only file profile = %v, %v; want nil, nil", factory, err)
	}

	profile.MIME.Allowed = []string{"application/pdf"}
	if factory, err := mediaUploaderFor(config.Server{Name: "s", Models: []string{"a"}}, models, map[string]*config.ProviderFileAPI{"pa": profile}); err != nil || factory == nil {
		t.Fatalf("compatible profile = %v, %v; want a factory", factory, err)
	}
}
