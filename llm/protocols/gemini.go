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

const geminiAdapter = "gemini"
const GeminiDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

var geminiMediaMIMEs = mediaMIMESet(MediaMIMEs)

type geminiTextPart struct {
	Text             string  `json:"text"`
	Thought          *bool   `json:"thought,omitempty"`
	ThoughtSignature *string `json:"thoughtSignature,omitempty"`
}
type geminiInlineDataPart struct {
	InlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	} `json:"inlineData"`
}
type geminiFunctionCallPart struct {
	FunctionCall struct {
		Name string `json:"name"`
		Args any    `json:"args"`
	} `json:"functionCall"`
	ThoughtSignature *string `json:"thoughtSignature,omitempty"`
}
type geminiFunctionResponsePart struct {
	FunctionResponse struct {
		Name     string `json:"name"`
		Response any    `json:"response"`
	} `json:"functionResponse"`
}

type geminiContent struct {
	Role  string `json:"role"`
	Parts []any  `json:"parts"`
}

type geminiFunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}
type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}
type geminiToolConfig struct {
	FunctionCallingConfig struct {
		Mode                 string   `json:"mode"`
		AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
	} `json:"functionCallingConfig"`
}

type geminiThinkingConfig struct {
	ThinkingBudget  *int  `json:"thinkingBudget,omitempty"`
	IncludeThoughts *bool `json:"includeThoughts,omitempty"`
}
type geminiGenerationConfig struct {
	MaxOutputTokens *int                  `json:"maxOutputTokens,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	TopP            *float64              `json:"topP,omitempty"`
	TopK            *float64              `json:"topK,omitempty"`
	StopSequences   []string              `json:"stopSequences,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiBody struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction any                     `json:"systemInstruction,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiUsage struct {
	CachedContentTokenCount *int                    `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount      *int                    `json:"thoughtsTokenCount,omitempty"`
	PromptTokenCount        *int                    `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    *int                    `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         *int                    `json:"totalTokenCount,omitempty"`
	ProviderMetadata        schema.ProviderMetadata `json:"-"`
}

func (u *geminiUsage) UnmarshalJSON(data []byte) error {
	type plain geminiUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = geminiUsage(decoded)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		u.ProviderMetadata = schema.ProviderMetadata{"google": raw}
	}
	return nil
}

type geminiCandidate struct {
	Content      *geminiContent `json:"content,omitempty"`
	FinishReason *string        `json:"finishReason,omitempty"`
}
type geminiEvent struct {
	Candidates    []geminiCandidate `json:"candidates,omitempty"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiParserState struct {
	finishReason       *string
	hasToolCalls       bool
	nextToolCallID     int
	usage              *schema.Usage
	lifecycle          utils.State
	reasoningSignature *string
}

func geminiIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func geminiGoogleMetadata(m map[string]any) schema.ProviderMetadata {
	return schema.ProviderMetadata{"google": m}
}

func geminiThoughtSignature(md schema.ProviderMetadata) *string {
	if m := md["google"]; len(m) > 0 {
		if s, _ := m["thoughtSignature"].(string); s != "" {
			return &s
		}
	}
	return nil
}

func geminiLowerTool(td schema.ToolDefinition, inputSchema any) geminiFunctionDeclaration {
	return geminiFunctionDeclaration{
		Name:        td.Name,
		Description: td.Description,
		Parameters:  utils.Convert(schemaMap(inputSchema)),
	}
}

func geminiLowerToolConfig(tc schema.ToolChoice) (*geminiToolConfig, error) {
	cfg := &geminiToolConfig{}
	switch tc.Type {
	case "auto":
		cfg.FunctionCallingConfig.Mode = "AUTO"
	case "none":
		cfg.FunctionCallingConfig.Mode = "NONE"
	case "required":
		cfg.FunctionCallingConfig.Mode = "ANY"
	default:
		if tc.Name == nil || *tc.Name == "" {
			return nil, InvalidRequest("Gemini tool choice requires a tool name")
		}
		cfg.FunctionCallingConfig.Mode = "ANY"
		cfg.FunctionCallingConfig.AllowedFunctionNames = []string{*tc.Name}
	}
	return cfg, nil
}

func geminiLowerUserPart(part schema.ContentPart) (any, error) {
	switch p := part.(type) {
	case schema.TextPart:
		return geminiTextPart{Text: p.Text}, nil
	case schema.MediaPart:
		media, err := utils.ValidateMedia("Gemini", p, geminiMediaMIMEs)
		if err != nil {
			return nil, err
		}
		var ip geminiInlineDataPart
		ip.InlineData.MimeType = media.MIME
		ip.InlineData.Data = media.Base64
		return ip, nil
	}
	return nil, fmt.Errorf("Gemini user parts only support text and media")
}

func geminiLowerToolCall(part schema.ToolCallPart) geminiFunctionCallPart {
	var fcp geminiFunctionCallPart
	fcp.FunctionCall.Name = part.Name
	fcp.FunctionCall.Args = part.Input
	fcp.ThoughtSignature = geminiThoughtSignature(part.ProviderMetadata)
	return fcp
}

func geminiLowerMessages(request *schema.LLMRequest) ([]geminiContent, error) {
	var contents []geminiContent
	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
			if len(contents) > 0 && contents[len(contents)-1].Role == "user" {
				last := &contents[len(contents)-1]
				last.Parts = append(last.Parts, geminiTextPart{Text: update})
			} else {
				contents = append(contents, geminiContent{Role: "user", Parts: []any{geminiTextPart{Text: update}}})
			}

		case schema.RoleUser:
			var parts []any
			for _, p := range msg.Content {
				part, err := geminiLowerUserPart(p)
				if err != nil {
					return nil, err
				}
				parts = append(parts, part)
			}
			contents = append(contents, geminiContent{Role: "user", Parts: parts})

		case schema.RoleAssistant:
			var parts []any
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					parts = append(parts, geminiTextPart{Text: part.Text})
				case schema.ReasoningPart:
					sig := geminiThoughtSignature(part.ProviderMetadata)
					if sig == nil {
						sig = part.Encrypted
					}
					t := true
					tp := geminiTextPart{Text: part.Text, Thought: &t, ThoughtSignature: sig}
					parts = append(parts, tp)
				case schema.ToolCallPart:
					parts = append(parts, geminiLowerToolCall(part))
				default:
					return nil, fmt.Errorf("Gemini assistant only supports text, reasoning, tool-call")
				}
			}
			contents = append(contents, geminiContent{Role: "model", Parts: parts})

		case schema.RoleTool:
			var parts []any
			for _, p := range msg.Content {
				if part, ok := p.(schema.ToolResultPart); ok {
					if part.Result.Type != "content" {
						parts = append(parts, geminiFunctionResponsePart{
							FunctionResponse: struct {
								Name     string `json:"name"`
								Response any    `json:"response"`
							}{Name: part.Name, Response: map[string]any{"name": part.Name, "content": ToolResultText(part)}},
						})
					} else {
						content, ok := part.Result.Value.([]schema.ToolContent)
						var texts []string
						if ok {
							for _, c := range content {
								if tc, ok := c.(schema.ToolTextContent); ok {
									texts = append(texts, tc.Text)
								}
							}
						}
						parts = append(parts, geminiFunctionResponsePart{
							FunctionResponse: struct {
								Name     string `json:"name"`
								Response any    `json:"response"`
							}{Name: part.Name, Response: map[string]any{"name": part.Name, "content": strings.Join(texts, "\n")}},
						})
						if ok {
							for _, c := range content {
								if _, ok := c.(schema.ToolTextContent); ok {
									continue
								}
								if fc, ok := c.(schema.ToolFileContent); ok {
									media, err := utils.ValidateToolFile("Gemini", fc, geminiMediaMIMEs)
									if err != nil {
										return nil, err
									}
									parts = append(parts, geminiInlineDataPart{
										InlineData: struct {
											MimeType string `json:"mimeType"`
											Data     string `json:"data"`
										}{MimeType: media.MIME, Data: media.Base64},
									})
								}
							}
						}
					}
				}
			}
			contents = append(contents, geminiContent{Role: "user", Parts: parts})
		}
	}
	return contents, nil
}

