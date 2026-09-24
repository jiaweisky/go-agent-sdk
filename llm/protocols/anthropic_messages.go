package protocols

import (
	"encoding/json"
	"fmt"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
	"github.com/user/go-agent-sdk/llm/schema"
)

const anthropicMessagesAdapter = "anthropic-messages"
const AnthropicMessagesDefaultBaseURL = "https://api.anthropic.com/v1"
const AnthropicMessagesPath = "/messages"
const anthropicMessagesBreakpointCap = 4

var anthropicMessagesImageMIMEs = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {},
}

type anthropicMessagesTextBlock struct {
	Type         string                         `json:"type"`
	Text         string                         `json:"text"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type anthropicMessagesImageBlock struct {
	Type         string                         `json:"type"`
	Source       anthropicMessagesImageSource   `json:"source"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesThinkingBlock struct {
	Type         string                         `json:"type"`
	Thinking     string                         `json:"thinking"`
	Signature    *string                        `json:"signature,omitempty"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesToolUseBlock struct {
	Type         string                         `json:"type"`
	ID           string                         `json:"id"`
	Name         string                         `json:"name"`
	Input        any                            `json:"input"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesServerToolUseBlock struct {
	Type         string                         `json:"type"`
	ID           string                         `json:"id"`
	Name         string                         `json:"name"`
	Input        any                            `json:"input"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesServerToolResultBlock struct {
	Type         string                         `json:"type"`
	ToolUseID    string                         `json:"tool_use_id"`
	Content      any                            `json:"content"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesToolResultBlock struct {
	Type         string                         `json:"type"`
	ToolUseID    string                         `json:"tool_use_id"`
	Content      any                            `json:"content"`
	IsError      *bool                          `json:"is_error,omitempty"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesCacheControl struct {
	Type string  `json:"type"`
	TTL  *string `json:"ttl,omitempty"`
}

type anthropicMessagesAnthropicMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicMessagesAnthropicTool struct {
	Name         string                         `json:"name"`
	Description  string                         `json:"description"`
	InputSchema  any                            `json:"input_schema"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessagesAnthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessagesAnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessagesBody struct {
	Model         string                              `json:"model"`
	System        []anthropicMessagesTextBlock        `json:"system,omitempty"`
	Messages      []anthropicMessagesAnthropicMessage `json:"messages"`
	Tools         []anthropicMessagesAnthropicTool    `json:"tools,omitempty"`
	ToolChoice    any                                 `json:"tool_choice,omitempty"`
	Stream        bool                                `json:"stream"`
	MaxTokens     int                                 `json:"max_tokens"`
	Temperature   *float64                            `json:"temperature,omitempty"`
	TopP          *float64                            `json:"top_p,omitempty"`
	TopK          *float64                            `json:"top_k,omitempty"`
	StopSequences []string                            `json:"stop_sequences,omitempty"`
	Thinking      *anthropicMessagesAnthropicThinking `json:"thinking,omitempty"`
}

type anthropicMessagesAnthropicUsage struct {
	InputTokens          *int                    `json:"input_tokens,omitempty"`
	OutputTokens         *int                    `json:"output_tokens,omitempty"`
	CacheCreationInput   *int                    `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens *int                    `json:"cache_read_input_tokens,omitempty"`
	ProviderMetadata     schema.ProviderMetadata `json:"-"`
}

func (u *anthropicMessagesAnthropicUsage) UnmarshalJSON(data []byte) error {
	type plain anthropicMessagesAnthropicUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = anthropicMessagesAnthropicUsage(decoded)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		u.ProviderMetadata = schema.ProviderMetadata{"anthropic": raw}
	}
	return nil
}

type anthropicMessagesStreamBlock struct {
	Type      string  `json:"type,omitempty"`
	ID        *string `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	Text      *string `json:"text,omitempty"`
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`
	Input     any     `json:"input,omitempty"`
	ToolUseID *string `json:"tool_use_id,omitempty"`
	Content   any     `json:"content,omitempty"`
}

type anthropicMessagesStreamDelta struct {
	Type         *string `json:"type,omitempty"`
	Text         *string `json:"text,omitempty"`
	Thinking     *string `json:"thinking,omitempty"`
	PartialJSON  *string `json:"partial_json,omitempty"`
	Signature    *string `json:"signature,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type anthropicMessagesAnthropicError struct {
	Type    *string `json:"type,omitempty"`
	Message *string `json:"message,omitempty"`
}

type anthropicMessagesAnthropicEvent struct {
	Type         string                           `json:"type"`
	Index        *int                             `json:"index,omitempty"`
	Message      *anthropicMessagesMessageEntry   `json:"message,omitempty"`
	ContentBlock *anthropicMessagesStreamBlock    `json:"content_block,omitempty"`
	Delta        *anthropicMessagesStreamDelta    `json:"delta,omitempty"`
	Usage        *anthropicMessagesAnthropicUsage `json:"usage,omitempty"`
	Error        *anthropicMessagesAnthropicError `json:"error,omitempty"`
}

type anthropicMessagesMessageEntry struct {
	Usage *anthropicMessagesAnthropicUsage `json:"usage,omitempty"`
}

type anthropicMessagesParserState struct {
	tools     map[string]*utils.PendingTool
	usage     *schema.Usage
	lifecycle utils.State
}

type anthropicMessagesBreakpointTracker struct {
	remaining int
	dropped   int
}

func anthropicMessagesNewBreakpoints(cap int) anthropicMessagesBreakpointTracker {
	return anthropicMessagesBreakpointTracker{remaining: cap}
}

func (b *anthropicMessagesBreakpointTracker) next(hint *schema.CacheHint) *anthropicMessagesCacheControl {
	if hint == nil {
		return nil
	}
	if hint.Type != "ephemeral" && hint.Type != "persistent" {
		return nil
	}
	if b.remaining <= 0 {
		b.dropped++
		return nil
	}
	b.remaining--
	if hint.TTLSeconds != nil && *hint.TTLSeconds >= 3600 {
		t := "1h"
		return &anthropicMessagesCacheControl{Type: "ephemeral", TTL: &t}
	}
	return &anthropicMessagesCacheControl{Type: "ephemeral"}
}

func anthropicMessagesAnthropicMetadata(m map[string]any) schema.ProviderMetadata {
	return schema.ProviderMetadata{"anthropic": m}
}

func anthropicMessagesSignatureFromMetadata(md schema.ProviderMetadata) *string {
	m := md["anthropic"]
	if len(m) == 0 {
		return nil
	}
	sig, _ := m["signature"].(string)
	if sig == "" {
		return nil
	}
	return &sig
}

var anthropicMessagesServerToolResultNames = map[string]string{
	"web_search_tool_result":     "web_search",
	"code_execution_tool_result": "code_execution",
	"web_fetch_tool_result":      "web_fetch",
}

func anthropicMessagesServerToolResultWire(partName string) string {
	switch partName {
	case "web_search":
		return "web_search_tool_result"
	case "code_execution":
		return "code_execution_tool_result"
	case "web_fetch":
		return "web_fetch_tool_result"
	}
	return ""
}

func anthropicMessagesIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func anthropicMessagesLowerTool(bp *anthropicMessagesBreakpointTracker, td schema.ToolDefinition, inputSchema any) anthropicMessagesAnthropicTool {
	return anthropicMessagesAnthropicTool{
		Name:         td.Name,
		Description:  td.Description,
		InputSchema:  inputSchema,
		CacheControl: bp.next(td.Cache),
	}
}

func anthropicMessagesLowerToolChoice(tc schema.ToolChoice) (any, error) {
	return MatchToolChoice("Anthropic Messages", tc,
		anthropicMessagesAnthropicToolChoice{Type: "auto"},
		nil,
		anthropicMessagesAnthropicToolChoice{Type: "any"},
		func(name string) any { return anthropicMessagesAnthropicToolChoice{Type: "tool", Name: name} },
	)
}

func anthropicMessagesLowerToolCall(part schema.ToolCallPart) anthropicMessagesToolUseBlock {
	return anthropicMessagesToolUseBlock{
		Type:  "tool_use",
		ID:    part.ID,
		Name:  part.Name,
		Input: part.Input,
	}
}

func anthropicMessagesLowerServerToolCall(part schema.ToolCallPart) anthropicMessagesServerToolUseBlock {
	return anthropicMessagesServerToolUseBlock{
		Type:  "server_tool_use",
		ID:    part.ID,
		Name:  part.Name,
		Input: part.Input,
	}
}

func anthropicMessagesLowerImage(part schema.MediaPart) (*anthropicMessagesImageBlock, error) {
	media, err := utils.ValidateMedia("Anthropic Messages", part, anthropicMessagesImageMIMEs)
	if err != nil {
		return nil, err
	}
	return &anthropicMessagesImageBlock{
		Type: "image",
		Source: anthropicMessagesImageSource{
			Type:      "base64",
			MediaType: media.MIME,
			Data:      media.Base64,
		},
	}, nil
}

func anthropicMessagesLowerToolResultContentItem(item schema.ToolContent) (any, error) {
	switch v := item.(type) {
	case schema.ToolTextContent:
		return anthropicMessagesTextBlock{Type: "text", Text: v.Text}, nil
	case schema.ToolFileContent:
		media, err := utils.ValidateToolFile("Anthropic Messages", v, anthropicMessagesImageMIMEs)
		if err != nil {
			return nil, err
		}
		return anthropicMessagesImageBlock{
			Type: "image",
			Source: anthropicMessagesImageSource{
				Type:      "base64",
				MediaType: media.MIME,
				Data:      media.Base64,
			},
		}, nil
	}
	return anthropicMessagesTextBlock{Type: "text", Text: fmt.Sprint(item)}, nil
}

func anthropicMessagesLowerToolResultContent(part schema.ToolResultPart) (any, error) {
	if part.Result.Type != "content" {
		return ToolResultText(part), nil
	}
	content, ok := part.Result.Value.([]schema.ToolContent)
	if !ok {
		return ToolResultText(part), nil
	}
	items := []any{}
	for _, c := range content {
		item, err := anthropicMessagesLowerToolResultContentItem(c)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func anthropicMessagesThinkingBudget(m map[string]any) (int, bool) {
	if b, ok := m["budgetTokens"].(float64); ok {
		return int(b), true
	}
	if b, ok := m["budget_tokens"].(float64); ok {
		return int(b), true
	}
	return 0, false
}

func anthropicMessagesSupportsNativeSystemUpdate(request *schema.LLMRequest) bool {
	return string(request.Model.ModelID()) == "claude-opus-4-8"
}

func anthropicMessagesEndsInServerToolUse(msg schema.Message) bool {
	if len(msg.Content) == 0 {
		return false
	}
	last := msg.Content[len(msg.Content)-1]
	tc, ok := last.(schema.ToolCallPart)
	return ok && tc.ProviderExecuted != nil && *tc.ProviderExecuted
}

func anthropicMessagesCanUseNativeSystemUpdate(messages []schema.Message, index int) bool {
	if index <= 0 || index >= len(messages) {
		return false
	}
	prev := messages[index-1]
	if prev.Role == "system" {
		return false
	}
	if prev.Role != "user" && prev.Role != "tool" && !anthropicMessagesEndsInServerToolUse(prev) {
		return false
	}
	if index+1 == len(messages) {
		return true
	}
	return messages[index+1].Role == "assistant"
}

func anthropicMessagesSplitsLocalToolResults(messages []schema.Message, index int) bool {
	pending := make(map[string]struct{})
	for i := 0; i < index; i++ {
		msg := messages[i]
		for _, p := range msg.Content {
			if msg.Role == "assistant" {
				if tc, ok := p.(schema.ToolCallPart); ok && (tc.ProviderExecuted == nil || !*tc.ProviderExecuted) {
					pending[tc.ID] = struct{}{}
				}
			}
			if msg.Role == "tool" {
				if tr, ok := p.(schema.ToolResultPart); ok {
					delete(pending, tr.ID)
				}
			}
		}
	}
	return len(pending) > 0
}

func anthropicMessagesFromRequest(request *schema.LLMRequest) (any, error) {
	gen := request.Generation
	bp := anthropicMessagesNewBreakpoints(anthropicMessagesBreakpointCap)

	var toolChoice any
	if request.ToolChoice != nil {
		choice, err := anthropicMessagesLowerToolChoice(*request.ToolChoice)
		if err != nil {
			return nil, err
		}
		toolChoice = choice
	}
	outputLimit := 4096
	if defaults := request.Model.GetDefaults(); defaults != nil && defaults.Limits != nil {
		outputLimit = defaults.Limits.Output
	} else if defaults := request.Model.GetRouteDefaults(); defaults != nil && defaults.Limits != nil {
		outputLimit = defaults.Limits.Output
	}
	if gen != nil && gen.MaxTokens != nil {
		outputLimit = *gen.MaxTokens
	}

	var tools []anthropicMessagesAnthropicTool
	if len(request.Tools) > 0 && (request.ToolChoice == nil || request.ToolChoice.Type != "none") {
		for _, t := range request.Tools {
			tools = append(tools, anthropicMessagesLowerTool(&bp, t, ProjectToolSchema(request.Model, t.InputSchema)))
		}
	}

	var system []anthropicMessagesTextBlock
	for _, p := range request.System {
		block := anthropicMessagesTextBlock{Type: "text", Text: p.Text}
		if cc := bp.next(p.Cache); cc != nil {
			block.CacheControl = cc
		}
		system = append(system, block)
	}

	messages, err := anthropicMessagesLowerMessages(request, &bp)
	if err != nil {
		return nil, err
	}

	if bp.dropped > 0 {
		// log dropped breakpoints
	}

	var thinking *anthropicMessagesAnthropicThinking
	if raw := request.ProviderOptions["anthropic"]; len(raw) > 0 {
		if m, ok := raw["thinking"].(map[string]any); ok {
			if t, _ := m["type"].(string); t == "enabled" {
				budget, ok := anthropicMessagesThinkingBudget(m)
				if !ok {
					return nil, InvalidRequest("Anthropic thinking provider option requires budgetTokens")
				}
				thinking = &anthropicMessagesAnthropicThinking{Type: "enabled", BudgetTokens: budget}
			}
		}
	}

	var genTemperature *float64
	var genTopP *float64
	var genTopK *float64
	var genStop []string
	if gen != nil {
		genTemperature = gen.Temperature
		genTopP = gen.TopP
		genTopK = gen.TopK
		genStop = gen.Stop
	}

	return anthropicMessagesBody{
		Model:         string(request.Model.ModelID()),
		System:        system,
		Messages:      messages,
		Tools:         tools,
		ToolChoice:    toolChoice,
		Stream:        true,
		MaxTokens:     outputLimit,
		Temperature:   genTemperature,
		TopP:          genTopP,
		TopK:          genTopK,
		StopSequences: genStop,
		Thinking:      thinking,
	}, nil
}

func anthropicMessagesLowerMessages(request *schema.LLMRequest, bp *anthropicMessagesBreakpointTracker) ([]anthropicMessagesAnthropicMessage, error) {
	var msgs []anthropicMessagesAnthropicMessage
	nativeSystem := anthropicMessagesSupportsNativeSystemUpdate(request)

	for i, msg := range request.Messages {
		if msg.Role == "system" {
			if anthropicMessagesSplitsLocalToolResults(request.Messages, i) {
				return nil, fmt.Errorf("Anthropic Messages system updates cannot split a local tool call from its tool result")
			}
			if nativeSystem && anthropicMessagesCanUseNativeSystemUpdate(request.Messages, i) {
				ns, err := anthropicMessagesLowerNativeSystemUpdate(msg, bp)
				if err != nil {
					return nil, err
				}
				msgs = append(msgs, ns)
				continue
			}
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
			block := anthropicMessagesTextBlock{Type: "text", Text: update}
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
				last := &msgs[len(msgs)-1]
				last.Content = append(last.Content, block)
			} else {
				msgs = append(msgs, anthropicMessagesAnthropicMessage{Role: "user", Content: []any{block}})
			}
			continue
		}
		if msg.Role == "user" {
			var content []any
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					content = append(content, anthropicMessagesTextBlock{Type: "text", Text: part.Text})
				case schema.MediaPart:
					img, err := anthropicMessagesLowerImage(part)
					if err != nil {
						return nil, err
					}
					content = append(content, *img)
				default:
					return nil, fmt.Errorf("Anthropic Messages user messages only support text and media")
				}
			}
			msgs = append(msgs, anthropicMessagesAnthropicMessage{Role: "user", Content: content})
			continue
		}
		if msg.Role == "assistant" {
			var content []any
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					content = append(content, anthropicMessagesTextBlock{Type: "text", Text: part.Text})
				case schema.ReasoningPart:
					sig := anthropicMessagesSignatureFromMetadata(part.ProviderMetadata)
					tb := anthropicMessagesThinkingBlock{Type: "thinking", Thinking: part.Text}
					if part.Encrypted != nil {
						tb.Signature = part.Encrypted
					} else if sig != nil {
						tb.Signature = sig
					}
					content = append(content, tb)
				case schema.ToolCallPart:
					if part.ProviderExecuted != nil && *part.ProviderExecuted {
						content = append(content, anthropicMessagesLowerServerToolCall(part))
					} else {
						content = append(content, anthropicMessagesLowerToolCall(part))
					}
				case schema.ToolResultPart:
					if part.ProviderExecuted != nil && *part.ProviderExecuted {
						wireType := anthropicMessagesServerToolResultWire(part.Name)
						if wireType == "" {
							return nil, fmt.Errorf("Anthropic Messages does not know how to round-trip server tool result for %s", part.Name)
						}
						content = append(content, anthropicMessagesServerToolResultBlock{
							Type:      wireType,
							ToolUseID: part.ID,
							Content:   part.Result.Value,
						})
					}
				default:
					return nil, fmt.Errorf("Anthropic Messages assistant messages only support text, reasoning, and tool-call content")
				}
			}
			msgs = append(msgs, anthropicMessagesAnthropicMessage{Role: "assistant", Content: content})
			continue
		}
		if msg.Role == "tool" {
			var content []any
			for _, p := range msg.Content {
				if part, ok := p.(schema.ToolResultPart); ok {
					c, err := anthropicMessagesLowerToolResultContent(part)
					if err != nil {
						return nil, err
					}
					isErr := part.Result.Type == "error"
					var errPtr *bool
					if isErr {
						ptr := true
						errPtr = &ptr
					}
					content = append(content, anthropicMessagesToolResultBlock{
						Type:      "tool_result",
						ToolUseID: part.ID,
						Content:   c,
						IsError:   errPtr,
					})
				}
			}
			msgs = append(msgs, anthropicMessagesAnthropicMessage{Role: "user", Content: content})
			continue
		}
	}
	return msgs, nil
}

func anthropicMessagesLowerNativeSystemUpdate(msg schema.Message, bp *anthropicMessagesBreakpointTracker) (anthropicMessagesAnthropicMessage, error) {
	var content []any
	for _, p := range msg.Content {
		if tp, ok := p.(schema.TextPart); ok {
			block := anthropicMessagesTextBlock{Type: "text", Text: tp.Text}
			if cc := bp.next(tp.Cache); cc != nil {
				block.CacheControl = cc
			}
			content = append(content, block)
		}
	}
	return anthropicMessagesAnthropicMessage{Role: "system", Content: content}, nil
}

func anthropicMessagesDecodeEvent(frame string) (any, error) {
	if frame == "" {
		return nil, nil
	}
	var evt anthropicMessagesAnthropicEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("Anthropic Messages: invalid stream event: %s: %w", frame, err)
	}
	return evt, nil
}

