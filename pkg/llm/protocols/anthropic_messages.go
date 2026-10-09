package protocols

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/internal/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route/transport"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

const anthropicMessagesAdapter = "anthropic-messages"

// AnthropicMessagesDefaultBaseURL is the Anthropic API base URL.
const AnthropicMessagesDefaultBaseURL = "https://api.anthropic.com/v1"

// AnthropicMessagesPath is the Anthropic Messages endpoint path.
const AnthropicMessagesPath = "/messages"
const anthropicMessagesBreakpointCap = 4

type anthropicMessagesTextBlock struct {
	Type         string                         `json:"type"`
	Text         string                         `json:"text"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}
type anthropicMessagesImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
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

type anthropicMessagesMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicMessagesTool struct {
	Name         string                         `json:"name"`
	Description  string                         `json:"description"`
	InputSchema  any                            `json:"input_schema"`
	CacheControl *anthropicMessagesCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessagesToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessagesThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessagesBody struct {
	Model         string                       `json:"model"`
	System        []anthropicMessagesTextBlock `json:"system,omitempty"`
	Messages      []anthropicMessagesMessage   `json:"messages"`
	Tools         []anthropicMessagesTool      `json:"tools,omitempty"`
	ToolChoice    any                          `json:"tool_choice,omitempty"`
	Stream        bool                         `json:"stream"`
	MaxTokens     int                          `json:"max_tokens"`
	Temperature   *float64                     `json:"temperature,omitempty"`
	TopP          *float64                     `json:"top_p,omitempty"`
	TopK          *float64                     `json:"top_k,omitempty"`
	StopSequences []string                     `json:"stop_sequences,omitempty"`
	Thinking      *anthropicMessagesThinking   `json:"thinking,omitempty"`
}

type anthropicMessagesUsage struct {
	InputTokens          *int                    `json:"input_tokens,omitempty"`
	OutputTokens         *int                    `json:"output_tokens,omitempty"`
	CacheCreationInput   *int                    `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens *int                    `json:"cache_read_input_tokens,omitempty"`
	ProviderMetadata     schema.ProviderMetadata `json:"-"`
}

// UnmarshalJSON reads the usage payload Anthropic sends.
func (u *anthropicMessagesUsage) UnmarshalJSON(data []byte) error {
	type plain anthropicMessagesUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = anthropicMessagesUsage(decoded)
	u.ProviderMetadata = providerMetadata("anthropic", rawRecord(data))
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

type anthropicMessagesError struct {
	Type    *string `json:"type,omitempty"`
	Message *string `json:"message,omitempty"`
}

type anthropicMessagesEvent struct {
	Type         string                         `json:"type"`
	Index        *int                           `json:"index,omitempty"`
	Message      *anthropicMessagesMessageEntry `json:"message,omitempty"`
	ContentBlock *anthropicMessagesStreamBlock  `json:"content_block,omitempty"`
	Delta        *anthropicMessagesStreamDelta  `json:"delta,omitempty"`
	Usage        *anthropicMessagesUsage        `json:"usage,omitempty"`
	Error        *anthropicMessagesError        `json:"error,omitempty"`
}

type anthropicMessagesMessageEntry struct {
	Usage *anthropicMessagesUsage `json:"usage,omitempty"`
}

type anthropicMessagesParserState struct {
	tools     map[string]*utils.PendingTool
	usage     *schema.Usage
	lifecycle utils.LifecycleState
}

type anthropicMessagesBreakpointTracker struct {
	remaining int
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
		return nil
	}
	b.remaining--
	if hint.TTLSeconds != nil && *hint.TTLSeconds >= 3600 {
		t := "1h"
		return &anthropicMessagesCacheControl{Type: "ephemeral", TTL: &t}
	}
	return &anthropicMessagesCacheControl{Type: "ephemeral"}
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
	for wire, name := range anthropicMessagesServerToolResultNames {
		if name == partName {
			return wire
		}
	}
	return ""
}

func anthropicMessagesLowerTool(
	bp *anthropicMessagesBreakpointTracker,
	td schema.ToolDefinition,
	inputSchema any,
) anthropicMessagesTool {
	return anthropicMessagesTool{
		Name:         td.Name,
		Description:  td.Description,
		InputSchema:  inputSchema,
		CacheControl: bp.next(td.Cache),
	}
}

