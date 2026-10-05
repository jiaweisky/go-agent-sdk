package protocols

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

const bedrockConverseAdapter = "bedrock-converse"

type bedrockConverseTextBlock struct {
	Text string `json:"text"`
}
type bedrockConverseToolUseBlock struct {
	ToolUse struct {
		ToolUseID string `json:"toolUseId"`
		Name      string `json:"name"`
		Input     any    `json:"input"`
	} `json:"toolUse"`
}
type bedrockConverseToolResultContentItem struct {
	Text  *string `json:"text,omitempty"`
	JSON  any     `json:"json,omitempty"`
	Image any     `json:"image,omitempty"`
}
type bedrockConverseToolResultBlock struct {
	ToolResult struct {
		ToolUseID string                                 `json:"toolUseId"`
		Content   []bedrockConverseToolResultContentItem `json:"content"`
		Status    string                                 `json:"status"`
	} `json:"toolResult"`
}
type bedrockConverseReasoningBlock struct {
	ReasoningContent struct {
		ReasoningText *struct {
			Text      string  `json:"text"`
			Signature *string `json:"signature,omitempty"`
		} `json:"reasoningText,omitempty"`
	} `json:"reasoningContent"`
}
type bedrockConverseMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}
type bedrockConverseToolSpec struct {
	ToolSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			JSON any `json:"json"`
		} `json:"inputSchema"`
	} `json:"toolSpec"`
}
type bedrockConverseBody struct {
	ModelID                      string                          `json:"modelId"`
	Messages                     []bedrockConverseMessage        `json:"messages"`
	System                       []any                           `json:"system,omitempty"`
	InferenceConfig              *bedrockConverseInferenceConfig `json:"inferenceConfig,omitempty"`
	ToolConfig                   *bedrockConverseToolConfig      `json:"toolConfig,omitempty"`
	AdditionalModelRequestFields any                             `json:"additionalModelRequestFields,omitempty"`
}
type bedrockConverseInferenceConfig struct {
	MaxTokens     *int     `json:"maxTokens,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"topP,omitempty"`
	StopSequences []string `json:"stopSequences,omitempty"`
}
type bedrockConverseToolConfig struct {
	Tools      []any `json:"tools"`
	ToolChoice any   `json:"toolChoice,omitempty"`
}

type bedrockConverseUsage struct {
	InputTokens           *int                    `json:"inputTokens,omitempty"`
	OutputTokens          *int                    `json:"outputTokens,omitempty"`
	TotalTokens           *int                    `json:"totalTokens,omitempty"`
	CacheReadInputTokens  *int                    `json:"cacheReadInputTokens,omitempty"`
	CacheWriteInputTokens *int                    `json:"cacheWriteInputTokens,omitempty"`
	ProviderMetadata      schema.ProviderMetadata `json:"-"`
}

// UnmarshalJSON reads the usage payload Bedrock sends.
func (u *bedrockConverseUsage) UnmarshalJSON(data []byte) error {
	type plain bedrockConverseUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = bedrockConverseUsage(decoded)
	u.ProviderMetadata = providerMetadata("bedrock", rawRecord(data))
	return nil
}

type bedrockConverseContentBlockStartEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Start             *struct {
		ToolUse *struct {
			ToolUseID string `json:"toolUseId"`
			Name      string `json:"name"`
		} `json:"toolUse,omitempty"`
	} `json:"start,omitempty"`
}
type bedrockConverseContentBlockDeltaEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Delta             *struct {
		Text             *string                 `json:"text,omitempty"`
		ToolUse          *struct{ Input string } `json:"toolUse,omitempty"`
		ReasoningContent *struct {
			Text      *string `json:"text,omitempty"`
			Signature *string `json:"signature,omitempty"`
		} `json:"reasoningContent,omitempty"`
	} `json:"delta,omitempty"`
}
type bedrockConverseContentBlockStopEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
}
type bedrockConverseMessageStopEvent struct {
	StopReason string `json:"stopReason"`
}
type bedrockConverseMetadataEvent struct {
	Usage   *bedrockConverseUsage `json:"usage,omitempty"`
	Metrics any                   `json:"metrics,omitempty"`
}

