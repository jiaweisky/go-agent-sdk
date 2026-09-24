package protocols

import (
	"encoding/json"
	"fmt"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
	"github.com/user/go-agent-sdk/llm/schema"
)

const openaiResponsesAdapter = "openai-responses"
const OpenAIResponsesDefaultBaseURL = "https://api.openai.com/v1"
const OpenAIResponsesPath = "/responses"

var openaiResponsesImageMIMEs = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {},
}

type openaiResponsesInputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type openaiResponsesInputImage struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
}
type openaiResponsesOutputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type openaiResponsesSummaryText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type openaiResponsesReasoningItem struct {
	Type             string                       `json:"type"`
	ID               string                       `json:"-"`
	Summary          []openaiResponsesSummaryText `json:"summary"`
	EncryptedContent *string                      `json:"encrypted_content,omitempty"`
}
type openaiResponsesItemReference struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type openaiResponsesFunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type openaiResponsesFunctionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}
type openaiResponsesUserItem struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}
type openaiResponsesAssistantItem struct {
	Role    string                      `json:"role"`
	Content []openaiResponsesOutputText `json:"content"`
}
type openaiResponsesSystemItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponsesInputItem any

type openaiResponsesResponsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict"`
}

type openaiResponsesResponsesBody struct {
	Model           string                         `json:"model"`
	Input           []openaiResponsesInputItem     `json:"input"`
	Instructions    *string                        `json:"instructions,omitempty"`
	Tools           []openaiResponsesResponsesTool `json:"tools,omitempty"`
	ToolChoice      any                            `json:"tool_choice,omitempty"`
	Stream          bool                           `json:"stream"`
	Store           *bool                          `json:"store,omitempty"`
	ServiceTier     *string                        `json:"service_tier,omitempty"`
	PromptCacheKey  *string                        `json:"prompt_cache_key,omitempty"`
	Include         []string                       `json:"include,omitempty"`
	Reasoning       any                            `json:"reasoning,omitempty"`
	Text            any                            `json:"text,omitempty"`
	MaxOutputTokens *int                           `json:"max_output_tokens,omitempty"`
	Temperature     *float64                       `json:"temperature,omitempty"`
	TopP            *float64                       `json:"top_p,omitempty"`
}

type openaiResponsesResponsesUsage struct {
	InputTokens         *int                                `json:"input_tokens,omitempty"`
	InputTokensDetails  *openaiResponsesInputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokens        *int                                `json:"output_tokens,omitempty"`
	OutputTokensDetails *openaiResponsesOutputTokensDetails `json:"output_tokens_details,omitempty"`
	TotalTokens         *int                                `json:"total_tokens,omitempty"`
	ProviderMetadata    schema.ProviderMetadata             `json:"-"`
}

func (u *openaiResponsesResponsesUsage) UnmarshalJSON(data []byte) error {
	type plain openaiResponsesResponsesUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = openaiResponsesResponsesUsage(decoded)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		u.ProviderMetadata = schema.ProviderMetadata{"openai": raw}
	}
	return nil
}

type openaiResponsesInputTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}
type openaiResponsesOutputTokensDetails struct {
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
}

type openaiResponsesStreamItem struct {
	Type             string  `json:"type"`
	ID               *string `json:"id,omitempty"`
	CallID           *string `json:"call_id,omitempty"`
	Name             *string `json:"name,omitempty"`
	Arguments        *string `json:"arguments,omitempty"`
	Status           *string `json:"status,omitempty"`
	Action           any     `json:"action,omitempty"`
	Queries          any     `json:"queries,omitempty"`
	Results          any     `json:"results,omitempty"`
	Code             *string `json:"code,omitempty"`
	ContainerID      *string `json:"container_id,omitempty"`
	Outputs          any     `json:"outputs,omitempty"`
	ServerLabel      *string `json:"server_label,omitempty"`
	Output           any     `json:"output,omitempty"`
	Error            any     `json:"error,omitempty"`
	EncryptedContent *string `json:"encrypted_content,omitempty"`
}

type openaiResponsesErrorPayload struct {
	Code    *string `json:"code,omitempty"`
	Message *string `json:"message,omitempty"`
	Param   *string `json:"param,omitempty"`
}

