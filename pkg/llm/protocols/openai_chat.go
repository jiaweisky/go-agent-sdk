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

const openaiChatAdapter = "openai-chat"

// OpenAIChatDefaultBaseURL is the OpenAI API base URL.
const OpenAIChatDefaultBaseURL = "https://api.openai.com/v1"

// OpenAIChatPath is the Chat Completions endpoint path.
const OpenAIChatPath = "/chat/completions"

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
	Type       string                `json:"type"`
	Text       string                `json:"text,omitempty"`
	ImageURL   *openaiChatImageURL   `json:"image_url,omitempty"`
	InputAudio *openaiChatInputAudio `json:"input_audio,omitempty"`
	AudioURL   *openaiChatAudioURL   `json:"audio_url,omitempty"`
	VideoURL   *openaiChatVideoURL   `json:"video_url,omitempty"`
}
type openaiChatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}
type openaiChatInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}
type openaiChatAudioURL struct {
	URL string `json:"url"`
}
type openaiChatVideoURL struct {
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

// UnmarshalJSON reads the usage payload OpenAI sends for chat completions.
func (u *openaiChatUsage) UnmarshalJSON(data []byte) error {
	type plain openaiChatUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = openaiChatUsage(decoded)
	u.ProviderMetadata = providerMetadata("openai", rawRecord(data))
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
	Modalities       []string `json:"modalities,omitempty"`
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
	lifecycle      utils.LifecycleState
}

func openaiChatLowerTool(tool schema.ToolDefinition, inputSchema any) openaiChatTool {
	return openaiChatTool{
		Type: "function",
		Function: openaiChatFunction{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  toolParameters(inputSchema),
		},
	}
}

func openaiChatLowerToolChoice(tc schema.ToolChoice) (any, error) {
	return MatchToolChoice("OpenAI Chat", tc,
		"auto",
		"none",
		"required",
		func(name string) any {
			return openaiChatToolChoice{Type: "function", Function: &openaiChatToolChoiceFunc{Name: name}}
		},
	)
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

func openaiChatLowerMedia(part schema.MediaPart, profile MediaEncodingProfile) (*openaiChatUserContent, error) {
	source := part.Source
	if source == "" {
		source = schema.MediaSourceInline
	}
	kind, ok := profile.blockFor(part.MediaType, source)
	if !ok {
		return nil, InvalidRequest(fmt.Sprintf("OpenAI Chat cannot carry %s media (%s)", part.MediaType, source))
	}
	// URL source: pass the http(s) URL through unchanged.
	if source == schema.MediaSourceURL {
		url, _ := part.Data.(string)
		if strings.TrimSpace(url) == "" {
			return nil, InvalidRequest("OpenAI Chat media URL is empty")
		}
		switch kind {
		case ImageURL:
			return &openaiChatUserContent{Type: "image_url", ImageURL: &openaiChatImageURL{URL: url, Detail: part.Detail}}, nil
		case AudioURLHTTP:
			return &openaiChatUserContent{Type: "audio_url", AudioURL: &openaiChatAudioURL{URL: url}}, nil
		case VideoURLHTTP:
			return &openaiChatUserContent{Type: "video_url", VideoURL: &openaiChatVideoURL{URL: url}}, nil
		}
	}
	media, err := utils.ValidateMedia("OpenAI Chat", part, kind.mimeSet())
	if err != nil {
		return nil, err
	}
	switch kind {
	case ImageInlineDataURL:
		return &openaiChatUserContent{Type: "image_url", ImageURL: &openaiChatImageURL{URL: media.DataURL, Detail: part.Detail}}, nil
	case InputAudioBase64:
		return &openaiChatUserContent{Type: "input_audio", InputAudio: &openaiChatInputAudio{Data: media.Base64, Format: audioFormat(media.MIME)}}, nil
	case AudioURLDataURL:
		return &openaiChatUserContent{Type: "audio_url", AudioURL: &openaiChatAudioURL{URL: media.DataURL}}, nil
	case VideoURLDataURL:
		return &openaiChatUserContent{Type: "video_url", VideoURL: &openaiChatVideoURL{URL: media.DataURL}}, nil
	}
	return nil, InvalidRequest(fmt.Sprintf("OpenAI Chat has no encoder for %s", kind))
}

func openaiChatLowerUserMessage(msg schema.Message, profile MediaEncodingProfile) (any, error) {
	var parts []openaiChatUserContent
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			parts = append(parts, openaiChatUserContent{Type: "text", Text: part.Text})
		case schema.MediaPart:
			m, err := openaiChatLowerMedia(part, profile)
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
		m.Content = strings.Join(texts, "\n")
	}
	if len(toolCalls) > 0 {
		m.ToolCalls = toolCalls
	}
	if len(reasonings) > 0 {
		m.ReasoningContent = strings.Join(reasonings, "")
	} else {
		m.ReasoningContent = openaiChatCompatibleReasoningContent(msg.Native)
	}
	return m, nil
}

// openaiChatCompatibleReasoningContent recovers the reasoning text an
// OpenAI-compatible provider stored on the message's native payload.
func openaiChatCompatibleReasoningContent(native map[string]any) string {
	compatible, ok := native["openaiCompatible"].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := compatible["reasoning_content"].(string)
	return value
}

func openaiChatLowerToolMessage(part schema.ToolResultPart) openaiChatMessage {
	return openaiChatMessage{
		Role:       "tool",
		ToolCallID: part.ID,
		Content:    ToolResultText(part),
	}
}

func openaiChatFromRequest(request *schema.LLMRequest, profile MediaEncodingProfile) (any, error) {
	messages, err := openaiChatLowerMessages(request, profile)
	if err != nil {
		return nil, err
	}
	body := openaiChatBody{
		Model:         string(request.Model.ModelID()),
		Messages:      messages,
		Stream:        true,
		StreamOptions: map[string]any{"include_usage": true},
	}
	if profile.has(InputAudioBase64) && requestHasModality(request, "audio") {
		body.Modalities = []string{"text", "audio"}
	}
	if len(request.Tools) > 0 {
		tools := make([]openaiChatTool, 0, len(request.Tools))
		for _, t := range request.Tools {
			tools = append(tools, openaiChatLowerTool(t, ProjectToolSchema(request.Model, t.InputSchema)))
		}
		body.Tools = tools
	}
	if request.ToolChoice != nil {
		choice, err := openaiChatLowerToolChoice(*request.ToolChoice)
		if err != nil {
			return nil, err
		}
		body.ToolChoice = choice
	}
	if raw := request.ProviderOptions["openai"]; len(raw) > 0 {
		if s, ok := raw["store"].(bool); ok {
			body.Store = &s
		}
	}
	if effort := utils.ReasoningEffort(request); effort != nil {
		if !utils.IsReasoningEffort(*effort) {
			return nil, InvalidRequest(fmt.Sprintf("OpenAI Chat does not support reasoning effort %s", *effort))
		}
		body.ReasoningEffort = effort
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

func openaiChatLowerMessages(request *schema.LLMRequest, profile MediaEncodingProfile) ([]openaiChatMessage, error) {
	msgs := []openaiChatMessage{}
	if len(request.System) > 0 {
		parts := make([]struct{ Text string }, len(request.System))
		for i, p := range request.System {
			parts[i].Text = p.Text
		}
		sysText := JoinText(parts)
		msgs = append(msgs, openaiChatMessage{Role: "system", Content: sysText})
	}

	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
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
			m, err := openaiChatLowerUserMessage(msg, profile)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, m.(openaiChatMessage))
		case schema.RoleAssistant:
			m, err := openaiChatLowerAssistantMessage(msg)
			if err != nil {
				return nil, err
			}
			// OpenAI Chat requires an assistant message to set content or
			// tool_calls. A reasoning-only message (for example a turn
			// interrupted while the model was still thinking) lowers to a null
			// content, which providers reject, so drop it.
			if m.Content == nil && len(m.ToolCalls) == 0 {
				continue
			}
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func openaiChatDecodeEvent(frame string) (any, error) {
	frame = strings.TrimSpace(frame)
	if frame == "" || frame == "[DONE]" {
		return nil, nil
	}
	// Defensive: some proxies occasionally let an unstripped SSE prefix through
	// to the decoder. Tolerate a leading "data:" and retry.
	if rest, ok := strings.CutPrefix(frame, "data:"); ok {
		if inner := strings.TrimSpace(rest); strings.HasPrefix(inner, "{") || strings.HasPrefix(inner, "[") {
			frame = inner
		}
	}
	var evt openaiChatEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("OpenAI Chat: invalid stream event: %s: %w", frame, err)
	}
	return evt, nil
}

func openaiChatMapFinishReason(reason *string) schema.FinishReason {
	if reason == nil {
		return schema.ReasonUnknown
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
	nonCached := intVal(usage.PromptTokens) - cached
	if nonCached < 0 {
		nonCached = 0
	}
	return &schema.Usage{
		InputTokens:          intVal(usage.PromptTokens),
		OutputTokens:         intVal(usage.CompletionTokens),
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      reasoning,
		TotalTokens:          intVal(TotalTokens(usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)),
		ProviderMetadata:     usage.ProviderMetadata,
	}
}

func openaiChatStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(openaiChatParserState)
	evt, ok := event.(openaiChatEvent)
	if !ok {
		return s, nil, nil
	}
	var events []schema.LLMEvent
	if u := openaiChatMapUsage(evt.Usage); u != nil {
		s.usage = u
	}
	if len(evt.Choices) == 0 {
		return s, events, nil
	}
	choice := evt.Choices[0]
	prevFinishReason := s.finishReason
	if choice.FinishReason != nil {
		s.finishReason = openaiChatMapFinishReason(choice.FinishReason)
	}
	if choice.Delta != nil {
		deltaEvents, err := openaiChatApplyDelta(&s, choice.Delta)
		if err != nil {
			return s, nil, err
		}
		events = append(events, deltaEvents...)
	}
	if choice.FinishReason != nil && prevFinishReason == "" && len(s.tools) > 0 {
		outcome, err := utils.FinishAllTools(openaiChatAdapter, s.tools)
		if err != nil {
			return s, nil, err
		}
		s.tools = outcome.Tools
		s.toolCallEvents = outcome.Events
	}
	return s, events, nil
}

func openaiChatApplyDelta(s *openaiChatParserState, delta *openaiChatDelta) ([]schema.LLMEvent, error) {
	var events []schema.LLMEvent
	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		utils.ReasoningDelta(&s.lifecycle, &events, "reasoning-0", *delta.ReasoningContent)
	}
	if delta.Content != nil && *delta.Content != "" {
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
			return nil, err
		}
		s.tools = result.Tools
		if len(result.Events) > 0 {
			utils.StepStart(&s.lifecycle, &events)
		}
		events = append(events, result.Events...)
	}
	return events, nil
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
		utils.LifecycleFinish(&s.lifecycle, &events, utils.FinishInput{Reason: reason, Usage: s.usage})
	}
	return events
}