type bedrockConverseEvent struct {
	MessageStart                *struct{ Role string }                 `json:"messageStart,omitempty"`
	ContentBlockStart           *bedrockConverseContentBlockStartEvent `json:"contentBlockStart,omitempty"`
	ContentBlockDelta           *bedrockConverseContentBlockDeltaEvent `json:"contentBlockDelta,omitempty"`
	ContentBlockStop            *bedrockConverseContentBlockStopEvent  `json:"contentBlockStop,omitempty"`
	MessageStop                 *bedrockConverseMessageStopEvent       `json:"messageStop,omitempty"`
	Metadata                    *bedrockConverseMetadataEvent          `json:"metadata,omitempty"`
	InternalServerException     *struct{ Message string }              `json:"internalServerException,omitempty"`
	ModelStreamErrorException   *struct{ Message string }              `json:"modelStreamErrorException,omitempty"`
	ValidationException         *struct{ Message string }              `json:"validationException,omitempty"`
	ThrottlingException         *struct{ Message string }              `json:"throttlingException,omitempty"`
	ServiceUnavailableException *struct{ Message string }              `json:"serviceUnavailableException,omitempty"`
}

type bedrockConverseParserState struct {
	tools               map[string]*utils.PendingTool
	pendingFinish       *bedrockConversePendingFinish
	hasToolCalls        bool
	lifecycle           utils.LifecycleState
	reasoningSignatures map[int]string
}
type bedrockConversePendingFinish struct {
	Reason schema.FinishReason
	Usage  *schema.Usage
}

func bedrockConverseLowerToolSpec(td schema.ToolDefinition, inputSchema any) bedrockConverseToolSpec {
	var spec bedrockConverseToolSpec
	spec.ToolSpec.Name = td.Name
	spec.ToolSpec.Description = td.Description
	spec.ToolSpec.InputSchema.JSON = inputSchema
	return spec
}

func bedrockConverseLowerTools(compatibility string, bp *utils.Breakpoints, tools []schema.ToolDefinition) []any {
	var result []any
	for _, t := range tools {
		result = append(result, bedrockConverseLowerToolSpec(t, projectToolSchema(t.InputSchema, compatibility)))
		if cp := utils.BedrockBlock(bp, t.Cache); cp != nil {
			result = append(result, *cp)
		}
	}
	return result
}

func bedrockConverseTextWithCache(bp *utils.Breakpoints, text string, cache *schema.CacheHint) []any {
	blocks := []any{bedrockConverseTextBlock{Text: text}}
	if cp := utils.BedrockBlock(bp, cache); cp != nil {
		blocks = append(blocks, *cp)
	}
	return blocks
}

func bedrockConverseLowerToolChoice(tc schema.ToolChoice) (any, error) {
	return MatchToolChoice("Bedrock Converse", tc,
		map[string]any{"auto": struct{}{}},
		nil,
		map[string]any{"any": struct{}{}},
		func(name string) any { return map[string]any{"tool": map[string]any{"name": name}} },
	)
}

func bedrockConverseFinishReason(reason string) schema.FinishReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return schema.ReasonStop
	case "max_tokens":
		return schema.ReasonLength
	case "tool_use":
		return schema.ReasonToolCalls
	case "content_filtered", "guardrail_intervened":
		return schema.ReasonContentFilter
	}
	return schema.ReasonUnknown
}

func bedrockConverseMapUsage(u *bedrockConverseUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	cacheTotal := intVal(u.CacheReadInputTokens) + intVal(u.CacheWriteInputTokens)
	nonCached := intVal(u.InputTokens) - cacheTotal
	if nonCached < 0 {
		nonCached = 0
	}
	return &schema.Usage{
		InputTokens:           intVal(u.InputTokens),
		OutputTokens:          intVal(u.OutputTokens),
		NonCachedInputTokens:  nonCached,
		CacheReadInputTokens:  intVal(u.CacheReadInputTokens),
		CacheWriteInputTokens: intVal(u.CacheWriteInputTokens),
		TotalTokens:           intVal(TotalTokens(u.InputTokens, u.OutputTokens, u.TotalTokens)),
		ProviderMetadata:      u.ProviderMetadata,
	}
}