type openaiResponsesResponseEntry struct {
	ID                *string                           `json:"id,omitempty"`
	ServiceTier       *string                           `json:"service_tier,omitempty"`
	IncompleteDetails *openaiResponsesIncompleteDetails `json:"incomplete_details,omitempty"`
	Usage             *openaiResponsesResponsesUsage    `json:"usage,omitempty"`
	Error             *openaiResponsesErrorPayload      `json:"error,omitempty"`
}
type openaiResponsesIncompleteDetails struct {
	Reason string `json:"reason"`
}

type openaiResponsesResponsesEvent struct {
	Type         string                        `json:"type"`
	Delta        *string                       `json:"delta,omitempty"`
	ItemID       *string                       `json:"item_id,omitempty"`
	SummaryIndex *int                          `json:"summary_index,omitempty"`
	Item         *openaiResponsesStreamItem    `json:"item,omitempty"`
	Response     *openaiResponsesResponseEntry `json:"response,omitempty"`
	Code         *string                       `json:"code,omitempty"`
	Message      *string                       `json:"message,omitempty"`
	Param        *string                       `json:"param,omitempty"`
}

type openaiResponsesReasoningStreamItem struct {
	EncryptedContent *string
	SummaryParts     map[int]string
}

type openaiResponsesParserState struct {
	tools           map[string]*utils.PendingTool
	hasFunctionCall bool
	lifecycle       utils.State
	reasoningItems  map[string]*openaiResponsesReasoningStreamItem
	store           *bool
}

type openaiResponsesHostedToolDef struct {
	Name  string
	Input func(item openaiResponsesStreamItem) any
}

var openaiResponsesHostedTools = map[string]openaiResponsesHostedToolDef{
	"web_search_call":         {Name: "web_search", Input: func(item openaiResponsesStreamItem) any { return item.Action }},
	"web_search_preview_call": {Name: "web_search_preview", Input: func(item openaiResponsesStreamItem) any { return item.Action }},
	"file_search_call":        {Name: "file_search", Input: func(item openaiResponsesStreamItem) any { return map[string]any{"queries": item.Queries} }},
	"code_interpreter_call": {Name: "code_interpreter", Input: func(item openaiResponsesStreamItem) any {
		return map[string]any{"code": item.Code, "container_id": item.ContainerID}
	}},
	"computer_use_call":     {Name: "computer_use", Input: func(item openaiResponsesStreamItem) any { return item.Action }},
	"image_generation_call": {Name: "image_generation", Input: func(item openaiResponsesStreamItem) any { return map[string]any{} }},
	"mcp_call": {Name: "mcp", Input: func(item openaiResponsesStreamItem) any {
		return map[string]any{"server_label": item.ServerLabel, "name": item.Name, "arguments": item.Arguments}
	}},
	"local_shell_call": {Name: "local_shell", Input: func(item openaiResponsesStreamItem) any { return item.Action }},
}

func openaiResponsesIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func openaiResponsesStrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func openaiResponsesLowerTool(td schema.ToolDefinition, inputSchema any) openaiResponsesResponsesTool {
	s, _ := inputSchema.(map[string]any)
	return openaiResponsesResponsesTool{
		Type:        "function",
		Name:        td.Name,
		Description: td.Description,
		Parameters:  s,
		Strict:      false,
	}
}

func openaiResponsesLowerToolChoice(tc schema.ToolChoice) any {
	if tc.Name != nil {
		return map[string]any{"type": "function", "name": *tc.Name}
	}
	return tc.Type
}

func openaiResponsesLowerToolCall(part schema.ToolCallPart) openaiResponsesFunctionCallItem {
	b, _ := json.Marshal(part.Input)
	return openaiResponsesFunctionCallItem{
		Type:      "function_call",
		CallID:    part.ID,
		Name:      part.Name,
		Arguments: string(b),
	}
}

func openaiResponsesLowerReasoning(part schema.ReasoningPart) *openaiResponsesReasoningItem {
	md := part.ProviderMetadata
	var raw map[string]any
	if md != nil {
		raw = md["openai"]
	}
	if raw == nil {
		return nil
	}
	itemID, _ := raw["itemId"].(string)
	if itemID == "" {
		return nil
	}
	item := &openaiResponsesReasoningItem{
		Type: "reasoning",
		ID:   itemID,
	}
	if part.Text != "" {
		item.Summary = []openaiResponsesSummaryText{{Type: "summary_text", Text: part.Text}}
	}
	if ec, ok := raw["reasoningEncryptedContent"].(string); ok {
		item.EncryptedContent = &ec
	}
	return item
}

