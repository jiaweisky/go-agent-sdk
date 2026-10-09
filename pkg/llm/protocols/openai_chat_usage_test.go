package protocols

import (
	"encoding/json"
	"testing"
)

// TestOpenAIChatUsageCapturesRaw guards that provider-specific usage fields
// (e.g. DeepSeek's prompt_cache_hit_tokens) survive normalization.
func TestOpenAIChatUsageCapturesRaw(t *testing.T) {
	data := []byte(`{
		"prompt_tokens": 35,
		"completion_tokens": 16,
		"total_tokens": 51,
		"prompt_tokens_details": {"cached_tokens": 0},
		"completion_tokens_details": {"reasoning_tokens": 16},
		"prompt_cache_hit_tokens": 0,
		"prompt_cache_miss_tokens": 35
	}`)
	var usage openaiChatUsage
	if err := json.Unmarshal(data, &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mapped := openaiChatMapUsage(&usage)
	if mapped == nil {
		t.Fatal("nil usage")
	}
	openai := mapped.ProviderMetadata["openai"]
	if openai["prompt_cache_miss_tokens"] != float64(35) {
		t.Fatalf("providerMetadata = %+v", mapped.ProviderMetadata)
	}
	if openai["prompt_cache_hit_tokens"] != float64(0) {
		t.Fatalf("raw hit = %+v", openai["prompt_cache_hit_tokens"])
	}
	if mapped.NonCachedInputTokens != 35 {
		t.Fatalf("nonCached = %d", mapped.NonCachedInputTokens)
	}
}