func geminiLowerOptions(request *schema.LLMRequest) *geminiThinkingConfig {
	raw := request.ProviderOptions["gemini"]
	if len(raw) == 0 {
		return nil
	}
	v, _ := raw["thinkingConfig"].(map[string]any)
	if len(v) == 0 {
		return nil
	}
	cfg := &geminiThinkingConfig{}
	if b, ok := v["thinkingBudget"].(float64); ok {
		n := int(b)
		cfg.ThinkingBudget = &n
	}
	if b, ok := v["includeThoughts"].(bool); ok {
		cfg.IncludeThoughts = &b
	}
	if cfg.ThinkingBudget == nil && cfg.IncludeThoughts == nil {
		return nil
	}
	return cfg
}

func geminiFromRequest(request *schema.LLMRequest) (any, error) {
	gen := request.Generation
	if gen == nil {
		gen = &schema.GenerationOptions{}
	}
	toolsEnabled := len(request.Tools) > 0 && (request.ToolChoice == nil || request.ToolChoice.Type != "none")

	contents, err := geminiLowerMessages(request)
	if err != nil {
		return nil, err
	}

	var sysInst any
	if len(request.System) > 0 {
		parts := make([]struct{ Text string }, len(request.System))
		for i, p := range request.System {
			parts[i].Text = p.Text
		}
		sysInst = map[string]any{"parts": []map[string]any{{"text": JoinText(parts)}}}
	}

	var tools []geminiTool
	if toolsEnabled {
		decls := make([]geminiFunctionDeclaration, 0, len(request.Tools))
		for _, t := range request.Tools {
			decls = append(decls, geminiLowerTool(t, ProjectToolSchema(request.Model, t.InputSchema)))
		}
		tools = []geminiTool{{FunctionDeclarations: decls}}
	}
	var toolCfg *geminiToolConfig
	if toolsEnabled && request.ToolChoice != nil {
		cfg, err := geminiLowerToolConfig(*request.ToolChoice)
		if err != nil {
			return nil, err
		}
		toolCfg = cfg
	}

	gc := &geminiGenerationConfig{
		MaxOutputTokens: gen.MaxTokens,
		Temperature:     gen.Temperature,
		TopP:            gen.TopP,
		TopK:            gen.TopK,
		StopSequences:   gen.Stop,
		ThinkingConfig:  geminiLowerOptions(request),
	}
	if gc.MaxOutputTokens == nil && gc.Temperature == nil && gc.TopP == nil && gc.TopK == nil &&
		gc.StopSequences == nil && gc.ThinkingConfig == nil {
		gc = nil
	}

	return geminiBody{
		Contents:          contents,
		SystemInstruction: sysInst,
		Tools:             tools,
		ToolConfig:        toolCfg,
		GenerationConfig:  gc,
	}, nil
}