func openaiResponsesHostedToolItemID(part schema.ToolResultPart) *string {
	md := part.ProviderMetadata
	var raw map[string]any
	if md != nil {
		raw = md["openai"]
	}
	if raw == nil {
		return nil
	}
	itemID, _ := raw["itemId"].(string)
	if itemID == "" {
		return nil
	}
	return &itemID
}

func openaiResponsesLowerUserContent(part schema.ContentPart) (any, error) {
	switch p := part.(type) {
	case schema.TextPart:
		return openaiResponsesInputText{Type: "input_text", Text: p.Text}, nil
	case schema.MediaPart:
		media, err := utils.ValidateMedia("OpenAI Responses", p, openaiResponsesImageMIMEs)
		if err != nil {
			return nil, err
		}
		return openaiResponsesInputImage{Type: "input_image", ImageURL: media.DataURL}, nil
	}
	return nil, fmt.Errorf("OpenAI Responses user content only supports text and media")
}

func openaiResponsesLowerToolResultContentItem(item schema.ToolContent) (any, error) {
	switch v := item.(type) {
	case schema.ToolTextContent:
		return openaiResponsesInputText{Type: "input_text", Text: v.Text}, nil
	case schema.ToolFileContent:
		media, err := utils.ValidateToolFile("OpenAI Responses", v, openaiResponsesImageMIMEs)
		if err != nil {
			return nil, err
		}
		return openaiResponsesInputImage{Type: "input_image", ImageURL: media.DataURL}, nil
	}
	return openaiResponsesInputText{Type: "input_text", Text: fmt.Sprint(item)}, nil
}