func anthropicMessagesLowerToolChoice(tc schema.ToolChoice) (any, error) {
	return MatchToolChoice("Anthropic Messages", tc,
		anthropicMessagesToolChoice{Type: "auto"},
		nil,
		anthropicMessagesToolChoice{Type: "any"},
		func(name string) any { return anthropicMessagesToolChoice{Type: "tool", Name: name} },
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

func anthropicMessagesLowerServerToolCall(part schema.ToolCallPart) anthropicMessagesToolUseBlock {
	return anthropicMessagesToolUseBlock{
		Type:  "server_tool_use",
		ID:    part.ID,
		Name:  part.Name,
		Input: part.Input,
	}
}

func anthropicMessagesLowerImage(part schema.MediaPart) (*anthropicMessagesImageBlock, error) {
	source := part.Source
	if source == "" {
		source = schema.MediaSourceInline
	}
	switch source {
	case schema.MediaSourceURL:
		url, _ := part.Data.(string)
		if strings.TrimSpace(url) == "" {
			return nil, InvalidRequest("Anthropic image URL is empty")
		}
		return &anthropicMessagesImageBlock{Type: "image", Source: anthropicMessagesImageSource{Type: "url", URL: url}}, nil
	}
	media, err := utils.ValidateMedia("Anthropic Messages", part, imageMIMESet)
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
		source := v.Source
		if source == "" {
			source = schema.MediaSourceInline
		}
		if source == schema.MediaSourceURL {
			return anthropicMessagesImageBlock{Type: "image", Source: anthropicMessagesImageSource{Type: "url", URL: v.URI}}, nil
		}
		media, err := utils.ValidateToolFile("Anthropic Messages", v, imageMIMESet)
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

	tools := anthropicMessagesLowerTools(request, &bp)
	system := anthropicMessagesLowerSystem(request, &bp)
	messages, err := anthropicMessagesLowerMessages(request, &bp)
	if err != nil {
		return nil, err
	}
	thinking, err := anthropicMessagesLowerThinking(request)
	if err != nil {
		return nil, err
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
		MaxTokens:     anthropicMessagesOutputLimit(request),
		Temperature:   genTemperature,
		TopP:          genTopP,
		TopK:          genTopK,
		StopSequences: genStop,
		Thinking:      thinking,
	}, nil
}

// anthropicMessagesOutputLimit resolves the max output tokens: model defaults,
// then route defaults, then an explicit generation override.
func anthropicMessagesOutputLimit(request *schema.LLMRequest) int {
	outputLimit := 4096
	if defaults := request.Model.GetDefaults(); defaults != nil && defaults.Limits != nil {
		outputLimit = defaults.Limits.Output
	} else if defaults := request.Model.GetRouteDefaults(); defaults != nil && defaults.Limits != nil {
		outputLimit = defaults.Limits.Output
	}
	if gen := request.Generation; gen != nil && gen.MaxTokens != nil {
		outputLimit = *gen.MaxTokens
	}
	return outputLimit
}

func anthropicMessagesLowerTools(
	request *schema.LLMRequest,
	bp *anthropicMessagesBreakpointTracker,
) []anthropicMessagesTool {
	var tools []anthropicMessagesTool
	if len(request.Tools) == 0 || (request.ToolChoice != nil && request.ToolChoice.Type == "none") {
		return tools
	}
	for _, t := range request.Tools {
		tools = append(tools, anthropicMessagesLowerTool(bp, t, ProjectToolSchema(request.Model, t.InputSchema)))
	}
	return tools
}

func anthropicMessagesLowerSystem(
	request *schema.LLMRequest,
	bp *anthropicMessagesBreakpointTracker,
) []anthropicMessagesTextBlock {
	var system []anthropicMessagesTextBlock
	for _, p := range request.System {
		block := anthropicMessagesTextBlock{Type: "text", Text: p.Text}
		if cc := bp.next(p.Cache); cc != nil {
			block.CacheControl = cc
		}
		system = append(system, block)
	}
	return system
}

func anthropicMessagesLowerThinking(request *schema.LLMRequest) (*anthropicMessagesThinking, error) {
	raw := request.ProviderOptions["anthropic"]
	if len(raw) == 0 {
		return nil, nil
	}
	m, ok := raw["thinking"].(map[string]any)
	if !ok {
		return nil, nil
	}
	if t, _ := m["type"].(string); t != "enabled" {
		return nil, nil
	}
	budget, ok := anthropicMessagesThinkingBudget(m)
	if !ok {
		return nil, InvalidRequest("Anthropic thinking provider option requires budgetTokens")
	}
	return &anthropicMessagesThinking{Type: "enabled", BudgetTokens: budget}, nil
}

func anthropicMessagesLowerMessages(
	request *schema.LLMRequest,
	bp *anthropicMessagesBreakpointTracker,
) ([]anthropicMessagesMessage, error) {
	msgs := []anthropicMessagesMessage{}
	nativeSystem := anthropicMessagesSupportsNativeSystemUpdate(request)

	for i, msg := range request.Messages {
		switch msg.Role {
		case "system":
			next, err := anthropicMessagesLowerSystemMessage(msgs, request, i, msg, nativeSystem, bp)
			if err != nil {
				return nil, err
			}
			msgs = next
		case "user":
			content, err := anthropicMessagesLowerUserContent(msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, anthropicMessagesMessage{Role: "user", Content: content})
		case "assistant":
			content, err := anthropicMessagesLowerAssistantContent(msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, anthropicMessagesMessage{Role: "assistant", Content: content})
		case "tool":
			content, err := anthropicMessagesLowerToolContent(msg)
			if err != nil {
				return nil, err
			}
			// Anthropic requires every tool_result for a tool_use turn in the
			// single user message right after it, so merge the results of
			// parallel tool calls into one message.
			if i > 0 && request.Messages[i-1].Role == "tool" && len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
				msgs[len(msgs)-1].Content = append(msgs[len(msgs)-1].Content, content...)
				continue
			}
			msgs = append(msgs, anthropicMessagesMessage{Role: "user", Content: content})
		}
	}
	return msgs, nil
}

// anthropicMessagesLowerSystemMessage lowers a system message, appending the
// wrapped update to a trailing user message when it cannot be sent natively.
func anthropicMessagesLowerSystemMessage(
	msgs []anthropicMessagesMessage,
	request *schema.LLMRequest,
	index int,
	msg schema.Message,
	nativeSystem bool,
	bp *anthropicMessagesBreakpointTracker,
) ([]anthropicMessagesMessage, error) {
	if anthropicMessagesSplitsLocalToolResults(request.Messages, index) {
		return nil, fmt.Errorf("Anthropic Messages system updates cannot split a local tool call from its tool result")
	}
	if nativeSystem && anthropicMessagesCanUseNativeSystemUpdate(request.Messages, index) {
		ns, err := anthropicMessagesLowerNativeSystemUpdate(msg, bp)
		if err != nil {
			return nil, err
		}
		return append(msgs, ns), nil
	}
	update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
	block := anthropicMessagesTextBlock{Type: "text", Text: update}
	if cc := bp.next(SystemUpdateCache(msg.Content)); cc != nil {
		block.CacheControl = cc
	}
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
		msgs[len(msgs)-1].Content = append(msgs[len(msgs)-1].Content, block)
		return msgs, nil
	}
	return append(msgs, anthropicMessagesMessage{Role: "user", Content: []any{block}}), nil
}