func anthropicMessagesMapFinishReason(reason *string) schema.FinishReason {
	if reason == nil {
		return ""
	}
	switch *reason {
	case "end_turn", "stop_sequence", "pause_turn":
		return schema.ReasonStop
	case "max_tokens":
		return schema.ReasonLength
	case "tool_use":
		return schema.ReasonToolCalls
	case "refusal":
		return schema.ReasonContentFilter
	}
	return schema.ReasonUnknown
}

func anthropicMessagesMapUsage(u *anthropicMessagesAnthropicUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	nonCached := anthropicMessagesIntVal(u.InputTokens)
	cacheRead := anthropicMessagesIntVal(u.CacheReadInputTokens)
	cacheWrite := anthropicMessagesIntVal(u.CacheCreationInput)
	inputTokens := nonCached + cacheRead + cacheWrite
	return &schema.Usage{
		InputTokens:           inputTokens,
		OutputTokens:          anthropicMessagesIntVal(u.OutputTokens),
		NonCachedInputTokens:  nonCached,
		CacheReadInputTokens:  cacheRead,
		CacheWriteInputTokens: cacheWrite,
		TotalTokens:           anthropicMessagesIntVal(TotalTokens(&inputTokens, u.OutputTokens, nil)),
		ProviderMetadata:      u.ProviderMetadata,
	}
}