func openaiResponsesLowerToolResultOutput(part schema.ToolResultPart) (any, error) {
	if part.Result.Type != "content" {
		return ToolResultText(part), nil
	}
	content, ok := part.Result.Value.([]schema.ToolContent)
	if !ok {
		return ToolResultText(part), nil
	}
	var items []any
	for _, c := range content {
		item, err := openaiResponsesLowerToolResultContentItem(c)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func openaiResponsesOpenAIMetadata(m map[string]any) schema.ProviderMetadata {
	return schema.ProviderMetadata{"openai": m}
}

func openaiResponsesReasoningMetadata(itemID string, encrypted *string) schema.ProviderMetadata {
	var value any
	if encrypted != nil {
		value = *encrypted
	}
	return openaiResponsesOpenAIMetadata(map[string]any{
		"itemId":                    itemID,
		"reasoningEncryptedContent": value,
	})
}

func openaiResponsesLowerMessages(request *schema.LLMRequest) ([]openaiResponsesInputItem, error) {
	var input []openaiResponsesInputItem
	if len(request.System) > 0 {
		parts := make([]struct{ Text string }, len(request.System))
		for i, p := range request.System {
			parts[i].Text = p.Text
		}
		text := JoinText(parts)
		if text != "" {
			input = append(input, openaiResponsesSystemItem{Role: "system", Content: text})
		}
	}
	store := utils.Store(request)

	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
			if len(input) > 0 {
				if last, ok := input[len(input)-1].(openaiResponsesUserItem); ok {
					last.Content = append(last.Content, openaiResponsesInputText{Type: "input_text", Text: update})
					input[len(input)-1] = last
				} else {
					input = append(input, openaiResponsesUserItem{Role: "user", Content: []any{openaiResponsesInputText{Type: "input_text", Text: update}}})
				}
			} else {
				input = append(input, openaiResponsesUserItem{Role: "user", Content: []any{openaiResponsesInputText{Type: "input_text", Text: update}}})
			}

		case schema.RoleUser:
			var userContent []any
			for _, p := range msg.Content {
				c, err := openaiResponsesLowerUserContent(p)
				if err != nil {
					return nil, err
				}
				userContent = append(userContent, c)
			}
			input = append(input, openaiResponsesUserItem{Role: "user", Content: userContent})

		case schema.RoleAssistant:
			var textParts []string
			reasoningItems := map[string]*openaiResponsesReasoningItem{}
			var reasoningRefs map[string]bool
			var hostedRefs map[string]bool
			for _, p := range msg.Content {
				switch part := p.(type) {
				case schema.TextPart:
					textParts = append(textParts, part.Text)
				case schema.ReasoningPart:
					r := openaiResponsesLowerReasoning(part)
					if r == nil {
						continue
					}
					openaiResponsesFlushText(&input, &textParts)
					if store == nil || *store {
						if reasoningRefs == nil {
							reasoningRefs = make(map[string]bool)
						}
						if !reasoningRefs[r.ID] {
							input = append(input, openaiResponsesItemReference{Type: "item_reference", ID: r.ID})
							reasoningRefs[r.ID] = true
						}
					} else if existing := reasoningItems[r.ID]; existing != nil {
						existing.Summary = append(existing.Summary, r.Summary...)
						if r.EncryptedContent != nil {
							existing.EncryptedContent = r.EncryptedContent
						}
					} else {
						reasoningItems[r.ID] = r
						input = append(input, r)
					}
				case schema.ToolCallPart:
					if part.ProviderExecuted != nil && *part.ProviderExecuted {
						continue
					}
					openaiResponsesFlushText(&input, &textParts)
					input = append(input, openaiResponsesLowerToolCall(part))
				case schema.ToolResultPart:
					if part.ProviderExecuted == nil || !*part.ProviderExecuted {
						continue
					}
					openaiResponsesFlushText(&input, &textParts)
					itemID := openaiResponsesHostedToolItemID(part)
					if store == nil || *store {
						if itemID != nil {
							if hostedRefs == nil {
								hostedRefs = make(map[string]bool)
							}
							if !hostedRefs[*itemID] {
								input = append(input, openaiResponsesItemReference{Type: "item_reference", ID: *itemID})
								hostedRefs[*itemID] = true
							}
						}
					}
				}
			}
			openaiResponsesFlushText(&input, &textParts)

		case schema.RoleTool:
			for _, p := range msg.Content {
				if part, ok := p.(schema.ToolResultPart); ok {
					output, err := openaiResponsesLowerToolResultOutput(part)
					if err != nil {
						return nil, err
					}
					input = append(input, openaiResponsesFunctionCallOutputItem{
						Type:   "function_call_output",
						CallID: part.ID,
						Output: output,
					})
				}
			}
		}
	}

	if store != nil && !*store {
		var filtered []openaiResponsesInputItem
		for _, item := range input {
			if r, ok := item.(*openaiResponsesReasoningItem); ok && r.EncryptedContent == nil {
				continue
			}
			filtered = append(filtered, item)
		}
		return filtered, nil
	}
	return input, nil
}

func openaiResponsesFlushText(input *[]openaiResponsesInputItem, texts *[]string) {
	if len(*texts) == 0 {
		return
	}
	content := make([]openaiResponsesOutputText, len(*texts))
	for i, t := range *texts {
		content[i] = openaiResponsesOutputText{Type: "output_text", Text: t}
	}
	*input = append(*input, openaiResponsesAssistantItem{Role: "assistant", Content: content})
	*texts = nil
}

func openaiResponsesLowerOptions(request *schema.LLMRequest) map[string]any {
	opts := make(map[string]any)
	if ins := utils.Instructions(request); ins != nil {
		opts["instructions"] = *ins
	}
	if store := utils.Store(request); store != nil {
		opts["store"] = *store
	}
	if key := utils.PromptCacheKey(request); key != nil {
		opts["prompt_cache_key"] = *key
	}
	if inc := utils.Include(request); inc != nil {
		opts["include"] = inc
	}
	effort := utils.ReasoningEffort(request)
	summary := utils.ReasoningSummary(request)
	if effort != nil || summary != nil {
		r := make(map[string]any)
		if effort != nil {
			r["effort"] = *effort
		}
		if summary != nil {
			r["summary"] = *summary
		}
		opts["reasoning"] = r
	}
	if verbosity := utils.TextVerbosity(request); verbosity != nil {
		opts["text"] = map[string]any{"verbosity": *verbosity}
	}
	if tier := utils.ServiceTier(request); tier != nil {
		opts["service_tier"] = *tier
	}
	return opts
}