func bedrockConverseLowerToolResultContent(part schema.ToolResultPart) ([]bedrockConverseToolResultContentItem, error) {
	if part.Result.Type == "text" || part.Result.Type == "error" {
		t := ToolResultText(part)
		return []bedrockConverseToolResultContentItem{{Text: &t}}, nil
	}
	if part.Result.Type == "json" {
		return []bedrockConverseToolResultContentItem{{JSON: part.Result.Value}}, nil
	}
	content, ok := part.Result.Value.([]schema.ToolContent)
	if !ok {
		t := ToolResultText(part)
		return []bedrockConverseToolResultContentItem{{Text: &t}}, nil
	}
	items := []bedrockConverseToolResultContentItem{}
	for _, c := range content {
		switch v := c.(type) {
		case schema.ToolTextContent:
			items = append(items, bedrockConverseToolResultContentItem{Text: &v.Text})
		case schema.ToolFileContent:
			img, err := utils.BedrockLower(schema.MediaPart{
				Type: "media", MediaType: v.Mime, Data: v.URI, Filename: v.Name,
			})
			if err != nil {
				return nil, err
			}
			if m, ok := img.(map[string]any); ok && m["image"] != nil {
				items = append(items, bedrockConverseToolResultContentItem{Image: img})
			} else {
				return nil, fmt.Errorf("Bedrock Converse only supports image media in tool results")
			}
		}
	}
	return items, nil
}

func bedrockConverseLowerSystem(bp *utils.Breakpoints, parts []schema.SystemPart) []any {
	var blocks []any
	for _, p := range parts {
		blocks = append(blocks, bedrockConverseTextWithCache(bp, p.Text, p.Cache)...)
	}
	return blocks
}

func bedrockConverseFromRequest(request *schema.LLMRequest) (any, error) {
	gen := request.Generation
	bp := utils.NewBedrockBreakpoints()

	var toolChoice any
	if request.ToolChoice != nil {
		choice, err := bedrockConverseLowerToolChoice(*request.ToolChoice)
		if err != nil {
			return nil, err
		}
		toolChoice = choice
	}
	var tools []any
	if len(request.Tools) > 0 && (request.ToolChoice == nil || request.ToolChoice.Type != "none") {
		tools = bedrockConverseLowerTools(ToolSchemaCompatibility(request.Model), &bp, request.Tools)
	}
	var toolCfg *bedrockConverseToolConfig
	if len(tools) > 0 {
		toolCfg = &bedrockConverseToolConfig{Tools: tools}
		if toolChoice != nil {
			toolCfg.ToolChoice = toolChoice
		}
	}

	sysBlocks := bedrockConverseLowerSystem(&bp, request.System)
	messages, err := bedrockConverseLowerMessages(request, &bp)
	if err != nil {
		return nil, err
	}

	var infCfg *bedrockConverseInferenceConfig
	if gen != nil && (gen.MaxTokens != nil || gen.Temperature != nil || gen.TopP != nil || len(gen.Stop) > 0) {
		infCfg = &bedrockConverseInferenceConfig{
			MaxTokens: gen.MaxTokens, Temperature: gen.Temperature, TopP: gen.TopP, StopSequences: gen.Stop,
		}
	}
	var additional any
	if gen != nil && gen.TopK != nil {
		additional = map[string]any{"top_k": *gen.TopK}
	}

	return bedrockConverseBody{
		ModelID: string(request.Model.ModelID()), Messages: messages,
		System: sysBlocks, InferenceConfig: infCfg, ToolConfig: toolCfg,
		AdditionalModelRequestFields: additional,
	}, nil
}

