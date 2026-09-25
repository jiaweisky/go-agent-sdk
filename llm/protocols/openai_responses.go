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

// OpenAIResponsesDefaultBaseURL is the OpenAI API base URL.
const OpenAIResponsesDefaultBaseURL = "https://api.openai.com/v1"

// OpenAIResponsesPath is the Responses endpoint path.
const OpenAIResponsesPath = "/responses"

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

type openaiResponsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict"`
}

type openaiResponsesUsage struct {
	InputTokens         *int                                `json:"input_tokens,omitempty"`
	InputTokensDetails  *openaiResponsesInputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokens        *int                                `json:"output_tokens,omitempty"`
	OutputTokensDetails *openaiResponsesOutputTokensDetails `json:"output_tokens_details,omitempty"`
	TotalTokens         *int                                `json:"total_tokens,omitempty"`
	ProviderMetadata    schema.ProviderMetadata             `json:"-"`
}

// UnmarshalJSON reads the usage payload the Responses API sends.
func (u *openaiResponsesUsage) UnmarshalJSON(data []byte) error {
	type plain openaiResponsesUsage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = openaiResponsesUsage(decoded)
	u.ProviderMetadata = providerMetadata("openai", rawRecord(data))
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
	Usage             *openaiResponsesUsage             `json:"usage,omitempty"`
	Error             *openaiResponsesErrorPayload      `json:"error,omitempty"`
}
type openaiResponsesIncompleteDetails struct {
	Reason string `json:"reason"`
}