func openaiResponsesFromRequest(request *schema.LLMRequest) (any, error) {
	gen := request.Generation
	messages, err := openaiResponsesLowerMessages(request)
	if err != nil {
		return nil, err
	}
	opts := openaiResponsesLowerOptions(request)
	body := map[string]any{
		"model":  string(request.Model.ModelID()),
		"input":  messages,
		"stream": true,
	}
	for k, v := range opts {
		body[k] = v
	}
	if len(request.Tools) > 0 {
		tools := make([]openaiResponsesResponsesTool, 0, len(request.Tools))
		for _, t := range request.Tools {
			tools = append(tools, openaiResponsesLowerTool(t, t.InputSchema))
		}
		body["tools"] = tools
	}
	if request.ToolChoice != nil {
		body["tool_choice"] = openaiResponsesLowerToolChoice(*request.ToolChoice)
	}
	if gen != nil {
		body["max_output_tokens"] = gen.MaxTokens
		body["temperature"] = gen.Temperature
		body["top_p"] = gen.TopP
	}
	return body, nil
}

func openaiResponsesDecodeEvent(frame string) (any, error) {
	if frame == "" || frame == "[DONE]" {
		return nil, nil
	}
	var evt openaiResponsesResponsesEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("OpenAI Responses: invalid stream event: %s: %w", frame, err)
	}
	return evt, nil
}

func openaiResponsesMapUsage(u *openaiResponsesResponsesUsage) *schema.Usage {
	if u == nil {
		return nil
	}
	var cached, reasoning int
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens != nil {
		cached = *u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens != nil {
		reasoning = *u.OutputTokensDetails.ReasoningTokens
	}
	nonCached := openaiResponsesIntVal(u.InputTokens)
	if cached > 0 {
		nonCached = openaiResponsesIntVal(u.InputTokens) - cached
	}
	return &schema.Usage{
		InputTokens:          openaiResponsesIntVal(u.InputTokens),
		OutputTokens:         openaiResponsesIntVal(u.OutputTokens),
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      reasoning,
		TotalTokens:          openaiResponsesIntVal(TotalTokens(u.InputTokens, u.OutputTokens, u.TotalTokens)),
		ProviderMetadata:     u.ProviderMetadata,
	}
}

func openaiResponsesMapFinishReason(evt openaiResponsesResponsesEvent, hasFunctionCall bool) schema.FinishReason {
	if evt.Response == nil || evt.Response.IncompleteDetails == nil {
		if hasFunctionCall {
			return schema.ReasonToolCalls
		}
		return schema.ReasonStop
	}
	switch evt.Response.IncompleteDetails.Reason {
	case "max_output_tokens":
		return schema.ReasonLength
	case "content_filter":
		return schema.ReasonContentFilter
	}
	if hasFunctionCall {
		return schema.ReasonToolCalls
	}
	return schema.ReasonUnknown
}

var openaiResponsesTerminalTypes = map[string]bool{
	"response.completed":  true,
	"response.incomplete": true,
	"response.failed":     true,
}

func openaiResponsesIsHostedTool(item openaiResponsesStreamItem) (openaiResponsesHostedToolDef, bool) {
	t, ok := openaiResponsesHostedTools[item.Type]
	return t, ok
}

func openaiResponsesIsReasoningItem(item openaiResponsesStreamItem) bool {
	return item.Type == "reasoning" && item.ID != nil && *item.ID != ""
}

func openaiResponsesHostedResult(item openaiResponsesStreamItem) schema.ToolResultValue {
	if item.Error != nil {
		return schema.ToolResultValue{Type: "error", Value: item.Error}
	}
	return schema.ToolResultValue{Type: "json", Value: item}
}

func openaiResponsesHostedEvents(item openaiResponsesStreamItem, tool openaiResponsesHostedToolDef) []schema.LLMEvent {
	pm := openaiResponsesOpenAIMetadata(map[string]any{"itemId": openaiResponsesStrVal(item.ID)})
	return []schema.LLMEvent{
		schema.NewToolCall(openaiResponsesStrVal(item.ID), tool.Name, tool.Input(item),
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)),
		schema.NewToolResult(openaiResponsesStrVal(item.ID), tool.Name, openaiResponsesHostedResult(item),
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)),
	}
}