func bedrockConverseLowerMessages(request *schema.LLMRequest, bp *utils.Breakpoints) ([]bedrockConverseMessage, error) {
	msgs := []bedrockConverseMessage{}
	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			msgs = bedrockConverseLowerSystemMessage(msgs, bp, msg)
		case schema.RoleUser:
			m, err := bedrockConverseLowerUserMessage(bp, msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m)
		case schema.RoleAssistant:
			m, err := bedrockConverseLowerAssistantMessage(bp, msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m)
		case schema.RoleTool:
			m, err := bedrockConverseLowerToolMessage(bp, msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func bedrockConverseLowerSystemMessage(msgs []bedrockConverseMessage, bp *utils.Breakpoints, msg schema.Message) []bedrockConverseMessage {
	update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
	blocks := bedrockConverseTextWithCache(bp, update, SystemUpdateCache(msg.Content))
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
		msgs[len(msgs)-1].Content = append(msgs[len(msgs)-1].Content, blocks...)
		return msgs
	}
	return append(msgs, bedrockConverseMessage{Role: "user", Content: blocks})
}

func bedrockConverseLowerUserMessage(bp *utils.Breakpoints, msg schema.Message) (bedrockConverseMessage, error) {
	var content []any
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			content = append(content, bedrockConverseTextWithCache(bp, part.Text, part.Cache)...)
		case schema.MediaPart:
			media, err := utils.BedrockLower(part)
			if err != nil {
				return bedrockConverseMessage{}, err
			}
			content = append(content, media)
		default:
			return bedrockConverseMessage{}, fmt.Errorf("Bedrock Converse user only supports text and media")
		}
	}
	return bedrockConverseMessage{Role: "user", Content: content}, nil
}

func bedrockConverseLowerAssistantMessage(bp *utils.Breakpoints, msg schema.Message) (bedrockConverseMessage, error) {
	var content []any
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			content = append(content, bedrockConverseTextWithCache(bp, part.Text, part.Cache)...)
		case schema.ReasoningPart:
			content = append(content, bedrockConverseLowerReasoning(part))
		case schema.ToolCallPart:
			var tub bedrockConverseToolUseBlock
			tub.ToolUse.ToolUseID = part.ID
			tub.ToolUse.Name = part.Name
			tub.ToolUse.Input = part.Input
			content = append(content, tub)
		default:
			return bedrockConverseMessage{}, fmt.Errorf("Bedrock Converse assistant only supports text, reasoning, tool-call")
		}
	}
	return bedrockConverseMessage{Role: "assistant", Content: content}, nil
}

func bedrockConverseLowerReasoning(part schema.ReasoningPart) bedrockConverseReasoningBlock {
	var sig *string
	if part.Encrypted != nil {
		sig = part.Encrypted
	} else if md := part.ProviderMetadata; md != nil {
		if raw := md["bedrock"]; raw != nil {
			if s, _ := raw["signature"].(string); s != "" {
				sig = &s
			}
		}
	}
	rb := bedrockConverseReasoningBlock{}
	rb.ReasoningContent.ReasoningText = &struct {
		Text      string  `json:"text"`
		Signature *string `json:"signature,omitempty"`
	}{Text: part.Text, Signature: sig}
	return rb
}

func bedrockConverseLowerToolMessage(bp *utils.Breakpoints, msg schema.Message) (bedrockConverseMessage, error) {
	var content []any
	for _, p := range msg.Content {
		part, ok := p.(schema.ToolResultPart)
		if !ok {
			continue
		}
		c, err := bedrockConverseLowerToolResultContent(part)
		if err != nil {
			return bedrockConverseMessage{}, err
		}
		status := "success"
		if part.Result.Type == "error" {
			status = "error"
		}
		content = append(content, bedrockConverseToolResultBlock{
			ToolResult: struct {
				ToolUseID string                                 `json:"toolUseId"`
				Content   []bedrockConverseToolResultContentItem `json:"content"`
				Status    string                                 `json:"status"`
			}{ToolUseID: part.ID, Content: c, Status: status},
		})
		if cp := utils.BedrockBlock(bp, part.Cache); cp != nil {
			content = append(content, *cp)
		}
	}
	return bedrockConverseMessage{Role: "user", Content: content}, nil
}