func anthropicMessagesLowerUserContent(msg schema.Message) ([]any, error) {
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
	return content, nil
}

func anthropicMessagesLowerAssistantContent(msg schema.Message) ([]any, error) {
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
	return content, nil
}

func anthropicMessagesLowerToolContent(msg schema.Message) ([]any, error) {
	var content []any
	for _, p := range msg.Content {
		part, ok := p.(schema.ToolResultPart)
		if !ok {
			continue
		}
		c, err := anthropicMessagesLowerToolResultContent(part)
		if err != nil {
			return nil, err
		}
		var errPtr *bool
		if part.Result.Type == "error" {
			isErr := true
			errPtr = &isErr
		}
		content = append(content, anthropicMessagesToolResultBlock{
			Type:      "tool_result",
			ToolUseID: part.ID,
			Content:   c,
			IsError:   errPtr,
		})
	}
	return content, nil
}

func anthropicMessagesLowerNativeSystemUpdate(
	msg schema.Message,
	bp *anthropicMessagesBreakpointTracker,
) (anthropicMessagesMessage, error) {
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
	return anthropicMessagesMessage{Role: "system", Content: content}, nil
}

func anthropicMessagesDecodeEvent(frame string) (any, error) {
	if frame == "" {
		return nil, nil
	}
	var evt anthropicMessagesEvent
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

func anthropicMessagesMapUsage(u *anthropicMessagesUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	nonCached := intVal(u.InputTokens)
	cacheRead := intVal(u.CacheReadInputTokens)
	cacheWrite := intVal(u.CacheCreationInput)
	inputTokens := nonCached + cacheRead + cacheWrite
	return &schema.Usage{
		InputTokens:           inputTokens,
		OutputTokens:          intVal(u.OutputTokens),
		NonCachedInputTokens:  nonCached,
		CacheReadInputTokens:  cacheRead,
		CacheWriteInputTokens: cacheWrite,
		TotalTokens:           intVal(TotalTokens(&inputTokens, u.OutputTokens, nil)),
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
		TotalTokens:           intVal(TotalTokens(&inputTokens, &outputTokens, nil)),
		ProviderMetadata:      anthropicMessagesMergeProviderMetadata(left.ProviderMetadata, right.ProviderMetadata),
	}
}

// anthropicMessagesMergeProviderMetadata right-biased merges provider usage payloads, shallow
// merging the inner map per provider.
func anthropicMessagesMergeProviderMetadata(left, right map[string]map[string]any) map[string]map[string]any {
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
	evt, ok := event.(anthropicMessagesEvent)
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

func anthropicMessagesOnMessageStart(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent) {
	usage := anthropicMessagesMapUsage(nil)
	if evt.Message != nil {
		usage = anthropicMessagesMapUsage(evt.Message.Usage)
	}
	if usage != nil {
		s.usage = anthropicMessagesMergeUsage(s.usage, usage)
	}
	return s, nil
}

func anthropicMessagesOnContentBlockStart(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent, error) {
	block := evt.ContentBlock
	if block == nil {
		return s, nil, nil
	}
	if (block.Type == "tool_use" || block.Type == "server_tool_use") && evt.Index != nil {
		var events []schema.LLMEvent
		utils.StepStart(&s.lifecycle, &events)
		provExecuted := block.Type == "server_tool_use"
		s.tools = utils.ToolStreamStart(s.tools, fmt.Sprint(*evt.Index), utils.PendingTool{
			ID:               strVal(block.ID, fmt.Sprint(*evt.Index)),
			Name:             strVal(block.Name, ""),
			ProviderExecuted: &provExecuted,
		})
		events = append(events, schema.NewToolInputStart(strVal(block.ID, fmt.Sprint(*evt.Index)), strVal(block.Name, "")))
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
				if t, _ := m["type"].(string); strings.HasSuffix(t, "_tool_result_error") {
					isErr = true
				}
			}
		}
		resultType := "json"
		resultValue := block.Content
		if isErr {
			resultType = "error"
		}
		pm := providerMetadata("anthropic", map[string]any{"blockType": block.Type})
		events = append(events, schema.NewToolResult(strVal(block.ToolUseID, ""), t,
			schema.ToolResultValue{Type: resultType, Value: resultValue},
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)))
		return s, events, nil
	}
	return s, nil, nil
}