type openaiResponsesEvent struct {
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
	lifecycle       utils.LifecycleState
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

func openaiResponsesLowerTool(td schema.ToolDefinition, inputSchema any) openaiResponsesTool {
	return openaiResponsesTool{
		Type:        "function",
		Name:        td.Name,
		Description: td.Description,
		Parameters:  toolParameters(inputSchema),
		Strict:      false,
	}
}

func openaiResponsesLowerToolChoice(tc schema.ToolChoice) (any, error) {
	return MatchToolChoice("OpenAI Responses", tc,
		"auto",
		"none",
		"required",
		func(name string) any { return map[string]any{"type": "function", "name": name} },
	)
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
		media, err := utils.ValidateMedia("OpenAI Responses", p, imageMIMESet)
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
		media, err := utils.ValidateToolFile("OpenAI Responses", v, imageMIMESet)
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

func openaiResponsesReasoningMetadata(itemID string, encrypted *string) schema.ProviderMetadata {
	var value any
	if encrypted != nil {
		value = *encrypted
	}
	return providerMetadata("openai", map[string]any{
		"itemId":                    itemID,
		"reasoningEncryptedContent": value,
	})
}

func openaiResponsesLowerMessages(request *schema.LLMRequest) ([]openaiResponsesInputItem, error) {
	input := []openaiResponsesInputItem{}
	if len(request.System) > 0 {
		parts := make([]struct{ Text string }, len(request.System))
		for i, p := range request.System {
			parts[i].Text = p.Text
		}
		input = append(input, openaiResponsesSystemItem{Role: "system", Content: JoinText(parts)})
	}
	store := utils.Store(request)

	for _, msg := range request.Messages {
		switch msg.Role {
		case schema.RoleSystem:
			input = openaiResponsesLowerSystemMessage(input, msg)
		case schema.RoleUser:
			item, err := openaiResponsesLowerUserMessage(msg)
			if err != nil {
				return nil, err
			}
			input = append(input, item)
		case schema.RoleAssistant:
			next, err := openaiResponsesLowerAssistantMessage(input, msg, store)
			if err != nil {
				return nil, err
			}
			input = next
		case schema.RoleTool:
			items, err := openaiResponsesLowerToolMessage(msg)
			if err != nil {
				return nil, err
			}
			input = append(input, items...)
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

// openaiResponsesLowerSystemMessage appends a wrapped system update to a trailing
// user item, or starts a new user item.
func openaiResponsesLowerSystemMessage(input []openaiResponsesInputItem, msg schema.Message) []openaiResponsesInputItem {
	update := WrapSystemUpdate(SystemUpdateParts(msg.Content))
	text := openaiResponsesInputText{Type: "input_text", Text: update}
	if len(input) > 0 {
		if last, ok := input[len(input)-1].(openaiResponsesUserItem); ok {
			last.Content = append(last.Content, text)
			input[len(input)-1] = last
			return input
		}
	}
	return append(input, openaiResponsesUserItem{Role: "user", Content: []any{text}})
}

func openaiResponsesLowerUserMessage(msg schema.Message) (openaiResponsesUserItem, error) {
	userContent := []any{}
	for _, p := range msg.Content {
		c, err := openaiResponsesLowerUserContent(p)
		if err != nil {
			return openaiResponsesUserItem{}, err
		}
		userContent = append(userContent, c)
	}
	return openaiResponsesUserItem{Role: "user", Content: userContent}, nil
}

func openaiResponsesLowerAssistantMessage(input []openaiResponsesInputItem, msg schema.Message, store *bool) ([]openaiResponsesInputItem, error) {
	var textParts []string
	reasoningItems := map[string]*openaiResponsesReasoningItem{}
	var reasoningRefs map[string]bool
	var hostedRefs map[string]bool
	for _, p := range msg.Content {
		switch part := p.(type) {
		case schema.TextPart:
			textParts = append(textParts, part.Text)
		case schema.ReasoningPart:
			openaiResponsesFlushText(&input, &textParts)
			r := openaiResponsesLowerReasoning(part)
			if r == nil {
				continue
			}
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
			openaiResponsesFlushText(&input, &textParts)
			if part.ProviderExecuted != nil && *part.ProviderExecuted {
				continue
			}
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
		default:
			return nil, fmt.Errorf("OpenAI Responses assistant messages only support text, reasoning, tool-call and tool-result content")
		}
	}
	openaiResponsesFlushText(&input, &textParts)
	return input, nil
}

func openaiResponsesLowerToolMessage(msg schema.Message) ([]openaiResponsesInputItem, error) {
	var items []openaiResponsesInputItem
	for _, p := range msg.Content {
		part, ok := p.(schema.ToolResultPart)
		if !ok {
			return nil, fmt.Errorf("OpenAI Responses tool messages only support tool-result content")
		}
		output, err := openaiResponsesLowerToolResultOutput(part)
		if err != nil {
			return nil, err
		}
		items = append(items, openaiResponsesFunctionCallOutputItem{
			Type:   "function_call_output",
			CallID: part.ID,
			Output: output,
		})
	}
	return items, nil
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

func openaiResponsesLowerOptions(request *schema.LLMRequest) (map[string]any, error) {
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
	if effort != nil && !utils.IsReasoningEffort(*effort) {
		return nil, InvalidRequest(fmt.Sprintf("OpenAI Responses does not support reasoning effort %s", *effort))
	}
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
	return opts, nil
}

func openaiResponsesFromRequest(request *schema.LLMRequest) (any, error) {
	gen := request.Generation
	messages, err := openaiResponsesLowerMessages(request)
	if err != nil {
		return nil, err
	}
	opts, err := openaiResponsesLowerOptions(request)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":  string(request.Model.ModelID()),
		"input":  messages,
		"stream": true,
	}
	for k, v := range opts {
		body[k] = v
	}
	if len(request.Tools) > 0 {
		tools := make([]openaiResponsesTool, 0, len(request.Tools))
		for _, t := range request.Tools {
			tools = append(tools, openaiResponsesLowerTool(t, ProjectToolSchema(request.Model, t.InputSchema)))
		}
		body["tools"] = tools
	}
	if request.ToolChoice != nil {
		choice, err := openaiResponsesLowerToolChoice(*request.ToolChoice)
		if err != nil {
			return nil, err
		}
		body["tool_choice"] = choice
	}
	if gen != nil {
		if gen.MaxTokens != nil {
			body["max_output_tokens"] = *gen.MaxTokens
		}
		if gen.Temperature != nil {
			body["temperature"] = *gen.Temperature
		}
		if gen.TopP != nil {
			body["top_p"] = *gen.TopP
		}
	}
	return body, nil
}

func openaiResponsesDecodeEvent(frame string) (any, error) {
	if frame == "" || frame == "[DONE]" {
		return nil, nil
	}
	var evt openaiResponsesEvent
	if err := json.Unmarshal([]byte(frame), &evt); err != nil {
		return nil, fmt.Errorf("OpenAI Responses: invalid stream event: %s: %w", frame, err)
	}
	return evt, nil
}

func openaiResponsesMapUsage(u *openaiResponsesUsage) *schema.Usage {
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
	nonCached := intVal(u.InputTokens) - cached
	if nonCached < 0 {
		nonCached = 0
	}
	return &schema.Usage{
		InputTokens:          intVal(u.InputTokens),
		OutputTokens:         intVal(u.OutputTokens),
		NonCachedInputTokens: nonCached,
		CacheReadInputTokens: cached,
		ReasoningTokens:      reasoning,
		TotalTokens:          intVal(TotalTokens(u.InputTokens, u.OutputTokens, u.TotalTokens)),
		ProviderMetadata:     u.ProviderMetadata,
	}
}

func openaiResponsesMapFinishReason(evt openaiResponsesEvent, hasFunctionCall bool) schema.FinishReason {
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
	if !ok || item.ID == nil || *item.ID == "" {
		return openaiResponsesHostedToolDef{}, false
	}
	return t, true
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
	pm := providerMetadata("openai", map[string]any{"itemId": strVal(item.ID)})
	return []schema.LLMEvent{
		schema.NewToolCall(strVal(item.ID), tool.Name, tool.Input(item),
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)),
		schema.NewToolResult(strVal(item.ID), tool.Name, openaiResponsesHostedResult(item),
			schema.WithEventProviderExecuted(true), schema.WithEventProviderMetadata(pm)),
	}
}

func openaiResponsesOnOutputTextDelta(state openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	if evt.Delta == nil {
		return state, nil
	}
	var events []schema.LLMEvent
	itemID := strVal(evt.ItemID)
	if itemID == "" {
		itemID = "text-0"
	}
	utils.TextDelta(&state.lifecycle, &events, itemID, *evt.Delta)
	return state, events
}

func openaiResponsesOnReasoningDelta(state openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	if evt.Delta == nil {
		return state, nil
	}
	var events []schema.LLMEvent
	itemID := strVal(evt.ItemID)
	if itemID == "" {
		itemID = "reasoning-0"
	}
	id := itemID
	if _, tracked := state.reasoningItems[itemID]; evt.SummaryIndex != nil || tracked {
		id = fmt.Sprintf("%s:%d", itemID, intVal(evt.SummaryIndex))
	}
	utils.ReasoningDelta(&state.lifecycle, &events, id, *evt.Delta)
	return state, events
}

func openaiResponsesStep(state any, event any) (any, []schema.LLMEvent, error) {
	s := state.(openaiResponsesParserState)
	evt, ok := event.(openaiResponsesEvent)
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
	evt, ok := event.(openaiResponsesEvent)
	if !ok {
		return false
	}
	return openaiResponsesTerminalTypes[evt.Type]
}

func openaiResponsesOnReasoningSummaryPartAdded(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
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
					providerMetadata("openai", map[string]any{"itemId": itemID}))
				existing.SummaryParts[k] = "concluded"
			}
		}
	}
	utils.ReasoningStart(&s.lifecycle, &events, fmt.Sprintf("%s:%d", itemID, idx),
		openaiResponsesReasoningMetadata(itemID, existing.EncryptedContent))
	existing.SummaryParts[idx] = "active"
	return s, events, nil
}

