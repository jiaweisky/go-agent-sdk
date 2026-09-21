package provider

import (
	"testing"

	"github.com/user/go-agent-sdk/llm/protocols"
)

func TestNewOpenAICompatible(t *testing.T) {
	apiKey := "test-key"
	facade := NewOpenAICompatible(protocols.OpenAICompatibleChatHTTPRoute, OpenAICompatibleGenericOptions{
		Provider: "deepseek",
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   &apiKey,
	})
	if facade.ID != "deepseek" {
		t.Fatalf("facade id = %q", facade.ID)
	}
	model := facade.Model("deepseek-chat")
	if model.ID != "deepseek-chat" {
		t.Fatalf("model id = %q", model.ID)
	}
	if model.Provider != "deepseek" {
		t.Fatalf("model provider = %q", model.Provider)
	}
	if model.Route.Endpoint.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("base url = %q", model.Route.Endpoint.BaseURL)
	}
	if model.Route.Provider != "deepseek" {
		t.Fatalf("route provider = %q", model.Route.Provider)
	}
}

func TestNewOpenAICompatibleDefaultsProvider(t *testing.T) {
	facade := NewOpenAICompatible(protocols.OpenAICompatibleChatHTTPRoute, OpenAICompatibleGenericOptions{
		BaseURL: "https://example.com/v1",
	})
	if facade.ID != OpenAICompatibleProviderID {
		t.Fatalf("facade id = %q", facade.ID)
	}
}

func TestOpenAICompatibleProfile(t *testing.T) {
	profile, ok := Profiles["groq"]
	if !ok || profile.Provider != "groq" || profile.BaseURL == "" {
		t.Fatalf("groq profile = %+v", profile)
	}
	if ByProvider["groq"].Provider != "groq" {
		t.Fatalf("ByProvider groq = %+v", ByProvider["groq"])
	}
}
