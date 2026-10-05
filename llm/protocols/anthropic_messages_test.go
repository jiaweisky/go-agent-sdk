package protocols

import (
	"encoding/json"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestAnthropicMessagesOutputLimitFallsBackToRouteDefaults(t *testing.T) {
	r := AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Provider: "test",
		Defaults: &route.RouteDefaultsInput{Limits: &schema.ModelLimits{Context: 100, Output: 12345}},
	})
	model := mustBindModel(t, r, route.RouteModelInput{ID: "claude"})

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
	model := mustBindModel(t, r, route.RouteModelInput{
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

func TestAnthropicThinkingRequiresBudget(t *testing.T) {
	model := mustBindModel(t, AnthropicMessagesHTTPRoute, route.RouteModelInput{ID: "claude"})
	for _, thinking := range []map[string]any{
		{"type": "enabled"},
		{"type": "enabled", "budgetTokens": "lots"},
	} {
		_, err := anthropicMessagesFromRequest(&schema.LLMRequest{
			Model:           model,
			ProviderOptions: schema.ProviderOptions{"anthropic": {"thinking": thinking}},
		})
		if err == nil {
			t.Fatalf("thinking %+v: want an error", thinking)
		}
	}

	body, err := anthropicMessagesFromRequest(&schema.LLMRequest{
		Model: model,
		ProviderOptions: schema.ProviderOptions{"anthropic": {
			"thinking": map[string]any{"type": "enabled", "budget_tokens": float64(2048)},
		}},
	})
	if err != nil {
		t.Fatalf("thinking: %v", err)
	}
	request := body.(anthropicMessagesBody)
	if request.Thinking == nil || request.Thinking.BudgetTokens != 2048 {
		t.Fatalf("thinking = %+v", request.Thinking)
	}
}

func TestAnthropicCanUseNativeSystemUpdate(t *testing.T) {
	user := schema.Message{Role: schema.RoleUser, Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "hi"}}}
	system := schema.Message{Role: schema.RoleSystem, Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "note"}}}
	assistant := schema.Message{Role: schema.RoleAssistant, Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "ok"}}}

	cases := []struct {
		name     string
		messages []schema.Message
		index    int
		want     bool
	}{
		{"last message", []schema.Message{user, system}, 1, true},
		{"before assistant", []schema.Message{user, system, assistant}, 1, true},
		{"before user", []schema.Message{user, system, user}, 1, false},
		{"after system", []schema.Message{system, system, assistant}, 1, false},
		{"first message", []schema.Message{system, assistant}, 0, false},
	}
	for _, tc := range cases {
		if got := anthropicMessagesCanUseNativeSystemUpdate(tc.messages, tc.index); got != tc.want {
			t.Fatalf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAnthropicMergesParallelToolResults guards that the results of parallel
// tool calls land in one user message right after the tool_use blocks: Anthropic
// rejects a tool_use whose tool_result is not in the immediately following
// message.
func TestAnthropicMergesParallelToolResults(t *testing.T) {
	model := mustBindModel(t, AnthropicMessagesHTTPRoute, route.RouteModelInput{ID: "claude"})
	result := func(id string) schema.ToolResultPart {
		return schema.ToolResultPart{
			Type: "tool-result", ID: id, Name: "tool",
			Result: schema.ToolResultValue{Type: "content", Value: []schema.ToolContent{
				schema.ToolTextContent{Type: "text", Text: "ok"},
			}},
		}
	}
	body, err := anthropicMessagesFromRequest(&schema.LLMRequest{
		Model: model,
		Messages: []schema.Message{
			schema.MessageUser("hi"),
			schema.NewMessage(schema.MessageInput{Role: schema.RoleAssistant, Content: []schema.ContentPart{
				schema.ToolCallPart{Type: "tool-call", ID: "a", Name: "bash"},
				schema.ToolCallPart{Type: "tool-call", ID: "b", Name: "tool"},
			}}),
			schema.MessageTool(result("a")),
			schema.MessageTool(result("b")),
		},
	})
	if err != nil {
		t.Fatalf("fromRequest: %v", err)
	}
	messages := body.(anthropicMessagesBody).Messages
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant, merged tool)", len(messages))
	}
	last := messages[2]
	if last.Role != "user" {
		t.Fatalf("last role = %q, want user", last.Role)
	}
	var ids []string
	for _, block := range last.Content {
		if tr, ok := block.(anthropicMessagesToolResultBlock); ok {
			ids = append(ids, tr.ToolUseID)
		}
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("tool_result ids = %v, want [a b]", ids)
	}
}

func TestAnthropicToolResultEmptyContentIsArray(t *testing.T) {
	got, err := anthropicMessagesLowerToolResultContent(schema.ToolResultPart{
		Type: "tool-result",
		ID:   "call_1",
		Name: "lookup",
		Result: schema.ToolResultValue{
			Type:  "content",
			Value: []schema.ToolContent{},
		},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("content = %s, want []", encoded)
	}
}