func geminiDecodeEvent(frame string) (any, error) {
	if frame == "" || frame == "[DONE]" {
		return nil, nil
	}
	var evt geminiEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("Gemini: invalid event: %w", err)
	}
	// Response parts arrive as raw JSON objects; geminiStep switches on typed
	// parts, so decode them here.
	for i := range evt.Candidates {
		if evt.Candidates[i].Content != nil {
			evt.Candidates[i].Content.Parts = geminiDecodeParts(evt.Candidates[i].Content.Parts)
		}
	}
	return evt, nil
}

// geminiDecodeParts converts raw response part objects into the typed parts the
// stream step handles. Parts with neither text nor functionCall are passed
// through unchanged.
func geminiDecodeParts(parts []any) []any {
	decoded := make([]any, 0, len(parts))
	for _, part := range parts {
		raw, ok := part.(map[string]any)
		if !ok {
			decoded = append(decoded, part)
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			decoded = append(decoded, part)
			continue
		}
		if _, isCall := raw["functionCall"]; isCall {
			var call geminiFunctionCallPart
			if json.Unmarshal(encoded, &call) == nil {
				decoded = append(decoded, call)
				continue
			}
		}
		if _, hasText := raw["text"]; hasText {
			var text geminiTextPart
			if json.Unmarshal(encoded, &text) == nil {
				decoded = append(decoded, text)
				continue
			}
		}
		decoded = append(decoded, part)
	}
	return decoded
}

func geminiMapUsage(u *geminiUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	cached := geminiIntVal(u.CachedContentTokenCount)
	nonCached := geminiIntVal(u.PromptTokenCount) - cached
	if nonCached < 0 {
		nonCached = 0
	}
	var outputTokens int
	var hasOutput bool
	if u.CandidatesTokenCount != nil {
		outputTokens = geminiIntVal(u.CandidatesTokenCount) + geminiIntVal(u.ThoughtsTokenCount)
		hasOutput = true
	}
	var total int
	if hasOutput {
		total = geminiIntVal(TotalTokens(u.PromptTokenCount, &outputTokens, u.TotalTokenCount))
	} else {
		total = geminiIntVal(u.PromptTokenCount)
		if u.TotalTokenCount != nil {
			total = *u.TotalTokenCount
		}
	}
	return &schema.Usage{
		InputTokens:          geminiIntVal(u.PromptTokenCount),
		OutputTokens:         outputTokens,
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      geminiIntVal(u.ThoughtsTokenCount),
		TotalTokens:          total,
		ProviderMetadata:     u.ProviderMetadata,
	}
}

func geminiMapFinishReason(reason *string, hasToolCalls bool) schema.FinishReason {
	if reason == nil {
		return schema.ReasonUnknown
	}
	switch *reason {
	case "STOP":
		if hasToolCalls {
			return schema.ReasonToolCalls
		}
		return schema.ReasonStop
	case "MAX_TOKENS":
		return schema.ReasonLength
	case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return schema.ReasonContentFilter
	case "MALFORMED_FUNCTION_CALL":
		return schema.ReasonError
	}
	return schema.ReasonUnknown
}

func geminiStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(geminiParserState)
	evt, ok := event.(geminiEvent)
	if !ok {
		return state, nil, nil
	}

	if evt.UsageMetadata != nil {
		u := geminiMapUsage(evt.UsageMetadata)
		if u != nil {
			s.usage = u
		}
	}

	if len(evt.Candidates) == 0 || evt.Candidates[0].Content == nil {
		if len(evt.Candidates) > 0 {
			s.finishReason = evt.Candidates[0].FinishReason
		}
		return s, nil, nil
	}

	candidate := evt.Candidates[0]
	var events []schema.LLMEvent
	hasToolCalls := s.hasToolCalls
	lifecycle := s.lifecycle
	nextID := s.nextToolCallID
	reasoningSig := s.reasoningSignature

	for _, part := range candidate.Content.Parts {
		switch p := part.(type) {
		case geminiTextPart:
			if p.ThoughtSignature != nil && p.Thought != nil && *p.Thought {
				reasoningSig = p.ThoughtSignature
			}
			if p.Text == "" {
				continue
			}
			if p.Thought != nil && *p.Thought {
				var pm schema.ProviderMetadata
				if p.ThoughtSignature != nil {
					pm = geminiGoogleMetadata(map[string]any{"thoughtSignature": *p.ThoughtSignature})
				}
				utils.ReasoningDelta(&lifecycle, &events, "reasoning-0", p.Text, pm)
				continue
			}
			var pm schema.ProviderMetadata
			if reasoningSig != nil {
				pm = geminiGoogleMetadata(map[string]any{"thoughtSignature": *reasoningSig})
			}
			utils.ReasoningEnd(&lifecycle, &events, "reasoning-0", pm)
			utils.TextDelta(&lifecycle, &events, "text-0", p.Text)

		case geminiFunctionCallPart:
			id := fmt.Sprintf("tool_%d", nextID)
			nextID++
			var pm schema.ProviderMetadata
			if reasoningSig != nil {
				pm = geminiGoogleMetadata(map[string]any{"thoughtSignature": *reasoningSig})
			}
			utils.ReasoningEnd(&lifecycle, &events, "reasoning-0", pm)
			utils.StepStart(&lifecycle, &events)
			var fpm schema.ProviderMetadata
			if p.ThoughtSignature != nil {
				fpm = geminiGoogleMetadata(map[string]any{"thoughtSignature": *p.ThoughtSignature})
			}
			events = append(events, schema.NewToolCall(id, p.FunctionCall.Name, p.FunctionCall.Args,
				schema.WithEventProviderMetadata(fpm)))
			hasToolCalls = true
		}
	}

	s.hasToolCalls = hasToolCalls
	s.lifecycle = lifecycle
	s.nextToolCallID = nextID
	s.reasoningSignature = reasoningSig
	if candidate.FinishReason != nil {
		s.finishReason = candidate.FinishReason
	}
	return s, events, nil
}

func geminiFinish(state any) []schema.LLMEvent {
	s := state.(geminiParserState)
	if s.finishReason == nil && s.usage == nil {
		return nil
	}
	var events []schema.LLMEvent
	lifecycle := s.lifecycle
	if s.reasoningSignature != nil {
		utils.ReasoningEnd(&lifecycle, &events, "reasoning-0",
			geminiGoogleMetadata(map[string]any{"thoughtSignature": *s.reasoningSignature}))
	}
	utils.Finish(&lifecycle, &events, struct {
		Reason           schema.FinishReason
		Usage            *schema.Usage
		ProviderMetadata schema.ProviderMetadata
	}{Reason: geminiMapFinishReason(s.finishReason, s.hasToolCalls), Usage: s.usage})
	return events
}

var GeminiProtocol = route.Protocol{
	ID:   geminiAdapter,
	Body: route.ProtocolBody{From: geminiFromRequest},
	Stream: route.ProtocolStream{
		DecodeEvent: geminiDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return geminiParserState{
				lifecycle: utils.Initial(),
			}
		},
		Step:   geminiStep,
		OnHalt: geminiFinish,
	},
}

var GeminiHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:       geminiAdapter,
	Provider: "google",
	Protocol: GeminiProtocol,
	Endpoint: route.NewPath(func(input route.EndpointInput) string {
		modelID := "<unknown>"
		if input.Request != nil {
			modelID = string(input.Request.Model.ModelID())
		}
		return fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", modelID)
	}, route.WithBaseURL(GeminiDefaultBaseURL)),
	Transport: transport.SSEJson(),
})
