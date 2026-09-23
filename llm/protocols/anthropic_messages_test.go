package protocols

import (
	"testing"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

func TestAnthropicMessagesOutputLimitFallsBackToRouteDefaults(t *testing.T) {
	r := AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Provider: "test",
		Defaults: &route.RouteDefaultsInput{Limits: &schema.ModelLimits{Context: 100, Output: 12345}},
	})
	model := r.Model(route.RouteModelInput{ID: "claude"})

	body, err := r.Body.From(&schema.LLMRequest{Model: model})
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	request, ok := body.(anthropicMessagesBody)
	if !ok {
		t.Fatalf("body = %T", body)
	}
	if request.MaxTokens != 12345 {
		t.Fatalf("max_tokens = %d, want route default 12345", request.MaxTokens)
	}
}

func TestAnthropicMessagesOutputLimitPrefersModelDefaults(t *testing.T) {
	r := AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Provider: "test",
		Defaults: &route.RouteDefaultsInput{Limits: &schema.ModelLimits{Context: 100, Output: 12345}},
	})
	model := r.Model(route.RouteModelInput{
		ID:       "claude",
		Defaults: &schema.ModelDefaults{Limits: &schema.ModelLimits{Output: 7}},
	})

	body, err := r.Body.From(&schema.LLMRequest{Model: model})
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	request := body.(anthropicMessagesBody)
	if request.MaxTokens != 7 {
		t.Fatalf("max_tokens = %d, want model default 7", request.MaxTokens)
	}
}

func TestAnthropicMergeUsageMergesProviderMetadata(t *testing.T) {
	left := &schema.Usage{ProviderMetadata: schema.ProviderMetadata{"anthropic": map[string]any{"input_tokens": 1}}}
	right := &schema.Usage{ProviderMetadata: schema.ProviderMetadata{"anthropic": map[string]any{"output_tokens": 2}}}
	merged := anthropicMessagesMergeUsage(left, right)
	got := merged.ProviderMetadata["anthropic"]
	if got["input_tokens"] != 1 || got["output_tokens"] != 2 {
		t.Fatalf("merged providerMetadata = %+v", got)
	}
}
