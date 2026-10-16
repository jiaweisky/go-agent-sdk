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

const geminiAdapter = "gemini"

// GeminiDefaultBaseURL is the Google Generative Language API base URL.
const GeminiDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// geminiMediaMIMEs are the media types Gemini accepts: images, audio, video, and
// pdf documents — all lowered by the generic inlineData/fileData block.
var geminiMediaMIMEs = mediaMIMESet(append(append([]string{}, mediaMIMEs...), "application/pdf"))

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
type geminiFileDataPart struct {
	FileData struct {
		MimeType string `json:"mimeType"`
		FileURI  string `json:"fileUri"`
	} `json:"fileData"`
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

// UnmarshalJSON reads the usage payload Gemini sends.
func (u *geminiUsage) UnmarshalJSON(data []byte) error {
	type plain geminiUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = geminiUsage(decoded)
	u.ProviderMetadata = providerMetadata("google", rawRecord(data))
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
	lifecycle          utils.LifecycleState
	reasoningSignature *string
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
	decl := geminiFunctionDeclaration{Name: td.Name, Description: td.Description}
	// An empty object schema projects to nil, so the key stays absent.
	if params := utils.GeminiConvert(schemaMap(inputSchema)); params != nil {
		decl.Parameters = params
	}
	return decl
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
		return geminiLowerMedia(p)
	}
	return nil, fmt.Errorf("Gemini user parts only support text and media")
}

// geminiLowerMedia lowers a media part: inline bytes become inlineData; url and
// file sources become fileData (the provider fetches a URL or reads a Files API
// reference).
func geminiLowerMedia(part schema.MediaPart) (any, error) {
	source := part.Source
	if source == "" {
		source = schema.MediaSourceInline
	}
	if source == schema.MediaSourceURL || source == schema.MediaSourceFile {
		uri, _ := part.Data.(string)
		if strings.TrimSpace(uri) == "" {
			return nil, InvalidRequest("Gemini media reference is empty")
		}
		return geminiFileData(part.MediaType, uri), nil
	}
	media, err := utils.ValidateMedia("Gemini", part, geminiMediaMIMEs)
	if err != nil {
		return nil, err
	}
	return geminiInlineData(media.MIME, media.Base64), nil
}

func geminiLowerToolCall(part schema.ToolCallPart) geminiFunctionCallPart {
	var fcp geminiFunctionCallPart
	fcp.FunctionCall.Name = part.Name
	fcp.FunctionCall.Args = part.Input
	fcp.ThoughtSignature = geminiThoughtSignature(part.ProviderMetadata)
	return fcp
}

func geminiLowerMessages(request *schema.LLMRequest) ([]geminiContent, error) {
	contents := []geminiContent{}
	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			contents = geminiLowerSystemMessage(contents, msg)
		case schema.RoleUser:
			content, err := geminiLowerUserMessage(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		case schema.RoleAssistant:
			content, err := geminiLowerAssistantMessage(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		case schema.RoleTool:
			content, err := geminiLowerToolMessage(msg)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		}
	}
	return contents, nil
}

func geminiLowerSystemMessage(contents []geminiContent, msg schema.Message) []geminiContent {
	update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
	if len(contents) > 0 && contents[len(contents)-1].Role == "user" {
		contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, geminiTextPart{Text: update})
		return contents
	}
	return append(contents, geminiContent{Role: "user", Parts: []any{geminiTextPart{Text: update}}})
}

func geminiLowerUserMessage(msg schema.Message) (geminiContent, error) {
	var parts []any
	for _, p := range msg.Content {
		part, err := geminiLowerUserPart(p)
		if err != nil {
			return geminiContent{}, err
		}
		parts = append(parts, part)
	}
	return geminiContent{Role: "user", Parts: parts}, nil
}

func geminiLowerAssistantMessage(msg schema.Message) (geminiContent, error) {
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
			parts = append(parts, geminiTextPart{Text: part.Text, Thought: &t, ThoughtSignature: sig})
		case schema.ToolCallPart:
			parts = append(parts, geminiLowerToolCall(part))
		default:
			return geminiContent{}, fmt.Errorf("Gemini assistant only supports text, reasoning, tool-call")
		}
	}
	return geminiContent{Role: "model", Parts: parts}, nil
}

func geminiLowerToolMessage(msg schema.Message) (geminiContent, error) {
	var parts []any
	for _, p := range msg.Content {
		part, ok := p.(schema.ToolResultPart)
		if !ok {
			continue
		}
		blocks, err := geminiLowerToolResultPart(part)
		if err != nil {
			return geminiContent{}, err
		}
		parts = append(parts, blocks...)
	}
	return geminiContent{Role: "user", Parts: parts}, nil
}

