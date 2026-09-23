package protocols

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
	"github.com/user/go-agent-sdk/llm/schema"
)

const openaiChatAdapter = "openai-chat"
const OpenAIChatDefaultBaseURL = "https://api.openai.com/v1"
const OpenAIChatPath = "/chat/completions"

var openaiChatImageMIMEs = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {},
}

type openaiChatTool struct {
	Type     string             `json:"type"`
	Function openaiChatFunction `json:"function"`
}
type openaiChatFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}
type openaiChatAssistantToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openaiChatToolCallFunction `json:"function"`
}
type openaiChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type openaiChatUserContent struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *openaiChatImageURL `json:"image_url,omitempty"`
}
type openaiChatImageURL struct {
	URL string `json:"url"`
}
type openaiChatMessage struct {
	Role             string                        `json:"role"`
	Content          any                           `json:"content"`
	ToolCallID       string                        `json:"tool_call_id,omitempty"`
	ToolCalls        []openaiChatAssistantToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string                        `json:"reasoning_content,omitempty"`
}
type openaiChatToolChoice struct {
	Type     string                    `json:"type"`
	Function *openaiChatToolChoiceFunc `json:"function,omitempty"`
}
type openaiChatToolChoiceFunc struct {
	Name string `json:"name"`
}
type openaiChatUsage struct {
	PromptTokens            *int                         `json:"prompt_tokens,omitempty"`
	CompletionTokens        *int                         `json:"completion_tokens,omitempty"`
	TotalTokens             *int                         `json:"total_tokens,omitempty"`
	PromptTokensDetails     *openaiChatTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openaiChatCompletionDetails `json:"completion_tokens_details,omitempty"`
	// ProviderMetadata keeps the provider's original usage object so
	// provider-specific billing fields (e.g. DeepSeek's
	// prompt_cache_hit_tokens) survive.
	ProviderMetadata schema.ProviderMetadata `json:"-"`
}

func (u *openaiChatUsage) UnmarshalJSON(data []byte) error {
	type plain openaiChatUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = openaiChatUsage(decoded)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		u.ProviderMetadata = schema.ProviderMetadata{"openai": raw}
	}
	return nil
}

type openaiChatTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}
type openaiChatCompletionDetails struct {
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
}
type openaiChatToolCallDeltaFunction struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}
type openaiChatToolCallDelta struct {
	Index    int                              `json:"index"`
	ID       *string                          `json:"id,omitempty"`
	Function *openaiChatToolCallDeltaFunction `json:"function,omitempty"`
}
type openaiChatDelta struct {
	Content          *string                   `json:"content,omitempty"`
	ReasoningContent *string                   `json:"reasoning_content,omitempty"`
	ToolCalls        []openaiChatToolCallDelta `json:"tool_calls,omitempty"`
}
type openaiChatChoice struct {
	Delta        *openaiChatDelta `json:"delta,omitempty"`
	FinishReason *string          `json:"finish_reason,omitempty"`
}
type openaiChatEvent struct {
	Choices []openaiChatChoice `json:"choices"`
	Usage   *openaiChatUsage   `json:"usage,omitempty"`
}
type openaiChatBody struct {
	Model            string   `json:"model"`
	Messages         any      `json:"messages"`
	Tools            any      `json:"tools,omitempty"`
	ToolChoice       any      `json:"tool_choice,omitempty"`
	Stream           bool     `json:"stream"`
	StreamOptions    any      `json:"stream_options,omitempty"`
	Store            *bool    `json:"store,omitempty"`
	ReasoningEffort  *string  `json:"reasoning_effort,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	Seed             *int     `json:"seed,omitempty"`
	Stop             []string `json:"stop,omitempty"`
}

type openaiChatParserState struct {
	tools          map[string]*utils.PendingTool
	toolCallEvents []schema.LLMEvent
	usage          *schema.Usage
	finishReason   schema.FinishReason
	lifecycle      utils.State
}

func openaiChatLowerTool(tool schema.ToolDefinition, inputSchema any) openaiChatTool {
	s, _ := inputSchema.(map[string]any)
	converted := utils.ModelCompatibility(s, "")
	return openaiChatTool{
		Type: "function",
		Function: openaiChatFunction{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  converted,
		},
	}
}

func openaiChatLowerToolChoice(tc schema.ToolChoice) any {
	if tc.Name != nil {
		return openaiChatToolChoice{
			Type:     "function",
			Function: &openaiChatToolChoiceFunc{Name: *tc.Name},
		}
	}
	return tc.Type
}

func openaiChatLowerToolCall(part schema.ToolCallPart) openaiChatAssistantToolCall {
	b, _ := json.Marshal(part.Input)
	return openaiChatAssistantToolCall{
		ID:   part.ID,
		Type: "function",
		Function: openaiChatToolCallFunction{
			Name:      part.Name,
			Arguments: string(b),
		},
	}
}

func openaiChatLowerMedia(part schema.MediaPart) (*openaiChatUserContent, error) {
	media, err := utils.ValidateMedia("OpenAI Chat", part, openaiChatImageMIMEs)
	if err != nil {
		return nil, err
	}
	return &openaiChatUserContent{
		Type:     "image_url",
		ImageURL: &openaiChatImageURL{URL: media.DataURL},
	}, nil
}

func openaiChatLowerUserMessage(msg schema.Message) (any, error) {
	var parts []openaiChatUserContent
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			parts = append(parts, openaiChatUserContent{Type: "text", Text: part.Text})
		case schema.MediaPart:
			m, err := openaiChatLowerMedia(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, *m)
		default:
			return nil, fmt.Errorf("OpenAI Chat user messages only support text and media")
		}
	}
	allText := true
	for _, p := range parts {
		if p.Type != "text" {
			allText = false
			break
		}
	}
	if allText {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return openaiChatMessage{Role: "user", Content: b.String()}, nil
	}
	return openaiChatMessage{Role: "user", Content: parts}, nil
}

func openaiChatLowerAssistantMessage(msg schema.Message) (openaiChatMessage, error) {
	var texts, reasonings []string
	var toolCalls []openaiChatAssistantToolCall
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			texts = append(texts, part.Text)
		case schema.ReasoningPart:
			reasonings = append(reasonings, part.Text)
		case schema.ToolCallPart:
			toolCalls = append(toolCalls, openaiChatLowerToolCall(part))
		default:
			return openaiChatMessage{}, fmt.Errorf("OpenAI Chat assistant only supports text, reasoning, tool-call")
		}
	}
	m := openaiChatMessage{Role: "assistant"}
	if len(texts) > 0 {
		m.Content = strings.Join(texts, "")
	}
	if len(toolCalls) > 0 {
		m.ToolCalls = toolCalls
	}
	if len(reasonings) > 0 {
		m.ReasoningContent = strings.Join(reasonings, "")
	}
	return m, nil
}

func openaiChatLowerToolMessage(part schema.ToolResultPart) openaiChatMessage {
	return openaiChatMessage{
		Role:       "tool",
		ToolCallID: part.ID,
		Content:    ToolResultText(part),
	}
}

func openaiChatSystemPartToText(v struct{ Text string }) string { return v.Text }

func openaiChatFromRequest(request *schema.LLMRequest) (any, error) {
	messages, err := openaiChatLowerMessages(request)
	if err != nil {
		return nil, err
	}
	body := openaiChatBody{
		Model:         string(request.Model.ModelID()),
		Messages:      messages,
		Stream:        true,
		StreamOptions: map[string]any{"include_usage": true},
	}
	if len(request.Tools) > 0 {
		tools := make([]openaiChatTool, 0, len(request.Tools))
		for _, t := range request.Tools {
			tools = append(tools, openaiChatLowerTool(t, t.InputSchema))
		}
		body.Tools = tools
	}
	if request.ToolChoice != nil {
		body.ToolChoice = openaiChatLowerToolChoice(*request.ToolChoice)
	}
	if raw := request.ProviderOptions["openai"]; len(raw) > 0 {
		if s, ok := raw["store"].(bool); ok {
			body.Store = &s
		}
		if e, ok := raw["reasoningEffort"].(string); ok {
			body.ReasoningEffort = &e
		}
	}
	if g := request.Generation; g != nil {
		body.MaxTokens = g.MaxTokens
		body.Temperature = g.Temperature
		body.TopP = g.TopP
		body.FrequencyPenalty = g.FrequencyPenalty
		body.PresencePenalty = g.PresencePenalty
		body.Seed = g.Seed
		body.Stop = g.Stop
	}
	return body, nil
}

func openaiChatLowerMessages(request *schema.LLMRequest) ([]openaiChatMessage, error) {
	var msgs []openaiChatMessage
	if len(request.System) > 0 {
		parts := make([]struct{ Text string }, len(request.System))
		for i, p := range request.System {
			parts[i].Text = p.Text
		}
		sysText := JoinText(parts)
		if sysText != "" {
			msgs = append(msgs, openaiChatMessage{Role: "system", Content: sysText})
		}
	}

	var pendingImages []any
	flushImages := func() {
		if len(pendingImages) == 0 {
			return
		}
		content := make([]openaiChatUserContent, len(pendingImages))
		for i, img := range pendingImages {
			content[i] = img.(openaiChatUserContent)
		}
		msgs = append(msgs, openaiChatMessage{Role: "user", Content: content})
		pendingImages = nil
	}
	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
			if len(pendingImages) > 0 {
				content := []openaiChatUserContent{{Type: "text", Text: update}}
				for _, img := range pendingImages {
					content = append(content, img.(openaiChatUserContent))
				}
				msgs = append(msgs, openaiChatMessage{Role: "user", Content: content})
				pendingImages = nil
				continue
			}
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
				prev := &msgs[len(msgs)-1]
				if s, ok := prev.Content.(string); ok {
					prev.Content = s + "\n" + update
				} else if arr, ok := prev.Content.([]openaiChatUserContent); ok {
					prev.Content = append(arr, openaiChatUserContent{Type: "text", Text: update})
				}
			} else {
				msgs = append(msgs, openaiChatMessage{Role: "user", Content: update})
			}
		case schema.RoleTool:
			for _, p := range msg.Content {
				if part, ok := p.(schema.ToolResultPart); ok {
					msgs = append(msgs, openaiChatLowerToolMessage(part))
				}
			}
		case schema.RoleUser:
			flushImages()
			m, err := openaiChatLowerUserMessage(msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m.(openaiChatMessage))
		case schema.RoleAssistant:
			flushImages()
			m, err := openaiChatLowerAssistantMessage(msg)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m)
		}
	}
	flushImages()
	return msgs, nil
}

func openaiChatDecodeOpenAIChatEvent(frame string) (any, error) {
	if frame == "" || frame == "[DONE]" {
		return nil, nil
	}
	var evt openaiChatEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("OpenAI Chat: invalid stream event: %s: %w", frame, err)
	}
	return evt, nil
}

func openaiChatMapFinishReason(reason *string) schema.FinishReason {
	if reason == nil {
		return ""
	}
	switch *reason {
	case "stop":
		return schema.ReasonStop
	case "length":
		return schema.ReasonLength
	case "content_filter":
		return schema.ReasonContentFilter
	case "function_call", "tool_calls":
		return schema.ReasonToolCalls
	}
	return schema.ReasonUnknown
}

func openaiChatIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func openaiChatMapUsage(usage *openaiChatUsage) *schema.Usage {
	if usage == nil {
		return nil
	}
	var cached, reasoning int
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cached = *usage.PromptTokensDetails.CachedTokens
	}
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = *usage.CompletionTokensDetails.ReasoningTokens
	}
	nonCached := openaiChatIntVal(usage.PromptTokens)
	if cached > 0 {
		nonCached = openaiChatIntVal(usage.PromptTokens) - cached
	}
	return &schema.Usage{
		InputTokens:          openaiChatIntVal(usage.PromptTokens),
		OutputTokens:         openaiChatIntVal(usage.CompletionTokens),
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      reasoning,
		TotalTokens:          openaiChatIntVal(TotalTokens(usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)),
		ProviderMetadata:     usage.ProviderMetadata,
	}
}

func openaiChatStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(openaiChatParserState)
	evt, ok := event.(openaiChatEvent)
	if !ok {
		return state, nil, nil
	}
	var events []schema.LLMEvent
	u := openaiChatMapUsage(evt.Usage)
	if u != nil {
		s.usage = u
	}
	var finishReason schema.FinishReason
	if len(evt.Choices) > 0 {
		finishReason = openaiChatMapFinishReason(evt.Choices[0].FinishReason)
		if finishReason != "" {
			s.finishReason = finishReason
		}
	}
	if len(evt.Choices) == 0 {
		return s, events, nil
	}
	delta := evt.Choices[0].Delta
	if delta != nil {
		if delta.ReasoningContent != nil {
			utils.ReasoningDelta(&s.lifecycle, &events, "reasoning-0", *delta.ReasoningContent)
		}
		if delta.Content != nil {
			utils.ReasoningEnd(&s.lifecycle, &events, "reasoning-0")
			utils.TextDelta(&s.lifecycle, &events, "text-0", *delta.Content)
		}
		if len(delta.ToolCalls) > 0 {
			utils.ReasoningEnd(&s.lifecycle, &events, "reasoning-0")
		}
		for _, td := range delta.ToolCalls {
			var id, name, text string
			if td.ID != nil {
				id = *td.ID
			}
			if td.Function != nil {
				if td.Function.Name != nil {
					name = *td.Function.Name
				}
				if td.Function.Arguments != nil {
					text = *td.Function.Arguments
				}
			}
			result, err := utils.AppendOrStart(openaiChatAdapter, s.tools, fmt.Sprint(td.Index),
				struct {
					ID   string
					Name string
					Text string
				}{ID: id, Name: name, Text: text},
				"OpenAI Chat tool call delta is missing id or name")
			if err != nil {
				return state, nil, err
			}
			s.tools = result.Tools
			if len(result.Events) > 0 {
				utils.StepStart(&s.lifecycle, &events)
			}
			events = append(events, result.Events...)
		}
	}
	if finishReason != "" && len(s.tools) > 0 {
		outcome, finishErr := utils.FinishAllTools(openaiChatAdapter, s.tools)
		if finishErr != nil {
			return state, nil, finishErr
		}
		s.tools = outcome.Tools
		s.toolCallEvents = outcome.Events
	}
	return s, events, nil
}

func openaiChatFinish(state any) []schema.LLMEvent {
	s := state.(openaiChatParserState)
	var events []schema.LLMEvent
	hasToolCalls := len(s.toolCallEvents) > 0
	reason := s.finishReason
	if reason == schema.ReasonStop && hasToolCalls {
		reason = schema.ReasonToolCalls
	}
	if hasToolCalls {
		utils.StepStart(&s.lifecycle, &events)
	}
	events = append(events, s.toolCallEvents...)
	if reason != "" {
		utils.Finish(&s.lifecycle, &events, struct {
			Reason           schema.FinishReason
			Usage            *schema.Usage
			ProviderMetadata schema.ProviderMetadata
		}{Reason: reason, Usage: s.usage})
	}
	return events
}

var OpenAIChatProtocol = route.Protocol{
	ID: openaiChatAdapter,
	Body: route.ProtocolBody{
		From: openaiChatFromRequest,
	},
	Stream: route.ProtocolStream{
		DecodeEvent: openaiChatDecodeOpenAIChatEvent,
		Initial: func(request *schema.LLMRequest) any {
			return openaiChatParserState{
				tools:     utils.Empty(),
				lifecycle: utils.Initial(),
			}
		},
		Step:   openaiChatStep,
		OnHalt: openaiChatFinish,
	},
}

var OpenAIChatHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        openaiChatAdapter,
	Provider:  "openai",
	Protocol:  OpenAIChatProtocol,
	Endpoint:  route.NewStaticPath(OpenAIChatPath, route.WithBaseURL(OpenAIChatDefaultBaseURL)),
	Transport: transport.SSEJson(),
})