func openaiResponsesOnOutputTextDelta(state openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	if evt.Delta == nil {
		return state, nil
	}
	var events []schema.LLMEvent
	itemID := openaiResponsesStrVal(evt.ItemID)
	if itemID == "" {
		itemID = "text-0"
	}
	utils.TextDelta(&state.lifecycle, &events, itemID, *evt.Delta)
	return state, events
}

func openaiResponsesOnReasoningDelta(state openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	if evt.Delta == nil {
		return state, nil
	}
	var events []schema.LLMEvent
	itemID := openaiResponsesStrVal(evt.ItemID)
	if itemID == "" {
		itemID = "reasoning-0"
	}
	id := itemID
	if _, tracked := state.reasoningItems[itemID]; evt.SummaryIndex != nil || tracked {
		id = fmt.Sprintf("%s:%d", itemID, openaiResponsesIntVal(evt.SummaryIndex))
	}
	utils.ReasoningDelta(&state.lifecycle, &events, id, *evt.Delta)
	return state, events
}

func openaiResponsesStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(openaiResponsesParserState)
	evt, ok := event.(openaiResponsesResponsesEvent)
	if !ok {
		return state, nil, nil
	}
	switch evt.Type {
	case "response.output_text.delta":
		ns, events := openaiResponsesOnOutputTextDelta(s, evt)
		return ns, events, nil
	case "response.reasoning_text.delta", "response.reasoning_summary.delta", "response.reasoning_summary_text.delta":
		ns, events := openaiResponsesOnReasoningDelta(s, evt)
		return ns, events, nil
	case "response.reasoning_summary_part.added":
		return openaiResponsesOnReasoningSummaryPartAdded(s, evt)
	case "response.reasoning_summary_part.done":
		return openaiResponsesOnReasoningSummaryPartDone(s, evt)
	case "response.output_item.added":
		return openaiResponsesOnOutputItemAdded(s, evt)
	case "response.function_call_arguments.delta":
		return openaiResponsesOnFunctionCallArgumentsDelta(s, evt)
	case "response.output_item.done":
		return openaiResponsesOnOutputItemDone(s, evt)
	case "response.completed", "response.incomplete":
		ns, evts := openaiResponsesOnResponseFinish(s, evt)
		return ns, evts, nil
	case "response.failed":
		ns, evts := openaiResponsesOnResponseFailed(s, evt)
		return ns, evts, nil
	case "error":
		ns, evts := openaiResponsesOnError(s, evt)
		return ns, evts, nil
	}
	return s, nil, nil
}

func openaiResponsesTerminal(event any) bool {
	evt, ok := event.(openaiResponsesResponsesEvent)
	if !ok {
		return false
	}
	return openaiResponsesTerminalTypes[evt.Type]
}

func openaiResponsesOnReasoningSummaryPartAdded(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	if evt.ItemID == nil || evt.SummaryIndex == nil {
		return s, nil, nil
	}
	itemID := *evt.ItemID
	idx := *evt.SummaryIndex

	existing, has := s.reasoningItems[itemID]
	if !has {
		existing = &openaiResponsesReasoningStreamItem{SummaryParts: make(map[int]string)}
		s.reasoningItems[itemID] = existing
	}

	if idx == 0 && has {
		return s, nil, nil
	}

	var events []schema.LLMEvent
	if idx > 0 && existing.SummaryParts != nil {
		for k, v := range existing.SummaryParts {
			if v == "can-conclude" {
				utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("%s:%d", itemID, k),
					openaiResponsesOpenAIMetadata(map[string]any{"itemId": itemID}))
				existing.SummaryParts[k] = "concluded"
			}
		}
	}
	utils.ReasoningStart(&s.lifecycle, &events, fmt.Sprintf("%s:%d", itemID, idx),
		openaiResponsesReasoningMetadata(itemID, existing.EncryptedContent))
	existing.SummaryParts[idx] = "active"
	return s, events, nil
}

