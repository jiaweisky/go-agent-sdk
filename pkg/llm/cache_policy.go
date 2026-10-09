package llm

import (
	"encoding/json"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

var respectsInlineHints = map[string]struct{}{
	"anthropic-messages": {},
	"bedrock-converse":   {},
}

func resolveCachePolicy(policy *schema.CachePolicy) *schema.CachePolicy {
	if policy == nil {
		return &schema.CachePolicy{Tools: boolPtr(true), System: boolPtr(true), Messages: "latest-user-message"}
	}
	if policy.Auto {
		return &schema.CachePolicy{Tools: boolPtr(true), System: boolPtr(true), Messages: "latest-user-message"}
	}
	if policy.None {
		return &schema.CachePolicy{}
	}
	return policy
}

func boolPtr(b bool) *bool { return &b }

func makeHint(ttlSeconds *int) schema.CacheHint {
	return schema.NewCacheHint("ephemeral", ttlSeconds)
}

func markLastTool(tools []schema.ToolDefinition, hint schema.CacheHint) []schema.ToolDefinition {
	if len(tools) == 0 {
		return tools
	}
	last := len(tools) - 1
	if tools[last].Cache != nil {
		return tools
	}
	result := make([]schema.ToolDefinition, len(tools))
	copy(result, tools)
	cp := result[last]
	cp.Cache = &hint
	result[last] = cp
	return result
}

func markLastSystem(system []schema.SystemPart, hint schema.CacheHint) []schema.SystemPart {
	if len(system) == 0 {
		return system
	}
	last := len(system) - 1
	if system[last].Cache != nil {
		return system
	}
	result := make([]schema.SystemPart, len(system))
	copy(result, system)
	cp := result[last]
	cp.Cache = &hint
	result[last] = cp
	return result
}

func lastIndexOfRole(messages []schema.Message, role schema.MessageRole) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == role {
			return i
		}
	}
	return -1
}

func partHasCache(p schema.ContentPart) bool {
	switch part := p.(type) {
	case schema.TextPart:
		return part.Cache != nil
	case schema.ToolResultPart:
		return part.Cache != nil
	}
	return false
}

func setPartCache(p schema.ContentPart, hint *schema.CacheHint) schema.ContentPart {
	switch part := p.(type) {
	case schema.TextPart:
		part.Cache = hint
		return part
	case schema.ToolResultPart:
		part.Cache = hint
		return part
	}
	return p
}

func markMessageAt(messages []schema.Message, index int, hint schema.CacheHint) []schema.Message {
	if index < 0 || index >= len(messages) {
		return messages
	}
	target := messages[index]
	if len(target.Content) == 0 {
		return messages
	}
	lastTextIdx := -1
	for i := len(target.Content) - 1; i >= 0; i-- {
		if tp, ok := target.Content[i].(schema.TextPart); ok && tp.Type == "text" {
			lastTextIdx = i
			break
		}
	}
	markAt := lastTextIdx
	if markAt < 0 {
		markAt = len(target.Content) - 1
	}
	if partHasCache(target.Content[markAt]) {
		return messages
	}
	nextContent := make([]schema.ContentPart, len(target.Content))
	copy(nextContent, target.Content)
	nextContent[markAt] = setPartCache(nextContent[markAt], &hint)
	nextMsg := target
	nextMsg.Content = nextContent
	result := make([]schema.Message, len(messages))
	copy(result, messages)
	result[index] = nextMsg
	return result
}

func markMessages(messages []schema.Message, strategy any, hint schema.CacheHint) []schema.Message {
	if len(messages) == 0 {
		return messages
	}
	switch s := strategy.(type) {
	case string:
		switch s {
		case "latest-user-message":
			return markMessageAt(messages, lastIndexOfRole(messages, schema.RoleUser), hint)
		case "latest-assistant":
			return markMessageAt(messages, lastIndexOfRole(messages, schema.RoleAssistant), hint)
		}
	case schema.CachePolicyMessages:
		return markTailMessages(messages, s.Tail, hint)
	case json.RawMessage:
		var decoded schema.CachePolicyMessages
		if err := json.Unmarshal(s, &decoded); err == nil && decoded.Tail != nil {
			return markTailMessages(messages, decoded.Tail, hint)
		}
		var name string
		if err := json.Unmarshal(s, &name); err == nil {
			return markMessages(messages, name, hint)
		}
	case map[string]any:
		if tail, ok := s["tail"].(float64); ok {
			n := int(tail)
			return markTailMessages(messages, &n, hint)
		}
	}
	return messages
}

func markTailMessages(messages []schema.Message, tail *int, hint schema.CacheHint) []schema.Message {
	if tail == nil {
		return messages
	}
	start := len(messages) - *tail
	if start < 0 {
		start = 0
	}
	result := messages
	for i := start; i < len(messages); i++ {
		result = markMessageAt(result, i, hint)
	}
	return result
}

// ApplyCachePolicy adds cache hints to the request for protocols that respect
// inline hints, returning the request unchanged otherwise.
func ApplyCachePolicy(request *schema.LLMRequest, protocolID string) *schema.LLMRequest {
	if _, ok := respectsInlineHints[protocolID]; !ok {
		return request
	}
	policy := resolveCachePolicy(request.Cache)
	if policy.Tools == nil && policy.System == nil && policy.Messages == nil {
		return request
	}
	hint := makeHint(policy.TTLSeconds)
	changed := false
	tools := request.Tools
	if policy.Tools != nil && *policy.Tools {
		tools = markLastTool(request.Tools, hint)
		changed = true
	}
	system := request.System
	if policy.System != nil && *policy.System {
		system = markLastSystem(request.System, hint)
		changed = true
	}
	messages := request.Messages
	if policy.Messages != nil {
		messages = markMessages(request.Messages, policy.Messages, hint)
		changed = true
	}
	if !changed {
		return request
	}
	clone := request.Clone()
	clone.Tools = tools
	clone.System = system
	clone.Messages = messages
	return clone
}