func anthropicMessagesMergeUsage(left, right *schema.Usage) *schema.Usage {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	nonCached := right.NonCachedInputTokens
	if right.NonCachedInputTokens == 0 {
		nonCached = left.NonCachedInputTokens
	}
	cacheRead := right.CacheReadInputTokens
	if right.CacheReadInputTokens == 0 {
		cacheRead = left.CacheReadInputTokens
	}
	cacheWrite := right.CacheWriteInputTokens
	if right.CacheWriteInputTokens == 0 {
		cacheWrite = left.CacheWriteInputTokens
	}
	inputTokens := nonCached + cacheRead + cacheWrite
	outputTokens := right.OutputTokens
	if right.OutputTokens == 0 {
		outputTokens = left.OutputTokens
	}
	return &schema.Usage{
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		NonCachedInputTokens:  nonCached,
		CacheReadInputTokens:  cacheRead,
		CacheWriteInputTokens: cacheWrite,
		TotalTokens:           anthropicMessagesIntVal(TotalTokens(&inputTokens, &outputTokens, nil)),
		ProviderMetadata:      mergeProviderMetadata(left.ProviderMetadata, right.ProviderMetadata),
	}
}

// mergeProviderMetadata right-biased merges provider usage payloads, shallow
// merging the inner map per provider (mirrors the TS anthropic mergeUsage).
func mergeProviderMetadata(left, right map[string]map[string]any) map[string]map[string]any {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	merged := make(map[string]map[string]any, len(left)+len(right))
	for provider, values := range left {
		merged[provider] = values
	}
	for provider, values := range right {
		if existing, ok := merged[provider]; ok {
			inner := make(map[string]any, len(existing)+len(values))
			for k, v := range existing {
				inner[k] = v
			}
			for k, v := range values {
				inner[k] = v
			}
			merged[provider] = inner
		} else {
			merged[provider] = values
		}
	}
	return merged
}

func anthropicMessagesStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(anthropicMessagesParserState)
	evt, ok := event.(anthropicMessagesAnthropicEvent)
	if !ok {
		return state, nil, nil
	}
	switch evt.Type {
	case "message_start":
		ns, evts := anthropicMessagesOnMessageStart(s, evt)
		return ns, evts, nil
	case "content_block_start":
		return anthropicMessagesOnContentBlockStart(s, evt)
	case "content_block_delta":
		return anthropicMessagesOnContentBlockDelta(s, evt)
	case "content_block_stop":
		return anthropicMessagesOnContentBlockStop(s, evt)
	case "message_delta":
		ns, evts := anthropicMessagesOnMessageDelta(s, evt)
		return ns, evts, nil
	case "error":
		ns, evts := anthropicMessagesOnError(s, evt)
		return ns, evts, nil
	}
	return s, nil, nil
}

func anthropicMessagesOnMessageStart(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent) {
	usage := anthropicMessagesMapUsage(nil)
	if evt.Message != nil {
		usage = anthropicMessagesMapUsage(evt.Message.Usage)
	}
	if usage != nil {
		s.usage = anthropicMessagesMergeUsage(s.usage, usage)
	}
	return s, nil
}

func anthropicMessagesOnContentBlockStart(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent, error) {
	block := evt.ContentBlock
	if block == nil {
		return s, nil, nil
	}
	if (block.Type == "tool_use" || block.Type == "server_tool_use") && evt.Index != nil {
		var events []schema.LLMEvent
		utils.StepStart(&s.lifecycle, &events)
		provExecuted := block.Type == "server_tool_use"
		s.tools = utils.Start(s.tools, fmt.Sprint(*evt.Index), utils.PendingTool{
			ID:               anthropicMessagesStrVal(block.ID, fmt.Sprint(*evt.Index)),
			Name:             anthropicMessagesStrVal(block.Name, ""),
			ProviderExecuted: &provExecuted,
		})
		events = append(events, schema.NewToolInputStart(anthropicMessagesStrVal(block.ID, fmt.Sprint(*evt.Index)), anthropicMessagesStrVal(block.Name, "")))
		return s, events, nil
	}
	if block.Type == "text" && block.Text != nil && *block.Text != "" {
		var events []schema.LLMEvent
		idx := 0
		if evt.Index != nil {
			idx = *evt.Index
		}
		utils.TextDelta(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx), *block.Text)
		return s, events, nil
	}
	if block.Type == "thinking" && block.Thinking != nil && *block.Thinking != "" {
		var events []schema.LLMEvent
		idx := 0
		if evt.Index != nil {
			idx = *evt.Index
		}
		utils.ReasoningDelta(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), *block.Thinking)
		return s, events, nil
	}
	if t, ok := anthropicMessagesServerToolResultNames[block.Type]; ok {
		var events []schema.LLMEvent
		utils.StepStart(&s.lifecycle, &events)
		var isErr bool
		if block.Content != nil {
			if m, ok := block.Content.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "_tool_result_error" || len(t) > 0 && t[len(t)-8:] == "_error" {
					isErr = true
				}
			}
		}
		resultType := "json"
		resultValue := block.Content
		if isErr {
			resultType = "error"
		}
		pm := anthropicMessagesAnthropicMetadata(map[string]any{"blockType": block.Type})
		events = append(events, schema.NewToolResult(anthropicMessagesStrVal(block.ToolUseID, ""), t,
			schema.ToolResultValue{Type: resultType, Value: resultValue},
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)))
		return s, events, nil
	}
	return s, nil, nil
}