func openaiResponsesOnReasoningSummaryPartDone(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	if evt.ItemID == nil || evt.SummaryIndex == nil {
		return s, nil, nil
	}
	itemID := *evt.ItemID
	existing := s.reasoningItems[itemID]
	if existing == nil {
		return s, nil, nil
	}
	var events []schema.LLMEvent
	store := s.store == nil || *s.store
	if store {
		utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("%s:%d", itemID, *evt.SummaryIndex),
			openaiResponsesOpenAIMetadata(map[string]any{"itemId": itemID}))
		existing.SummaryParts[*evt.SummaryIndex] = "concluded"
	} else {
		existing.SummaryParts[*evt.SummaryIndex] = "can-conclude"
	}
	return s, events, nil
}

func openaiResponsesOnOutputItemAdded(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	item := evt.Item
	if item == nil {
		return s, nil, nil
	}
	if openaiResponsesIsReasoningItem(*item) {
		var events []schema.LLMEvent
		utils.ReasoningStart(&s.lifecycle, &events, fmt.Sprintf("%s:0", *item.ID),
			openaiResponsesReasoningMetadata(*item.ID, item.EncryptedContent))
		if s.reasoningItems == nil {
			s.reasoningItems = make(map[string]*openaiResponsesReasoningStreamItem)
		}
		s.reasoningItems[*item.ID] = &openaiResponsesReasoningStreamItem{
			EncryptedContent: item.EncryptedContent,
			SummaryParts:     map[int]string{0: "active"},
		}
		return s, events, nil
	}
	if item.Type != "function_call" || item.ID == nil {
		return s, nil, nil
	}
	pm := openaiResponsesOpenAIMetadata(map[string]any{"itemId": *item.ID})
	var events []schema.LLMEvent
	utils.StepStart(&s.lifecycle, &events)
	s.tools = utils.Start(s.tools, *item.ID, utils.PendingTool{
		ID:               openaiResponsesStrVal(item.CallID),
		Name:             openaiResponsesStrVal(item.Name),
		Input:            openaiResponsesStrVal(item.Arguments),
		ProviderMetadata: pm,
	})
	events = append(events, schema.NewToolInputStart(openaiResponsesStrVal(item.CallID), openaiResponsesStrVal(item.Name), schema.WithEventProviderMetadata(pm)))
	return s, events, nil
}

func openaiResponsesOnFunctionCallArgumentsDelta(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	if evt.ItemID == nil || evt.Delta == nil {
		return s, nil, nil
	}
	result, err := utils.AppendExisting(openaiResponsesAdapter, s.tools, *evt.ItemID, *evt.Delta,
		"OpenAI Responses tool argument delta is missing its tool call")
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

func openaiResponsesOnOutputItemDone(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	item := evt.Item
	if item == nil {
		return s, nil, nil
	}
	switch item.Type {
	case "function_call":
		if item.ID == nil || item.CallID == nil || item.Name == nil {
			return s, nil, nil
		}
		if s.tools[*item.ID] == nil {
			s.tools = utils.Start(s.tools, *item.ID, utils.PendingTool{
				ID:   *item.CallID,
				Name: *item.Name,
			})
		}
		var outcome utils.FinishOutcome
		var finishErr error
		if item.Arguments != nil {
			outcome, finishErr = utils.FinishToolWithInput(openaiResponsesAdapter, s.tools, *item.ID, *item.Arguments)
		} else {
			outcome, finishErr = utils.FinishTool(openaiResponsesAdapter, s.tools, *item.ID)
		}
		if finishErr != nil {
			return s, nil, finishErr
		}
		var events []schema.LLMEvent
		if len(outcome.Events) > 0 {
			utils.StepStart(&s.lifecycle, &events)
		}
		events = append(events, outcome.Events...)
		for _, e := range outcome.Events {
			if e.IsToolCall() {
				s.hasFunctionCall = true
				break
			}
		}
		s.tools = outcome.Tools
		return s, events, nil
	default:
		if t, ok := openaiResponsesIsHostedTool(*item); ok {
			var events []schema.LLMEvent
			utils.StepStart(&s.lifecycle, &events)
			events = append(events, openaiResponsesHostedEvents(*item, t)...)
			return s, events, nil
		}
		if openaiResponsesIsReasoningItem(*item) {
			var events []schema.LLMEvent
			ri := s.reasoningItems[*item.ID]
			if ri != nil {
				for k, v := range ri.SummaryParts {
					if v == "active" || v == "can-conclude" {
						utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("%s:%d", *item.ID, k),
							openaiResponsesReasoningMetadata(*item.ID, item.EncryptedContent))
					}
				}
				delete(s.reasoningItems, *item.ID)
			} else if _, ok := s.lifecycle.Reasoning[*item.ID]; !ok {
				utils.StepStart(&s.lifecycle, &events)
				pm := openaiResponsesOpenAIMetadata(map[string]any{"itemId": *item.ID})
				events = append(events, schema.NewReasoningStart(*item.ID, schema.WithEventProviderMetadata(pm)))
				events = append(events, schema.NewReasoningEnd(*item.ID, schema.WithEventProviderMetadata(pm)))
			} else {
				utils.ReasoningEnd(&s.lifecycle, &events, *item.ID,
					openaiResponsesOpenAIMetadata(map[string]any{"itemId": *item.ID}))
			}
			return s, events, nil
		}
		return s, nil, nil
	}
}

