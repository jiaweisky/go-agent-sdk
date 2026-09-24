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

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const bedrockConverseAdapter = "bedrock-converse"
const bedrockConverseBedrockBreakpointCap = 4

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
type bedrockConverseBedrockMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}
type bedrockConverseSystemBlock struct {
	Text string `json:"text"`
}
type bedrockConverseBedrockToolSpec struct {
	ToolSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			JSON any `json:"json"`
		} `json:"inputSchema"`
	} `json:"toolSpec"`
}
type bedrockConverseBedrockBody struct {
	ModelID                      string                          `json:"modelId"`
	Messages                     []bedrockConverseBedrockMessage `json:"messages"`
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

type bedrockConverseBedrockUsage struct {
	InputTokens           *int                    `json:"inputTokens,omitempty"`
	OutputTokens          *int                    `json:"outputTokens,omitempty"`
	TotalTokens           *int                    `json:"totalTokens,omitempty"`
	CacheReadInputTokens  *int                    `json:"cacheReadInputTokens,omitempty"`
	CacheWriteInputTokens *int                    `json:"cacheWriteInputTokens,omitempty"`
	ProviderMetadata      schema.ProviderMetadata `json:"-"`
}

func (u *bedrockConverseBedrockUsage) UnmarshalJSON(data []byte) error {
	type plain bedrockConverseBedrockUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = bedrockConverseBedrockUsage(decoded)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		u.ProviderMetadata = schema.ProviderMetadata{"bedrock": raw}
	}
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
	Usage   *bedrockConverseBedrockUsage `json:"usage,omitempty"`
	Metrics any                          `json:"metrics,omitempty"`
}

type bedrockConverseBedrockEvent struct {
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
	lifecycle           utils.State
	reasoningSignatures map[int]string
}
type bedrockConversePendingFinish struct {
	Reason schema.FinishReason
	Usage  *schema.Usage
}

func bedrockConverseIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func bedrockConverseBedrockMetadata(m map[string]any) schema.ProviderMetadata {
	return schema.ProviderMetadata{"bedrock": m}
}

func bedrockConverseLowerToolSpec(td schema.ToolDefinition, inputSchema any) bedrockConverseBedrockToolSpec {
	var spec bedrockConverseBedrockToolSpec
	spec.ToolSpec.Name = td.Name
	spec.ToolSpec.Description = td.Description
	spec.ToolSpec.InputSchema.JSON = inputSchema
	return spec
}

func bedrockConverseLowerTools(compatibility string, bp *utils.Breakpoints, tools []schema.ToolDefinition) []any {
	var result []any
	for _, t := range tools {
		result = append(result, bedrockConverseLowerToolSpec(t, projectToolSchema(t.InputSchema, compatibility)))
		if cp := utils.Block(bp, t.Cache); cp != nil {
			result = append(result, *cp)
		}
	}
	return result
}