func openaiResponsesOnReasoningSummaryPartDone(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
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
			providerMetadata("openai", map[string]any{"itemId": itemID}))
		existing.SummaryParts[*evt.SummaryIndex] = "concluded"
	} else {
		existing.SummaryParts[*evt.SummaryIndex] = "can-conclude"
	}
	return s, events, nil
}

func openaiResponsesOnOutputItemAdded(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
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
	pm := providerMetadata("openai", map[string]any{"itemId": *item.ID})
	var events []schema.LLMEvent
	utils.StepStart(&s.lifecycle, &events)
	callID := strVal(item.CallID)
	if callID == "" {
		callID = *item.ID
	}
	s.tools = utils.ToolStreamStart(s.tools, *item.ID, utils.PendingTool{
		ID:               callID,
		Name:             strVal(item.Name),
		Input:            strVal(item.Arguments),
		ProviderMetadata: pm,
	})
	events = append(events, schema.NewToolInputStart(callID, strVal(item.Name), schema.WithEventProviderMetadata(pm)))
	return s, events, nil
}

func openaiResponsesOnFunctionCallArgumentsDelta(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
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

func openaiResponsesOnOutputItemDone(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent, error) {
	item := evt.Item
	if item == nil {
		return s, nil, nil
	}
	if item.Type == "function_call" {
		return openaiResponsesOnFunctionCallDone(s, *item)
	}
	if t, ok := openaiResponsesIsHostedTool(*item); ok {
		return openaiResponsesOnHostedToolDone(s, *item, t)
	}
	if openaiResponsesIsReasoningItem(*item) {
		return openaiResponsesOnReasoningItemDone(s, *item)
	}
	return s, nil, nil
}

func openaiResponsesOnFunctionCallDone(s openaiResponsesParserState, item openaiResponsesStreamItem) (openaiResponsesParserState, []schema.LLMEvent, error) {
	if item.ID == nil || item.CallID == nil || item.Name == nil {
		return s, nil, nil
	}
	if s.tools[*item.ID] == nil {
		s.tools = utils.ToolStreamStart(s.tools, *item.ID, utils.PendingTool{
			ID:   *item.CallID,
			Name: *item.Name,
		})
	}
	var outcome utils.FinishOutcome
	var err error
	if item.Arguments != nil {
		outcome, err = utils.FinishToolWithInput(openaiResponsesAdapter, s.tools, *item.ID, *item.Arguments)
	} else {
		outcome, err = utils.FinishTool(openaiResponsesAdapter, s.tools, *item.ID)
	}
	if err != nil {
		return s, nil, err
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
}

func openaiResponsesOnHostedToolDone(s openaiResponsesParserState, item openaiResponsesStreamItem, t openaiResponsesHostedToolDef) (openaiResponsesParserState, []schema.LLMEvent, error) {
	var events []schema.LLMEvent
	utils.StepStart(&s.lifecycle, &events)
	events = append(events, openaiResponsesHostedEvents(item, t)...)
	return s, events, nil
}

func openaiResponsesOnReasoningItemDone(s openaiResponsesParserState, item openaiResponsesStreamItem) (openaiResponsesParserState, []schema.LLMEvent, error) {
	var events []schema.LLMEvent
	if ri := s.reasoningItems[*item.ID]; ri != nil {
		for k, v := range ri.SummaryParts {
			if v == "active" || v == "can-conclude" {
				utils.ReasoningEnd(&s.lifecycle, &events, fmt.Sprintf("%s:%d", *item.ID, k),
					openaiResponsesReasoningMetadata(*item.ID, item.EncryptedContent))
			}
		}
		delete(s.reasoningItems, *item.ID)
		return s, events, nil
	}
	pm := providerMetadata("openai", map[string]any{"itemId": *item.ID})
	if _, ok := s.lifecycle.Reasoning[*item.ID]; !ok {
		utils.StepStart(&s.lifecycle, &events)
		events = append(events, schema.NewReasoningStart(*item.ID, schema.WithEventProviderMetadata(pm)))
		events = append(events, schema.NewReasoningEnd(*item.ID, schema.WithEventProviderMetadata(pm)))
		return s, events, nil
	}
	utils.ReasoningEnd(&s.lifecycle, &events, *item.ID, pm)
	return s, events, nil
}

func openaiResponsesOnResponseFinish(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
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
		pm = providerMetadata("openai", m)
	}
	utils.LifecycleFinish(&s.lifecycle, &events, utils.FinishInput{
		Reason:           openaiResponsesMapFinishReason(evt, s.hasFunctionCall),
		Usage:            usage,
		ProviderMetadata: pm,
	})
	return s, events
}

func openaiResponsesOnResponseFailed(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	return s, []schema.LLMEvent{openaiResponsesProviderError(evt, "OpenAI Responses response failed")}
}

func openaiResponsesOnError(s openaiResponsesParserState, evt openaiResponsesEvent) (openaiResponsesParserState, []schema.LLMEvent) {
	return s, []schema.LLMEvent{openaiResponsesProviderError(evt, "OpenAI Responses stream error")}
}

func openaiResponsesProviderError(evt openaiResponsesEvent, fallback string) schema.LLMEvent {
	code := strVal(evt.Code)
	if code == "" && evt.Response != nil && evt.Response.Error != nil {
		code = strVal(evt.Response.Error.Code)
	}
	message := openaiResponsesProviderErrorMessage(evt, fallback)
	opts := []schema.EventOption{}
	if code == "context_length_exceeded" || schema.IsContextOverflow(message) {
		opts = append(opts, schema.WithEventClassification(schema.ClassificationContextOverflow))
	}
	return schema.NewProviderErrorEvent(message, opts...)
}

func openaiResponsesProviderErrorMessage(evt openaiResponsesEvent, fallback string) string {
	var nested *openaiResponsesErrorPayload
	if evt.Response != nil {
		nested = evt.Response.Error
	}
	message := strVal(evt.Message)
	if message == "" && nested != nil {
		message = strVal(nested.Message)
	}
	code := strVal(evt.Code)
	if code == "" && nested != nil {
		code = strVal(nested.Code)
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

// OpenAIResponsesProtocol is the OpenAI Responses wire protocol.
var OpenAIResponsesProtocol = route.Protocol{
	ID: openaiResponsesAdapter,
	Body: route.ProtocolBody{
		Schema: openaiResponsesBodySchema,
		From:   openaiResponsesFromRequest,
	},
	Stream: route.ProtocolStream{
		DecodeEvent: openaiResponsesDecodeEvent,
		Initial: func(request *schema.LLMRequest) any {
			return openaiResponsesParserState{
				tools:          utils.ToolStreamEmpty(),
				lifecycle:      utils.LifecycleInitial(),
				reasoningItems: make(map[string]*openaiResponsesReasoningStreamItem),
				store:          utils.Store(request),
			}
		},
		Step:     openaiResponsesStep,
		Terminal: openaiResponsesTerminal,
	},
}

// OpenAIResponsesHTTPRoute is the OpenAI Responses route.
var OpenAIResponsesHTTPRoute = route.MakeFromTransport(route.MakeTransportInput{
	ID:        openaiResponsesAdapter,
	Provider:  "openai",
	Protocol:  OpenAIResponsesProtocol,
	Endpoint:  route.NewStaticPath(OpenAIResponsesPath, route.WithBaseURL(OpenAIResponsesDefaultBaseURL)),
	Transport: transport.SSEJson(),
})