func openaiResponsesOnResponseFinish(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	var events []schema.LLMEvent
	usage := openaiResponsesMapUsage(nil)
	if evt.Response != nil {
		usage = openaiResponsesMapUsage(evt.Response.Usage)
	}
	var pm schema.ProviderMetadata
	if evt.Response != nil && (evt.Response.ID != nil || evt.Response.ServiceTier != nil) {
		m := make(map[string]any)
		if evt.Response.ID != nil {
			m["responseId"] = *evt.Response.ID
		}
		if evt.Response.ServiceTier != nil {
			m["serviceTier"] = *evt.Response.ServiceTier
		}
		pm = openaiResponsesOpenAIMetadata(m)
	}
	utils.Finish(&s.lifecycle, &events, struct {
		Reason           schema.FinishReason
		Usage            *schema.Usage
		ProviderMetadata schema.ProviderMetadata
	}{Reason: openaiResponsesMapFinishReason(evt, s.hasFunctionCall), Usage: usage, ProviderMetadata: pm})
	return s, events
}

func openaiResponsesOnResponseFailed(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	msg := openaiResponsesProviderErrorMessage(evt, "OpenAI Responses response failed")
	events := []schema.LLMEvent{schema.NewProviderErrorEvent(msg)}
	return s, events
}

func openaiResponsesOnError(s openaiResponsesParserState, evt openaiResponsesResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	msg := openaiResponsesProviderErrorMessage(evt, "OpenAI Responses stream error")
	events := []schema.LLMEvent{schema.NewProviderErrorEvent(msg)}
	return s, events
}

func openaiResponsesProviderErrorMessage(evt openaiResponsesResponsesEvent, fallback string) string {
	var nested *openaiResponsesErrorPayload
	if evt.Response != nil {
		nested = evt.Response.Error
	}
	message := openaiResponsesStrVal(evt.Message)
	if message == "" && nested != nil {
		message = openaiResponsesStrVal(nested.Message)
	}
	code := openaiResponsesStrVal(evt.Code)
	if code == "" && nested != nil {
		code = openaiResponsesStrVal(nested.Code)
	}
	if message != "" && code != "" {
		return fmt.Sprintf("%s: %s", code, message)
	}
	if message != "" {
		return message
	}
	if code != "" {
		return code
	}
	return fallback
}

var OpenAIResponsesProtocol = route.Protocol{
	ID: openaiResponsesAdapter,
	Body: route.ProtocolBody{
		From: openaiResponsesFromRequest,
	},
	Stream: route.ProtocolStream{
		DecodeEvent: openaiResponsesDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return openaiResponsesParserState{
				tools:          utils.Empty(),
				lifecycle:      utils.Initial(),
				reasoningItems: make(map[string]*openaiResponsesReasoningStreamItem),
				store:          utils.Store(request),
			}
		},
		Step:     openaiResponsesStep,
		Terminal: openaiResponsesTerminal,
	},
}

var OpenAIResponsesHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        openaiResponsesAdapter,
	Provider:  "openai",
	Protocol:  OpenAIResponsesProtocol,
	Endpoint:  route.NewStaticPath(OpenAIResponsesPath, route.WithBaseURL(OpenAIResponsesDefaultBaseURL)),
	Transport: transport.SSEJson(),
})