func geminiLowerToolResultPart(part schema.ToolResultPart) ([]any, error) {
	if part.Result.Type != "content" {
		return []any{geminiFunctionResponse(part.Name, map[string]any{
			"name": part.Name, "content": ToolResultText(part),
		})}, nil
	}
	content, ok := part.Result.Value.([]schema.ToolContent)
	var texts []string
	if ok {
		for _, c := range content {
			if tc, ok := c.(schema.ToolTextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
	}
	parts := []any{geminiFunctionResponse(part.Name, map[string]any{
		"name": part.Name, "content": strings.Join(texts, "\n"),
	})}
	if !ok {
		return parts, nil
	}
	for _, c := range content {
		if _, ok := c.(schema.ToolTextContent); ok {
			continue
		}
		fc, ok := c.(schema.ToolFileContent)
		if !ok {
			continue
		}
		source := fc.Source
		if source == "" {
			source = schema.MediaSourceInline
		}
		if source == schema.MediaSourceURL || source == schema.MediaSourceFile {
			uri := fc.URI
			if fc.FileID != "" {
				uri = fc.FileID
			}
			parts = append(parts, geminiFileData(fc.Mime, uri))
			continue
		}
		media, err := utils.ValidateToolFile("Gemini", fc, geminiMediaMIMEs)
		if err != nil {
			return nil, err
		}
		parts = append(parts, geminiInlineData(media.MIME, media.Base64))
	}
	return parts, nil
}

func geminiFunctionResponse(name string, response any) geminiFunctionResponsePart {
	var part geminiFunctionResponsePart
	part.FunctionResponse.Name = name
	part.FunctionResponse.Response = response
	return part
}

func geminiInlineData(mimeType, data string) geminiInlineDataPart {
	var part geminiInlineDataPart
	part.InlineData.MimeType = mimeType
	part.InlineData.Data = data
	return part
}

func geminiFileData(mimeType, fileURI string) geminiFileDataPart {
	var part geminiFileDataPart
	part.FileData.MimeType = mimeType
	part.FileData.FileURI = fileURI
	return part
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
	cached := intVal(u.CachedContentTokenCount)
	nonCached := intVal(u.PromptTokenCount) - cached
	if nonCached < 0 {
		nonCached = 0
	}
	var outputTokens int
	var hasOutput bool
	if u.CandidatesTokenCount != nil {
		outputTokens = intVal(u.CandidatesTokenCount) + intVal(u.ThoughtsTokenCount)
		hasOutput = true
	}
	var total int
	if hasOutput {
		total = intVal(TotalTokens(u.PromptTokenCount, &outputTokens, u.TotalTokenCount))
	} else {
		total = intVal(u.PromptTokenCount)
		if u.TotalTokenCount != nil {
			total = *u.TotalTokenCount
		}
	}
	return &schema.Usage{
		InputTokens:          intVal(u.PromptTokenCount),
		OutputTokens:         outputTokens,
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      intVal(u.ThoughtsTokenCount),
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
		return s, nil, nil
	}

	if evt.UsageMetadata != nil {
		if u := geminiMapUsage(evt.UsageMetadata); u != nil {
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
	for _, part := range candidate.Content.Parts {
		geminiApplyPart(&s, &events, part)
	}
	if candidate.FinishReason != nil {
		s.finishReason = candidate.FinishReason
	}
	return s, events, nil
}

func geminiApplyPart(s *geminiParserState, events *[]schema.LLMEvent, part any) {
	switch p := part.(type) {
	case geminiTextPart:
		if p.ThoughtSignature != nil && p.Thought != nil && *p.Thought {
			s.reasoningSignature = p.ThoughtSignature
		}
		if p.Text == "" {
			return
		}
		if p.Thought != nil && *p.Thought {
			var pm schema.ProviderMetadata
			if p.ThoughtSignature != nil {
				pm = providerMetadata("google", map[string]any{"thoughtSignature": *p.ThoughtSignature})
			}
			utils.ReasoningDelta(&s.lifecycle, events, "reasoning-0", p.Text, pm)
			return
		}
		var pm schema.ProviderMetadata
		if s.reasoningSignature != nil {
			pm = providerMetadata("google", map[string]any{"thoughtSignature": *s.reasoningSignature})
		}
		utils.ReasoningEnd(&s.lifecycle, events, "reasoning-0", pm)
		utils.TextDelta(&s.lifecycle, events, "text-0", p.Text)

	case geminiFunctionCallPart:
		id := fmt.Sprintf("tool_%d", s.nextToolCallID)
		s.nextToolCallID++
		var pm schema.ProviderMetadata
		if s.reasoningSignature != nil {
			pm = providerMetadata("google", map[string]any{"thoughtSignature": *s.reasoningSignature})
		}
		utils.ReasoningEnd(&s.lifecycle, events, "reasoning-0", pm)
		utils.StepStart(&s.lifecycle, events)
		var fpm schema.ProviderMetadata
		if p.ThoughtSignature != nil {
			fpm = providerMetadata("google", map[string]any{"thoughtSignature": *p.ThoughtSignature})
		}
		*events = append(*events, schema.NewToolCall(id, p.FunctionCall.Name, p.FunctionCall.Args,
			schema.WithEventProviderMetadata(fpm)))
		s.hasToolCalls = true
	}
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
			providerMetadata("google", map[string]any{"thoughtSignature": *s.reasoningSignature}))
	}
	utils.LifecycleFinish(&lifecycle, &events, utils.FinishInput{
		Reason: geminiMapFinishReason(s.finishReason, s.hasToolCalls),
		Usage:  s.usage,
	})
	return events
}

// GeminiProtocol is the Gemini generateContent wire protocol.
var GeminiProtocol = route.Protocol{
	ID:   geminiAdapter,
	Body: route.ProtocolBody{Schema: geminiBodySchema, From: geminiFromRequest},
	Stream: route.ProtocolStream{
		DecodeEvent: geminiDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return geminiParserState{
				lifecycle: utils.LifecycleInitial(),
			}
		},
		Step:   geminiStep,
		OnHalt: geminiFinish,
	},
}

// GeminiHTTPRoute is the Gemini route.
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
