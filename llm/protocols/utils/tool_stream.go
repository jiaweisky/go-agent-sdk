package utils

import (
	"encoding/json"
	"fmt"

	"github.com/user/go-agent-sdk/llm/schema"
)

func parseToolInput(route, name, raw string) (any, error) {
	input := raw
	if input == "" {
		input = "{}"
	}
	var result any
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		return nil, fmt.Errorf("Invalid JSON input for %s tool call %s: %w", route, name, err)
	}
	return result, nil
}

func eventError(route, message string, raw ...string) *schema.LLMError {
	opts := []schema.ReasonOption{schema.WithRoute(route)}
	if len(raw) > 0 {
		opts = append(opts, schema.WithRaw(raw[0]))
	}
	return &schema.LLMError{
		Module: "ToolStream",
		Method: "stream",
		Reason: schema.NewInvalidProviderOutputReason(message, opts...),
	}
}

type PendingTool struct {
	ID               string
	Name             string
	Input            string
	ProviderExecuted *bool
	ProviderMetadata schema.ProviderMetadata
}

type AppendOutcome struct {
	Tools  map[string]*PendingTool
	Tool   *PendingTool
	Events []schema.LLMEvent
}

type FinishOutcome struct {
	Tools  map[string]*PendingTool
	Events []schema.LLMEvent
}

func Empty() map[string]*PendingTool {
	return make(map[string]*PendingTool)
}

func start(tools map[string]*PendingTool, key string, tool PendingTool) map[string]*PendingTool {
	result := make(map[string]*PendingTool, len(tools)+1)
	for k, v := range tools {
		result[k] = v
	}
	result[key] = &tool
	return result
}

func withoutTool(tools map[string]*PendingTool, key string) map[string]*PendingTool {
	result := make(map[string]*PendingTool, len(tools))
	for k, v := range tools {
		if k != key {
			result[k] = v
		}
	}
	return result
}

func inputStart(tool *PendingTool) schema.LLMEvent {
	opts := []schema.EventOption{}
	if tool.ProviderMetadata != nil {
		opts = append(opts, schema.WithEventProviderMetadata(tool.ProviderMetadata))
	}
	return schema.NewToolInputStart(tool.ID, tool.Name, opts...)
}

func inputDelta(tool *PendingTool, text string) schema.LLMEvent {
	return schema.NewToolInputDelta(tool.ID, tool.Name, text)
}

func toolCall(route string, tool *PendingTool, inputOverride ...string) (schema.LLMEvent, error) {
	input := tool.Input
	if len(inputOverride) > 0 {
		input = inputOverride[0]
	}
	parsed, err := parseToolInput(route, tool.Name, input)
	if err != nil {
		return schema.LLMEvent{}, err
	}
	opts := []schema.EventOption{}
	if tool.ProviderExecuted != nil && *tool.ProviderExecuted {
		opts = append(opts, schema.WithEventProviderExecuted(true))
	}
	if tool.ProviderMetadata != nil {
		opts = append(opts, schema.WithEventProviderMetadata(tool.ProviderMetadata))
	}
	return schema.NewToolCall(tool.ID, tool.Name, parsed, opts...), nil
}

func appendTool(tools map[string]*PendingTool, key string, tool *PendingTool, text string) AppendOutcome {
	events := []schema.LLMEvent{}
	if tools[key] == nil {
		events = append(events, inputStart(tool))
	}
	if len(text) > 0 {
		events = append(events, inputDelta(tool, text))
	}
	return AppendOutcome{
		Tools:  start(tools, key, *tool),
		Tool:   tool,
		Events: events,
	}
}

func Start(tools map[string]*PendingTool, key string, tool PendingTool) map[string]*PendingTool {
	return start(tools, key, tool)
}

func AppendOrStart(
	route string,
	tools map[string]*PendingTool,
	key string,
	delta struct {
		ID   string
		Name string
		Text string
	},
	missingToolMessage string,
) (AppendOutcome, *schema.LLMError) {
	current := tools[key]
	id := delta.ID
	if id == "" && current != nil {
		id = current.ID
	}
	name := delta.Name
	if name == "" && current != nil {
		name = current.Name
	}
	if id == "" || name == "" {
		return AppendOutcome{}, eventError(route, missingToolMessage)
	}
	tool := &PendingTool{
		ID:               id,
		Name:             name,
		Input:            "",
		ProviderExecuted: nil,
		ProviderMetadata: nil,
	}
	if current != nil {
		tool.Input = current.Input + delta.Text
		tool.ProviderExecuted = current.ProviderExecuted
		tool.ProviderMetadata = current.ProviderMetadata
	} else {
		tool.Input = delta.Text
	}
	if current != nil && len(delta.Text) == 0 && current.ID == id && current.Name == name {
		return AppendOutcome{Tools: tools, Tool: current, Events: nil}, nil
	}
	r := appendTool(tools, key, tool, delta.Text)
	return r, nil
}

func AppendExisting(
	route string,
	tools map[string]*PendingTool,
	key string,
	text string,
	missingToolMessage string,
) (AppendOutcome, *schema.LLMError) {
	current := tools[key]
	if current == nil {
		return AppendOutcome{}, eventError(route, missingToolMessage)
	}
	if len(text) == 0 {
		return AppendOutcome{Tools: tools, Tool: current, Events: nil}, nil
	}
	next := *current
	next.Input = current.Input + text
	return appendTool(tools, key, &next, text), nil
}

func FinishTool(route string, tools map[string]*PendingTool, key string) (FinishOutcome, error) {
	tool := tools[key]
	if tool == nil {
		return FinishOutcome{Tools: tools}, nil
	}
	call, err := toolCall(route, tool)
	if err != nil {
		return FinishOutcome{}, err
	}
	opts := []schema.EventOption{}
	if tool.ProviderMetadata != nil {
		opts = append(opts, schema.WithEventProviderMetadata(tool.ProviderMetadata))
	}
	return FinishOutcome{
		Tools: withoutTool(tools, key),
		Events: []schema.LLMEvent{
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		},
	}, nil
}

func FinishToolWithInput(route string, tools map[string]*PendingTool, key string, input string) (FinishOutcome, error) {
	tool := tools[key]
	if tool == nil {
		return FinishOutcome{Tools: tools}, nil
	}
	call, err := toolCall(route, tool, input)
	if err != nil {
		return FinishOutcome{}, err
	}
	opts := []schema.EventOption{}
	if tool.ProviderMetadata != nil {
		opts = append(opts, schema.WithEventProviderMetadata(tool.ProviderMetadata))
	}
	return FinishOutcome{
		Tools: withoutTool(tools, key),
		Events: []schema.LLMEvent{
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		},
	}, nil
}

func FinishAllTools(route string, tools map[string]*PendingTool) (FinishOutcome, error) {
	var allEvents []schema.LLMEvent
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		call, err := toolCall(route, tool)
		if err != nil {
			return FinishOutcome{}, err
		}
		opts := []schema.EventOption{}
		if tool.ProviderMetadata != nil {
			opts = append(opts, schema.WithEventProviderMetadata(tool.ProviderMetadata))
		}
		allEvents = append(allEvents,
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		)
	}
	return FinishOutcome{
		Tools:  Empty(),
		Events: allEvents,
	}, nil
}