func anthropicMessagesOnContentBlockDelta(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent, error) {
	delta := evt.Delta
	if delta == nil {
		return s, nil, nil
	}
	idx := 0
	if evt.Index != nil {
		idx = *evt.Index
	}
	if anthropicMessagesStrVal(delta.Type, "") == "text_delta" && delta.Text != nil && *delta.Text != "" {
		var events []schema.LLMEvent
		utils.TextDelta(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx), *delta.Text)
		return s, events, nil
	}
	if anthropicMessagesStrVal(delta.Type, "") == "thinking_delta" && delta.Thinking != nil && *delta.Thinking != "" {
		var events []schema.LLMEvent
		utils.ReasoningDelta(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), *delta.Thinking)
		return s, events, nil
	}
	if anthropicMessagesStrVal(delta.Type, "") == "signature_delta" && delta.Signature != nil && *delta.Signature != "" {
		var events []schema.LLMEvent
		utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx),
			anthropicMessagesAnthropicMetadata(map[string]any{"signature": *delta.Signature}))
		return s, events, nil
	}
	if anthropicMessagesStrVal(delta.Type, "") == "input_json_delta" && evt.Index != nil {
		if delta.PartialJSON == nil || *delta.PartialJSON == "" {
			return s, nil, nil
		}
		result, err := utils.AppendExisting(anthropicMessagesAdapter, s.tools, fmt.Sprint(*evt.Index), *delta.PartialJSON,
			"Anthropic Messages tool argument delta is missing its tool call")
		if err != nil {
			return s, nil, err
		}
		var events []schema.LLMEvent
		if len(result.Events) > 0 {
			utils.StepStart(&s.lifecycle, &events)
		}
		events = append(events, result.Events...)
		s.tools = result.Tools
		return s, events, nil
	}
	return s, nil, nil
}

