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

func TestOpenAICompatibleConfigure(t *testing.T) {
	apiKey := "k"
	facade := NewOpenAICompatible(protocols.OpenAICompatibleChatHTTPRoute, OpenAICompatibleGenericOptions{
		Provider: "a", BaseURL: "https://a/v1", APIKey: &apiKey,
	})
	next := facade.Configure(OpenAICompatibleGenericOptions{Provider: "b", BaseURL: "https://b/v1"})
	if next.ID != "b" {
		t.Fatalf("configured id = %q", next.ID)
	}
	model := next.Model("m")
	if model.Provider != "b" || model.Route.Endpoint.BaseURL != "https://b/v1" {
		t.Fatalf("configured model = %+v", model.Model)
	}
}

func TestOpenAICompatibleProfileConfigure(t *testing.T) {
	InitOpenAICompatible(protocols.OpenAICompatibleChatHTTPRoute)
	if len(OpenAICompatibleRoutes) != 1 {
		t.Fatalf("routes = %d", len(OpenAICompatibleRoutes))
	}
	// Profile configure fixes the provider and falls back to the profile BaseURL.
	groq := Groq.Configure(OpenAICompatibleGenericOptions{})
	if groq.ID != "groq" {
		t.Fatalf("profile id = %q", groq.ID)
	}
	if url := groq.Model("llama").Route.Endpoint.BaseURL; url != "https://api.groq.com/openai/v1" {
		t.Fatalf("profile base url = %q", url)
	}
	// An explicit BaseURL overrides the profile default.
	custom := Groq.Configure(OpenAICompatibleGenericOptions{BaseURL: "https://custom/v1"})
	if url := custom.Model("m").Route.Endpoint.BaseURL; url != "https://custom/v1" {
		t.Fatalf("custom base url = %q", url)
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
