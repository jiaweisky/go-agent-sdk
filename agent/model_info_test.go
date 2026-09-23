package agent

import (
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

func TestModelInfoProvider(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Provider: ProviderConfig{APIKey: "k"},
		Model:    "gpt-4o",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			return NewModelInfo(ModelAPIOpenAI, id, "", schema.ModelLimit{Context: 128_000, Output: 8_192})
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	limits := model.Route.Defaults.Limits
	if limits == nil || limits.Context != 128_000 || limits.Output != 8_192 {
		t.Fatalf("limits = %+v", limits)
	}
}

func TestModelInfoProviderUsesSessionModel(t *testing.T) {
	var seen string
	resolve, err := buildModelResolver(&ClientOptions{
		Provider: ProviderConfig{APIKey: "k"},
		Model:    "default",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			seen = id
			return NewModelInfo(ModelAPIAnthropic, id, "", schema.ModelLimit{Context: 200_000, Output: 4_096})
		},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if _, err := resolve(schema.SessionInfo{Model: &schema.ModelRef{ID: "claude-sonnet"}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if seen != "claude-sonnet" {
		t.Fatalf("resolved id = %q, want claude-sonnet", seen)
	}
}

func TestNewModelInfoRejectsBadInput(t *testing.T) {
	if _, err := NewModelInfo("bogus", "m", "", schema.ModelLimit{}); err == nil {
		t.Fatal("expected error for unknown api")
	}
	if _, err := NewModelInfo(ModelAPIOpenAICompatible, "m", "", schema.ModelLimit{}); err == nil {
		t.Fatal("expected error for openai-compatible without a base URL")
	}
}