func anthropicMessagesOnContentBlockStop(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent, error) {
	if evt.Index == nil {
		return s, nil, nil
	}
	key := fmt.Sprint(*evt.Index)
	result, err := utils.FinishTool(anthropicMessagesAdapter, s.tools, key)
	if err != nil {
		return s, nil, err
	}
	var events []schema.LLMEvent
	if len(result.Events) > 0 {
		utils.StepStart(&s.lifecycle, &events)
	} else {
		utils.TextEnd(&s.lifecycle, &events, fmt.Sprintf("text-%d", *evt.Index))
		utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", *evt.Index))
	}
	events = append(events, result.Events...)
	s.tools = result.Tools
	return s, events, nil
}

func anthropicMessagesOnMessageDelta(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent) {
	usage := anthropicMessagesMergeUsage(s.usage, anthropicMessagesMapUsage(evt.Usage))
	var events []schema.LLMEvent
	var pm schema.ProviderMetadata
	if evt.Delta != nil && evt.Delta.StopSequence != nil && *evt.Delta.StopSequence != "" {
		pm = anthropicMessagesAnthropicMetadata(map[string]any{"stopSequence": *evt.Delta.StopSequence})
	}
	utils.Finish(&s.lifecycle, &events, struct {
		Reason           schema.FinishReason
		Usage            *schema.Usage
		ProviderMetadata schema.ProviderMetadata
	}{
		Reason:           anthropicMessagesMapFinishReason(anthropicMessagesDeltaStopReason(evt.Delta)),
		Usage:            usage,
		ProviderMetadata: pm,
	})
	s.usage = usage
	return s, events
}