// requestHasModality reports whether any message carries media of a modality.
func requestHasModality(request *schema.LLMRequest, modality string) bool {
	for _, msg := range request.Messages {
		for _, part := range msg.Content {
			if media, ok := part.(schema.MediaPart); ok && utils.MediaModality(media.MediaType) == modality {
				return true
			}
		}
	}
	return false
}

// openAIChatProtocolWith builds the OpenAI Chat protocol parameterized by the
// block kinds its wire may emit. Body lowering and the registry
// descriptor both derive from the same profile.
func openAIChatProtocolWith(profile MediaEncodingProfile) route.Protocol {
	return route.Protocol{
		ID: openaiChatAdapter,
		Body: route.ProtocolBody{
			Schema: openaiChatBodySchema,
			From: func(request *schema.LLMRequest) (any, error) {
				return openaiChatFromRequest(request, profile)
			},
		},
		Stream: route.ProtocolStream{
			DecodeEvent: openaiChatDecodeEvent,
			Initial: func(request *schema.LLMRequest) any {
				return openaiChatParserState{
					tools:     utils.ToolStreamEmpty(),
					lifecycle: utils.LifecycleInitial(),
				}
			},
			Step:   openaiChatStep,
			OnHalt: openaiChatFinish,
		},
	}
}

// OpenAIChatProtocol is the OpenAI Chat Completions wire protocol.
var OpenAIChatProtocol = openAIChatProtocolWith(openAIChatProfile)

// OpenAICompatibleChatProtocol is the OpenAI-compatible wire protocol with the
// vLLM video/audio extension blocks.
var OpenAICompatibleChatProtocol = openAIChatProtocolWith(openAICompatibleProfile)

// OpenAIChatHTTPRoute is the OpenAI Chat Completions route.
var OpenAIChatHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        openaiChatAdapter,
	Provider:  "openai",
	Protocol:  OpenAIChatProtocol,
	Endpoint:  route.NewStaticPath(OpenAIChatPath, route.WithBaseURL(OpenAIChatDefaultBaseURL)),
	Transport: transport.SSEJson(),
})