func bedrockConverseDecodeEvent(frame string) (any, error) {
	if frame == "" {
		return nil, nil
	}
	var evt bedrockConverseEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("Bedrock Converse: invalid event: %w", err)
	}
	return evt, nil
}

func bedrockConverseStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(bedrockConverseParserState)
	evt, ok := event.(bedrockConverseEvent)
	if !ok {
		return s, nil, nil
	}
	switch {
	case evt.ContentBlockStart != nil && evt.ContentBlockStart.Start != nil && evt.ContentBlockStart.Start.ToolUse != nil:
		return bedrockConverseOnContentBlockStart(s, evt)
	case evt.ContentBlockDelta != nil && evt.ContentBlockDelta.Delta != nil:
		return bedrockConverseOnContentBlockDelta(s, evt)
	case evt.ContentBlockStop != nil:
		return bedrockConverseOnContentBlockStop(s, evt)
	case evt.MessageStop != nil:
		return bedrockConverseOnMessageStop(s, evt)
	case evt.Metadata != nil:
		return bedrockConverseOnMetadata(s, evt)
	case evt.InternalServerException != nil || evt.ModelStreamErrorException != nil || evt.ServiceUnavailableException != nil:
		return s, bedrockConverseRetryableError(evt), nil
	case evt.ValidationException != nil || evt.ThrottlingException != nil:
		return s, bedrockConverseTerminalError(evt), nil
	}
	return s, nil, nil
}

func bedrockConverseOnContentBlockStart(s bedrockConverseParserState, evt bedrockConverseEvent) (any, []schema.LLMEvent, error) {
	idx := evt.ContentBlockStart.ContentBlockIndex
	tu := evt.ContentBlockStart.Start.ToolUse
	var events []schema.LLMEvent
	utils.StepStart(&s.lifecycle, &events)
	s.tools = utils.ToolStreamStart(s.tools, fmt.Sprint(idx), utils.PendingTool{ID: tu.ToolUseID, Name: tu.Name})
	events = append(events, schema.NewToolInputStart(tu.ToolUseID, tu.Name))
	return s, events, nil
}

func bedrockConverseOnContentBlockDelta(s bedrockConverseParserState, evt bedrockConverseEvent) (any, []schema.LLMEvent, error) {
	d := evt.ContentBlockDelta.Delta
	idx := evt.ContentBlockDelta.ContentBlockIndex
	switch {
	case d.Text != nil && *d.Text != "":
		var events []schema.LLMEvent
		utils.TextDelta(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx), *d.Text)
		return s, events, nil
	case d.ReasoningContent != nil:
		return bedrockConverseOnReasoningDelta(s, idx, d.ReasoningContent.Text, d.ReasoningContent.Signature)
	case d.ToolUse != nil:
		return bedrockConverseOnToolUseDelta(s, idx, d.ToolUse.Input)
	}
	return s, nil, nil
}

func bedrockConverseOnReasoningDelta(s bedrockConverseParserState, idx int, text, signature *string) (any, []schema.LLMEvent, error) {
	var events []schema.LLMEvent
	if text != nil && *text != "" {
		utils.ReasoningDelta(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), *text)
	}
	if signature != nil {
		if s.reasoningSignatures == nil {
			s.reasoningSignatures = make(map[int]string)
		}
		s.reasoningSignatures[idx] = *signature
	}
	return s, events, nil
}