func anthropicMessagesDeltaStopReason(delta *anthropicMessagesStreamDelta) *string {
	if delta == nil {
		return nil
	}
	return delta.StopReason
}

func anthropicMessagesOnError(s anthropicMessagesParserState, evt anthropicMessagesAnthropicEvent) (anthropicMessagesParserState, []schema.LLMEvent) {
	msg := "Anthropic Messages stream error"
	raw := ""
	if evt.Error != nil {
		t := anthropicMessagesStrVal(evt.Error.Type, "")
		raw = anthropicMessagesStrVal(evt.Error.Message, "")
		if t != "" && raw != "" {
			msg = fmt.Sprintf("%s: %s", t, raw)
		} else if raw != "" {
			msg = raw
		} else if t != "" {
			msg = t
		}
	}
	opts := []schema.EventOption{}
	if schema.IsContextOverflow(raw) {
		opts = append(opts, schema.WithEventClassification(schema.ClassificationContextOverflow))
	}
	events := []schema.LLMEvent{schema.NewProviderErrorEvent(msg, opts...)}
	return s, events
}

func anthropicMessagesStrVal(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

var AnthropicMessagesProtocol = route.Protocol{
	ID: anthropicMessagesAdapter,
	Body: route.ProtocolBody{
		From: anthropicMessagesFromRequest,
	},
	Stream: route.ProtocolStream{
		DecodeEvent: anthropicMessagesDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return anthropicMessagesParserState{
				tools:     utils.Empty(),
				lifecycle: utils.Initial(),
			}
		},
		Step: anthropicMessagesStep,
	},
}

var AnthropicMessagesHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:       anthropicMessagesAdapter,
	Provider: "anthropic",
	Protocol: AnthropicMessagesProtocol,
	Endpoint: route.NewStaticPath(AnthropicMessagesPath, route.WithBaseURL(AnthropicMessagesDefaultBaseURL)),
	Headers: func(request *schema.LLMRequest) map[string]string {
		return map[string]string{"anthropic-version": "2023-06-01"}
	},
	Transport: transport.SSEJson(),
})