func bedrockConverseTextWithCache(bp *utils.Breakpoints, text string, cache *schema.CacheHint) []any {
	blocks := []any{bedrockConverseTextBlock{Text: text}}
	if cp := utils.Block(bp, cache); cp != nil {
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

func bedrockConverseBedrockFinishReason(reason string) schema.FinishReason {
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

func bedrockConverseMapUsage(u *bedrockConverseBedrockUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	cacheTotal := bedrockConverseIntVal(u.CacheReadInputTokens) + bedrockConverseIntVal(u.CacheWriteInputTokens)
	nonCached := bedrockConverseIntVal(u.InputTokens) - cacheTotal
	if nonCached < 0 {
		nonCached = 0
	}
	return &schema.Usage{
		InputTokens:           bedrockConverseIntVal(u.InputTokens),
		OutputTokens:          bedrockConverseIntVal(u.OutputTokens),
		NonCachedInputTokens:  nonCached,
		CacheReadInputTokens:  bedrockConverseIntVal(u.CacheReadInputTokens),
		CacheWriteInputTokens: bedrockConverseIntVal(u.CacheWriteInputTokens),
		TotalTokens:           bedrockConverseIntVal(TotalTokens(u.InputTokens, u.OutputTokens, u.TotalTokens)),
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
			img, err := utils.Lower(schema.MediaPart{
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

	return bedrockConverseBedrockBody{
		ModelID: string(request.Model.ModelID()), Messages: messages,
		System: sysBlocks, InferenceConfig: infCfg, ToolConfig: toolCfg,
		AdditionalModelRequestFields: additional,
	}, nil
}

func bedrockConverseLowerMessages(request *schema.LLMRequest, bp *utils.Breakpoints) ([]bedrockConverseBedrockMessage, error) {
	var msgs []bedrockConverseBedrockMessage
	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
			blocks := bedrockConverseTextWithCache(bp, update, nil)
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
				last := &msgs[len(msgs)-1]
				last.Content = append(last.Content, blocks...)
			} else {
				msgs = append(msgs, bedrockConverseBedrockMessage{Role: "user", Content: blocks})
			}
		case schema.RoleUser:
			var content []any
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					content = append(content, bedrockConverseTextWithCache(bp, part.Text, part.Cache)...)
				case schema.MediaPart:
					media, err := utils.Lower(part)
					if err != nil {
						return nil, err
					}
					content = append(content, media)
				default:
					return nil, fmt.Errorf("Bedrock Converse user only supports text and media")
				}
			}
			msgs = append(msgs, bedrockConverseBedrockMessage{Role: "user", Content: content})
		case schema.RoleAssistant:
			var content []any
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					content = append(content, bedrockConverseTextWithCache(bp, part.Text, part.Cache)...)
				case schema.ReasoningPart:
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
					content = append(content, rb)
				case schema.ToolCallPart:
					var tub bedrockConverseToolUseBlock
					tub.ToolUse.ToolUseID = part.ID
					tub.ToolUse.Name = part.Name
					tub.ToolUse.Input = part.Input
					content = append(content, tub)
				default:
					return nil, fmt.Errorf("Bedrock Converse assistant only supports text, reasoning, tool-call")
				}
			}
			msgs = append(msgs, bedrockConverseBedrockMessage{Role: "assistant", Content: content})
		case schema.RoleTool:
			var content []any
			for _, p := range msg.Content {
				if part, ok := p.(schema.ToolResultPart); ok {
					c, err := bedrockConverseLowerToolResultContent(part)
					if err != nil {
						return nil, err
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
					if cp := utils.Block(bp, part.Cache); cp != nil {
						content = append(content, *cp)
					}
				}
			}
			msgs = append(msgs, bedrockConverseBedrockMessage{Role: "user", Content: content})
		}
	}
	return msgs, nil
}

func bedrockConverseDecodeEvent(frame string) (any, error) {
	if frame == "" {
		return nil, nil
	}
	var evt bedrockConverseBedrockEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("Bedrock Converse: invalid event: %w", err)
	}
	return evt, nil
}

func bedrockConverseStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(bedrockConverseParserState)
	evt, ok := event.(bedrockConverseBedrockEvent)
	if !ok {
		return state, nil, nil
	}

	if evt.ContentBlockStart != nil && evt.ContentBlockStart.Start != nil && evt.ContentBlockStart.Start.ToolUse != nil {
		idx := evt.ContentBlockStart.ContentBlockIndex
		tu := evt.ContentBlockStart.Start.ToolUse
		var events []schema.LLMEvent
		utils.StepStart(&s.lifecycle, &events)
		s.tools = utils.Start(s.tools, fmt.Sprint(idx), utils.PendingTool{ID: tu.ToolUseID, Name: tu.Name})
		events = append(events, schema.NewToolInputStart(tu.ToolUseID, tu.Name))
		return s, events, nil
	}
	if evt.ContentBlockDelta != nil && evt.ContentBlockDelta.Delta != nil {
		d := evt.ContentBlockDelta.Delta
		idx := evt.ContentBlockDelta.ContentBlockIndex
		if d.Text != nil && *d.Text != "" {
			var events []schema.LLMEvent
			utils.TextDelta(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx), *d.Text)
			return s, events, nil
		}
		if d.ReasoningContent != nil {
			var events []schema.LLMEvent
			if d.ReasoningContent.Text != nil && *d.ReasoningContent.Text != "" {
				utils.ReasoningDelta(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), *d.ReasoningContent.Text)
			}
			if d.ReasoningContent.Signature != nil {
				if s.reasoningSignatures == nil {
					s.reasoningSignatures = make(map[int]string)
				}
				s.reasoningSignatures[idx] = *d.ReasoningContent.Signature
			}
			return s, events, nil
		}
		if d.ToolUse != nil {
			result, err := utils.AppendExisting(bedrockConverseAdapter, s.tools, fmt.Sprint(idx), d.ToolUse.Input,
				"Bedrock Converse tool delta is missing its tool call")
			if err != nil {
				return state, nil, err
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
	if evt.ContentBlockStop != nil {
		idx := evt.ContentBlockStop.ContentBlockIndex
		result, err := utils.FinishTool(bedrockConverseAdapter, s.tools, fmt.Sprint(idx))
		if err != nil {
			return state, nil, err
		}
		var events []schema.LLMEvent
		if len(result.Events) > 0 {
			utils.StepStart(&s.lifecycle, &events)
		} else {
			var pm schema.ProviderMetadata
			if s.reasoningSignatures != nil {
				if sig, ok := s.reasoningSignatures[idx]; ok {
					pm = bedrockConverseBedrockMetadata(map[string]any{"signature": sig})
				}
			}
			utils.TextEnd(&s.lifecycle, &events, fmt.Sprintf("text-%d", idx))
			utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("reasoning-%d", idx), pm)
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
	if evt.MessageStop != nil {
		pending := &bedrockConversePendingFinish{Reason: bedrockConverseBedrockFinishReason(evt.MessageStop.StopReason)}
		if s.pendingFinish != nil {
			pending.Usage = s.pendingFinish.Usage
		}
		s.pendingFinish = pending
		return s, nil, nil
	}
	if evt.Metadata != nil {
		usage := bedrockConverseMapUsage(evt.Metadata.Usage)
		reason := schema.ReasonStop
		if s.pendingFinish != nil {
			reason = s.pendingFinish.Reason
		}
		s.pendingFinish = &bedrockConversePendingFinish{Reason: reason, Usage: usage}
		return s, nil, nil
	}
	if evt.InternalServerException != nil || evt.ModelStreamErrorException != nil || evt.ServiceUnavailableException != nil {
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
		return s, []schema.LLMEvent{schema.NewProviderErrorEvent(msg, schema.WithEventRetryable(true))}, nil
	}
	if evt.ValidationException != nil || evt.ThrottlingException != nil {
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
		return s, []schema.LLMEvent{schema.NewProviderErrorEvent(msg, opts...)}, nil
	}
	return s, nil, nil
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
	utils.Finish(&s.lifecycle, &events, struct {
		Reason           schema.FinishReason
		Usage            *schema.Usage
		ProviderMetadata schema.ProviderMetadata
	}{Reason: reason, Usage: s.pendingFinish.Usage})
	return events
}

var BedrockConverseProtocol = route.Protocol{
	ID:   bedrockConverseAdapter,
	Body: route.ProtocolBody{From: bedrockConverseFromRequest},
	Stream: route.ProtocolStream{
		DecodeEvent: bedrockConverseDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return bedrockConverseParserState{tools: utils.Empty(), lifecycle: utils.Initial()}
		},
		Step: bedrockConverseStep, OnHalt: bedrockConverseOnHalt,
	},
}

type bedrockConverseBedrockTransport struct{}

func (bedrockConverseBedrockTransport) ID() string { return "bedrock-converse" }

func (bedrockConverseBedrockTransport) Prepare(input route.TransportPrepareInput) (any, error) {
	body, ok := input.Body.(bedrockConverseBedrockBody)
	if !ok {
		return nil, fmt.Errorf("bedrock converse: unexpected body type %T", input.Body)
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
			Request: input.Request, Method: "POST", URL: u,
			Body: string(bodyText), Headers: headers,
		})
		if err != nil {
			return nil, err
		}
		headers = authHdrs
	}
	return struct {
		URL     string
		Method  string
		Body    string
		Headers map[string]string
	}{URL: u, Method: "POST", Body: string(bodyText), Headers: headers}, nil
}

func (bedrockConverseBedrockTransport) Frames(ctx context.Context, prepared any, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[any, error] {
	p := prepared.(struct {
		URL     string
		Method  string
		Body    string
		Headers map[string]string
	})
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
		var fb FrameBufferState
		tmp := make([]byte, 16384)
		for {
			n, readErr := resp.Body.Read(tmp)
			if n > 0 {
				decoded, decodeErr := DecodeEventStreamFrames(&fb, tmp[:n])
				if decodeErr != nil {
					yield(nil, decodeErr)
					resp.Body.Close()
					return
				}
				for _, d := range decoded {
					frame, wrapErr := RewrapFrame(d)
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
					decoded, decodeErr := DecodeEventStreamFrames(&fb, nil)
					if decodeErr != nil {
						yield(nil, decodeErr)
						break
					}
					for _, d := range decoded {
						frame, wrapErr := RewrapFrame(d)
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

func BedrockConverseRoute(protocol route.Protocol) route.Route {
	return route.MakeFromTransport(route.MakeTransportInput{
		ID:        bedrockConverseAdapter,
		Provider:  "bedrock",
		Protocol:  protocol,
		Endpoint:  route.NewStaticPath(""),
		Transport: bedrockConverseBedrockTransport{},
	})
}