func anthropicMessagesOnContentBlockDelta(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent, error) {
	delta := evt.Delta
	if delta == nil {
		return s, nil, nil
	}
	idx := 0
	if evt.Index != nil {
		idx = *evt.Index
	}
	if strVal(delta.Type, "") == "text_delta" && delta.Text != nil && *delta.Text != "" {
		var events []schema.LLMEvent
		utils.TextDelta(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx), *delta.Text)
		return s, events, nil
	}
	if strVal(delta.Type, "") == "thinking_delta" && delta.Thinking != nil && *delta.Thinking != "" {
		var events []schema.LLMEvent
		utils.ReasoningDelta(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), *delta.Thinking)
		return s, events, nil
	}
	if strVal(delta.Type, "") == "signature_delta" && delta.Signature != nil && *delta.Signature != "" {
		var events []schema.LLMEvent
		utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx),
			providerMetadata("anthropic", map[string]any{"signature": *delta.Signature}))
		return s, events, nil
	}
	if strVal(delta.Type, "") == "input_json_delta" && evt.Index != nil {
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

func anthropicMessagesOnContentBlockStop(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent, error) {
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

func anthropicMessagesOnMessageDelta(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent) {
	usage := anthropicMessagesMergeUsage(s.usage, anthropicMessagesMapUsage(evt.Usage))
	var events []schema.LLMEvent
	var pm schema.ProviderMetadata
	if evt.Delta != nil && evt.Delta.StopSequence != nil && *evt.Delta.StopSequence != "" {
		pm = providerMetadata("anthropic", map[string]any{"stopSequence": *evt.Delta.StopSequence})
	}
	utils.LifecycleFinish(&s.lifecycle, &events, utils.FinishInput{
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

func anthropicMessagesOnError(
	s anthropicMessagesParserState,
	evt anthropicMessagesEvent,
) (anthropicMessagesParserState, []schema.LLMEvent) {
	msg := "Anthropic Messages stream error"
	raw := ""
	if evt.Error != nil {
		t := strVal(evt.Error.Type, "")
		raw = strVal(evt.Error.Message, "")
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

// AnthropicMessagesProtocol is the Anthropic Messages wire protocol.
var AnthropicMessagesProtocol = route.Protocol{
	ID: anthropicMessagesAdapter,
	Body: route.ProtocolBody{
		Schema: anthropicMessagesBodySchema,
		From:   anthropicMessagesFromRequest,
	},
	Stream: route.ProtocolStream{
		DecodeEvent: anthropicMessagesDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return anthropicMessagesParserState{
				tools:     utils.ToolStreamEmpty(),
				lifecycle: utils.LifecycleInitial(),
			}
		},
		Step: anthropicMessagesStep,
	},
}

// AnthropicMessagesHTTPRoute is the Anthropic Messages route.
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