func bedrockConverseOnToolUseDelta(s bedrockConverseParserState, idx int, input string) (any, []schema.LLMEvent, error) {
	result, err := utils.AppendExisting(bedrockConverseAdapter, s.tools, fmt.Sprint(idx), input,
		"Bedrock Converse tool delta is missing its tool call")
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

func bedrockConverseOnContentBlockStop(s bedrockConverseParserState, evt bedrockConverseEvent) (any, []schema.LLMEvent, error) {
	idx := evt.ContentBlockStop.ContentBlockIndex
	result, err := utils.FinishTool(bedrockConverseAdapter, s.tools, fmt.Sprint(idx))
	if err != nil {
		return s, nil, err
	}
	var events []schema.LLMEvent
	if len(result.Events) > 0 {
		utils.StepStart(&s.lifecycle, &events)
	} else {
		utils.TextEnd(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx))
		utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), s.reasoningSignature(idx))
	}
	events = append(events, result.Events...)
	for _, e := range result.Events {
		if e.IsToolCall() {
			s.hasToolCalls = true
		}
	}
	if s.reasoningSignatures != nil {
		delete(s.reasoningSignatures, idx)
	}
	s.tools = result.Tools
	return s, events, nil
}

func (s bedrockConverseParserState) reasoningSignature(idx int) schema.ProviderMetadata {
	if s.reasoningSignatures == nil {
		return nil
	}
	sig, ok := s.reasoningSignatures[idx]
	if !ok {
		return nil
	}
	return providerMetadata("bedrock", map[string]any{"signature": sig})
}

func bedrockConverseOnMessageStop(s bedrockConverseParserState, evt bedrockConverseEvent) (any, []schema.LLMEvent, error) {
	pending := &bedrockConversePendingFinish{Reason: bedrockConverseFinishReason(evt.MessageStop.StopReason)}
	if s.pendingFinish != nil {
		pending.Usage = s.pendingFinish.Usage
	}
	s.pendingFinish = pending
	return s, nil, nil
}

func bedrockConverseOnMetadata(s bedrockConverseParserState, evt bedrockConverseEvent) (any, []schema.LLMEvent, error) {
	usage := bedrockConverseMapUsage(evt.Metadata.Usage)
	reason := schema.ReasonStop
	if s.pendingFinish != nil {
		reason = s.pendingFinish.Reason
	}
	s.pendingFinish = &bedrockConversePendingFinish{Reason: reason, Usage: usage}
	return s, nil, nil
}

func bedrockConverseRetryableError(evt bedrockConverseEvent) []schema.LLMEvent {
	msg := "Bedrock Converse stream error"
	if evt.InternalServerException != nil {
		msg = evt.InternalServerException.Message
	}
	if evt.ModelStreamErrorException != nil {
		msg = evt.ModelStreamErrorException.Message
	}
	if evt.ServiceUnavailableException != nil {
		msg = evt.ServiceUnavailableException.Message
	}
	return []schema.LLMEvent{schema.NewProviderErrorEvent(msg, schema.WithEventRetryable(true))}
}

func bedrockConverseTerminalError(evt bedrockConverseEvent) []schema.LLMEvent {
	msg := "Bedrock Converse error"
	if evt.ValidationException != nil {
		msg = evt.ValidationException.Message
	}
	if evt.ThrottlingException != nil {
		msg = evt.ThrottlingException.Message
	}
	opts := []schema.EventOption{}
	if evt.ValidationException != nil && schema.IsContextOverflow(msg) {
		opts = append(opts, schema.WithEventClassification(schema.ClassificationContextOverflow))
	}
	if evt.ThrottlingException != nil {
		opts = append(opts, schema.WithEventRetryable(true))
	}
	return []schema.LLMEvent{schema.NewProviderErrorEvent(msg, opts...)}
}

func bedrockConverseOnHalt(state any) []schema.LLMEvent {
	s := state.(bedrockConverseParserState)
	if s.pendingFinish == nil {
		return nil
	}
	var events []schema.LLMEvent
	reason := s.pendingFinish.Reason
	if reason == schema.ReasonStop && s.hasToolCalls {
		reason = schema.ReasonToolCalls
	}
	utils.LifecycleFinish(&s.lifecycle, &events, utils.FinishInput{Reason: reason, Usage: s.pendingFinish.Usage})
	return events
}

