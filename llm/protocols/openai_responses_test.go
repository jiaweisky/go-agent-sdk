package protocols

import (
	"encoding/json"
	"testing"

	"github.com/user/go-agent-sdk/llm/schema"
)

func statelessRequest(parts ...schema.ContentPart) *schema.LLMRequest {
	off := false
	return &schema.LLMRequest{
		ProviderOptions: schema.ProviderOptions{"openai": {"store": off}},
		Messages: []schema.Message{
			{Role: schema.RoleAssistant, Content: parts},
		},
	}
}

func lowerToJSON(t *testing.T, request *schema.LLMRequest) []map[string]any {
	t.Helper()
	items, err := openaiResponsesLowerMessages(request)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body []map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return body
}

func TestOpenAIResponsesLowerReasoningStateless(t *testing.T) {
	request := statelessRequest(
		schema.ReasoningPart{
			Type:             "reasoning",
			Text:             "Checked the previous diff.",
			ProviderMetadata: schema.ProviderMetadata{"openai": {"itemId": "rs_1", "reasoningEncryptedContent": "encrypted-state"}},
		},
		schema.TextPart{Type: "text", Text: "The parser changed."},
	)

	body := lowerToJSON(t, request)
	if len(body) != 2 {
		t.Fatalf("input = %v, want a reasoning item and an assistant message", body)
	}
	reasoning := body[0]
	if reasoning["type"] != "reasoning" || reasoning["encrypted_content"] != "encrypted-state" {
		t.Fatalf("reasoning item = %v", reasoning)
	}
	if _, ok := reasoning["id"]; ok {
		t.Fatalf("reasoning item must not carry an id: %v", reasoning)
	}
	summary, _ := reasoning["summary"].([]any)
	if len(summary) != 1 || summary[0].(map[string]any)["text"] != "Checked the previous diff." {
		t.Fatalf("summary = %v", reasoning["summary"])
	}
}

func TestOpenAIResponsesLowerReasoningDropsUnencryptedStateless(t *testing.T) {
	request := statelessRequest(schema.ReasoningPart{
		Type:             "reasoning",
		Text:             "no state",
		ProviderMetadata: schema.ProviderMetadata{"openai": {"itemId": "rs_1", "reasoningEncryptedContent": nil}},
	})

	if body := lowerToJSON(t, request); len(body) != 0 {
		t.Fatalf("input = %v, want none", body)
	}
}

func TestOpenAIResponsesLowerReasoningMergesDuplicateParts(t *testing.T) {
	request := statelessRequest(
		schema.ReasoningPart{
			Type:             "reasoning",
			Text:             "First",
			ProviderMetadata: schema.ProviderMetadata{"openai": {"itemId": "rs_1"}},
		},
		schema.ReasoningPart{
			Type:             "reasoning",
			Text:             "Second",
			ProviderMetadata: schema.ProviderMetadata{"openai": {"itemId": "rs_1", "reasoningEncryptedContent": "encrypted-state"}},
		},
	)

	body := lowerToJSON(t, request)
	if len(body) != 1 {
		t.Fatalf("input = %v, want one merged reasoning item", body)
	}
	if body[0]["encrypted_content"] != "encrypted-state" {
		t.Fatalf("encrypted_content = %v", body[0]["encrypted_content"])
	}
	summary, _ := body[0]["summary"].([]any)
	if len(summary) != 2 {
		t.Fatalf("summary = %v, want both parts", body[0]["summary"])
	}
}

func TestOpenAIResponsesLowerReasoningStoredUsesItemReference(t *testing.T) {
	request := &schema.LLMRequest{
		Messages: []schema.Message{
			{Role: schema.RoleAssistant, Content: []schema.ContentPart{
				schema.ReasoningPart{
					Type:             "reasoning",
					Text:             "thinking",
					ProviderMetadata: schema.ProviderMetadata{"openai": {"itemId": "rs_1"}},
				},
			}},
		},
	}

	body := lowerToJSON(t, request)
	if len(body) != 1 {
		t.Fatalf("input = %v", body)
	}
	if body[0]["type"] != "item_reference" || body[0]["id"] != "rs_1" {
		t.Fatalf("reasoning item = %v, want an item_reference", body[0])
	}
}
