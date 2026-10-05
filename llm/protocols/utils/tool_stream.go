package utils

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// ParseToolInput decodes a tool call's JSON input.
func ParseToolInput(route, name, raw string) (any, error) {
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

// EventError builds the shared invalid-provider-output error.
func EventError(route, message string, raw ...string) *schema.LLMError {
	opts := []schema.ReasonOption{schema.WithRoute(route)}
	if len(raw) > 0 {
		opts = append(opts, schema.WithRaw(raw[0]))
	}
	return &schema.LLMError{
		Module: "ProviderShared",
		Method: "stream",
		Reason: schema.NewInvalidProviderOutputReason(message, opts...),
	}
}

// PendingTool is a tool call being accumulated: its id, name and arguments.
type PendingTool struct {
	ID               string
	Name             string
	Input            string
	ProviderExecuted *bool
	ProviderMetadata schema.ProviderMetadata
}

// AppendOutcome is what appending a chunk produced: the updated tools and the
// events it completed.
type AppendOutcome struct {
	Tools  map[string]*PendingTool
	Tool   *PendingTool
	Events []schema.LLMEvent
}

// FinishOutcome is what finishing a tool call produced.
type FinishOutcome struct {
	Tools  map[string]*PendingTool
	Events []schema.LLMEvent
}

// ToolStreamEmpty returns an empty set of pending tool calls.
func ToolStreamEmpty() map[string]*PendingTool {
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
	return schema.NewToolInputStart(tool.ID, tool.Name, providerMetadataOptions(tool.ProviderMetadata)...)
}

func inputDelta(tool *PendingTool, text string) schema.LLMEvent {
	return schema.NewToolInputDelta(tool.ID, tool.Name, text)
}

func toolCall(route string, tool *PendingTool, inputOverride ...string) (schema.LLMEvent, error) {
	input := tool.Input
	if len(inputOverride) > 0 {
		input = inputOverride[0]
	}
	parsed, err := ParseToolInput(route, tool.Name, input)
	if err != nil {
		return schema.LLMEvent{}, err
	}
	opts := []schema.EventOption{}
	if tool.ProviderExecuted != nil && *tool.ProviderExecuted {
		opts = append(opts, schema.WithEventProviderExecuted(true))
	}
	opts = append(opts, providerMetadataOptions(tool.ProviderMetadata)...)
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

// ToolStreamStart records a tool call that has just begun.
func ToolStreamStart(tools map[string]*PendingTool, key string, tool PendingTool) map[string]*PendingTool {
	return start(tools, key, tool)
}

// AppendOrStart appends a chunk to a pending tool call, starting it when it is
// new.
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
		return AppendOutcome{}, EventError(route, missingToolMessage)
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

// AppendExisting appends a chunk to a tool call that must already exist.
func AppendExisting(
	route string,
	tools map[string]*PendingTool,
	key string,
	text string,
	missingToolMessage string,
) (AppendOutcome, *schema.LLMError) {
	current := tools[key]
	if current == nil {
		return AppendOutcome{}, EventError(route, missingToolMessage)
	}
	if len(text) == 0 {
		return AppendOutcome{Tools: tools, Tool: current, Events: nil}, nil
	}
	next := *current
	next.Input = current.Input + text
	return appendTool(tools, key, &next, text), nil
}

// FinishTool completes a tool call and returns the events it produced.
func FinishTool(route string, tools map[string]*PendingTool, key string) (FinishOutcome, error) {
	tool := tools[key]
	if tool == nil {
		return FinishOutcome{Tools: tools}, nil
	}
	call, err := toolCall(route, tool)
	if err != nil {
		return FinishOutcome{}, err
	}
	opts := providerMetadataOptions(tool.ProviderMetadata)
	return FinishOutcome{
		Tools: withoutTool(tools, key),
		Events: []schema.LLMEvent{
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		},
	}, nil
}

// FinishToolWithInput completes a tool call whose arguments arrived whole.
func FinishToolWithInput(route string, tools map[string]*PendingTool, key string, input string) (FinishOutcome, error) {
	tool := tools[key]
	if tool == nil {
		return FinishOutcome{Tools: tools}, nil
	}
	call, err := toolCall(route, tool, input)
	if err != nil {
		return FinishOutcome{}, err
	}
	opts := providerMetadataOptions(tool.ProviderMetadata)
	return FinishOutcome{
		Tools: withoutTool(tools, key),
		Events: []schema.LLMEvent{
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		},
	}, nil
}

// FinishAllTools completes every pending tool call. Numeric keys are ordered
// numerically, other keys lexicographically.
func FinishAllTools(route string, tools map[string]*PendingTool) (FinishOutcome, error) {
	keys := make([]string, 0, len(tools))
	for key := range tools {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, iErr := strconv.Atoi(keys[i])
		nj, jErr := strconv.Atoi(keys[j])
		switch {
		case iErr == nil && jErr == nil:
			return ni < nj
		case iErr == nil:
			return true
		case jErr == nil:
			return false
		default:
			return keys[i] < keys[j]
		}
	})
	var allEvents []schema.LLMEvent
	for _, key := range keys {
		tool := tools[key]
		if tool == nil {
			continue
		}
		call, err := toolCall(route, tool)
		if err != nil {
			return FinishOutcome{}, err
		}
		opts := providerMetadataOptions(tool.ProviderMetadata)
		allEvents = append(allEvents,
			schema.NewToolInputEnd(tool.ID, tool.Name, opts...),
			call,
		)
	}
	return FinishOutcome{
		Tools:  ToolStreamEmpty(),
		Events: allEvents,
	}, nil
}