// BedrockConverseProtocol is the Bedrock Converse wire protocol.
var BedrockConverseProtocol = route.Protocol{
	ID:   bedrockConverseAdapter,
	Body: route.ProtocolBody{Schema: bedrockConverseBodySchema, From: bedrockConverseFromRequest},
	Stream: route.ProtocolStream{
		DecodeEvent: bedrockConverseDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return bedrockConverseParserState{tools: utils.ToolStreamEmpty(), lifecycle: utils.LifecycleInitial()}
		},
		Step: bedrockConverseStep, OnHalt: bedrockConverseOnHalt,
	},
}

type bedrockConverseTransport struct{}

// bedrockConversePrepared is a prepared Bedrock request.
type bedrockConversePrepared struct {
	URL     string
	Method  string
	Body    string
	Headers map[string]string
}

// ID identifies the Bedrock transport.
func (bedrockConverseTransport) ID() string { return "bedrock-converse" }

// Prepare builds the Bedrock request for one call.
func (bedrockConverseTransport) Prepare(input route.TransportPrepareInput) (any, error) {
	body, ok := input.Body.(bedrockConverseBody)
	if !ok {
		return nil, fmt.Errorf("Bedrock Converse: unexpected body type %T", input.Body)
	}
	encodedModelID := url.PathEscape(body.ModelID)
	path := fmt.Sprintf("/model/%s/converse-stream", encodedModelID)
	u := input.Endpoint.BaseURL + path
	bodyText, err := json.Marshal(input.Body)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string)
	headers["content-type"] = "application/json"
	if input.Headers != nil {
		for k, v := range input.Headers(input.Request) {
			headers[k] = v
		}
	}
	if input.Auth != nil {
		authHdrs, err := input.Auth.Apply(route.AuthInput{
			Request: input.Request, URL: u,
			Body: string(bodyText), Headers: headers,
		})
		if err != nil {
			return nil, err
		}
		headers = authHdrs
	}
	return bedrockConversePrepared{URL: u, Method: "POST", Body: string(bodyText), Headers: headers}, nil
}

// Frames sends the prepared request and yields its event-stream frames.
func (bedrockConverseTransport) Frames(ctx context.Context, prepared any, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
	p := prepared.(bedrockConversePrepared)
	return func(yield func(any, error) bool) {
		req, err := http.NewRequestWithContext(ctx, "POST", p.URL, strings.NewReader(p.Body))
		if err != nil {
			yield(nil, err)
			return
		}
		for k, v := range p.Headers {
			req.Header.Set(k, v)
		}
		resp, llmErr := runtime.HTTP.DoWithRetries(req)
		if llmErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				yield(nil, ctxErr)
				return
			}
			yield(nil, llmErr)
			return
		}
		var fb BedrockFrameBuffer
		tmp := make([]byte, 16384)
		for {
			n, readErr := resp.Body.Read(tmp)
			if n > 0 {
				decoded, decodeErr := DecodeBedrockEventStreamFrames(&fb, tmp[:n])
				if decodeErr != nil {
					yield(nil, decodeErr)
					resp.Body.Close()
					return
				}
				for _, d := range decoded {
					frame, wrapErr := RewrapBedrockFrame(d)
					if wrapErr != nil {
						yield(nil, wrapErr)
						resp.Body.Close()
						return
					}
					if !yield(frame, nil) {
						resp.Body.Close()
						return
					}
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					decoded, decodeErr := DecodeBedrockEventStreamFrames(&fb, nil)
					if decodeErr != nil {
						yield(nil, decodeErr)
						break
					}
					for _, d := range decoded {
						frame, wrapErr := RewrapBedrockFrame(d)
						if wrapErr != nil {
							yield(nil, wrapErr)
							break
						}
						if !yield(frame, nil) {
							break
						}
					}
				}
				break
			}
		}
		resp.Body.Close()
	}
}

// BedrockConverseRoute builds the Bedrock Converse route for a protocol.
func BedrockConverseRoute(protocol route.Protocol) route.Route {
	return route.MakeFromTransport(route.MakeTransportInput{
		ID:        bedrockConverseAdapter,
		Provider:  "bedrock",
		Protocol:  protocol,
		Endpoint:  route.NewStaticPath(""),
		Transport: bedrockConverseTransport{},
	})
}
