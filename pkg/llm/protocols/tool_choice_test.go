package protocols

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestMatchToolChoice(t *testing.T) {
	name := "lookup"
	tool := func(value string) any { return "tool:" + value }

	got, err := MatchToolChoice("Test", schema.ToolChoice{Type: "auto"}, "auto", nil, "required", tool)
	if err != nil || got != "auto" {
		t.Fatalf("auto = %#v, err = %v", got, err)
	}
	if got, err := MatchToolChoice("Test", schema.ToolChoice{Type: "none"}, "auto", nil, "required", tool); err != nil || got != nil {
		t.Fatalf("none = %#v, err = %v", got, err)
	}
	if got, err := MatchToolChoice("Test", schema.ToolChoice{Type: "required"}, "auto", nil, "required", tool); err != nil || got != "required" {
		t.Fatalf("required = %#v, err = %v", got, err)
	}
	if got, err := MatchToolChoice("Test", schema.ToolChoice{Type: "tool", Name: &name}, "auto", nil, "required", tool); err != nil || got != "tool:lookup" {
		t.Fatalf("tool = %#v, err = %v", got, err)
	}

	empty := ""
	for _, choice := range []schema.ToolChoice{{Type: "tool"}, {Type: "tool", Name: &empty}} {
		_, err := MatchToolChoice("Test", choice, "auto", nil, "required", tool)
		if err == nil || !strings.Contains(err.Error(), "requires a tool name") {
			t.Fatalf("choice %+v: err = %v, want a missing-name error", choice, err)
		}
	}
}

func TestToolChoiceRequiresName(t *testing.T) {
	choices := []schema.ToolChoice{{Type: "tool"}}
	lowerers := map[string]func(schema.ToolChoice) (any, error){
		"anthropic":        anthropicMessagesLowerToolChoice,
		"bedrock":          bedrockConverseLowerToolChoice,
		"gemini":           func(c schema.ToolChoice) (any, error) { return geminiLowerToolConfig(c) },
		"openai-chat":      openaiChatLowerToolChoice,
		"openai-responses": openaiResponsesLowerToolChoice,
	}
	for name, lower := range lowerers {
		for _, choice := range choices {
			if _, err := lower(choice); err == nil {
				t.Fatalf("%s: want an error for %+v", name, choice)
			}
		}
	}
}

func TestAnthropicToolChoiceModes(t *testing.T) {
	name := "lookup"
	cases := []struct {
		choice schema.ToolChoice
		want   any
	}{
		{schema.ToolChoice{Type: "auto"}, anthropicMessagesToolChoice{Type: "auto"}},
		{schema.ToolChoice{Type: "none"}, nil},
		{schema.ToolChoice{Type: "required"}, anthropicMessagesToolChoice{Type: "any"}},
		{schema.ToolChoice{Type: "tool", Name: &name}, anthropicMessagesToolChoice{Type: "tool", Name: "lookup"}},
	}
	for _, tc := range cases {
		got, err := anthropicMessagesLowerToolChoice(tc.choice)
		if err != nil {
			t.Fatalf("%+v: %v", tc.choice, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%+v = %#v, want %#v", tc.choice, got, tc.want)
		}
	}
}

func TestOpenAIToolChoiceModes(t *testing.T) {
	name := "lookup"
	// A name on a mode choice must not turn it into a named tool.
	chat, err := openaiChatLowerToolChoice(schema.ToolChoice{Type: "auto", Name: &name})
	if err != nil || chat != "auto" {
		t.Fatalf("chat auto = %#v, err = %v", chat, err)
	}
	responses, err := openaiResponsesLowerToolChoice(schema.ToolChoice{Type: "auto", Name: &name})
	if err != nil || responses != "auto" {
		t.Fatalf("responses auto = %#v, err = %v", responses, err)
	}

	chat, err = openaiChatLowerToolChoice(schema.ToolChoice{Type: "tool", Name: &name})
	if err != nil {
		t.Fatalf("chat tool: %v", err)
	}
	wantChat := openaiChatToolChoice{Type: "function", Function: &openaiChatToolChoiceFunc{Name: "lookup"}}
	if !reflect.DeepEqual(chat, wantChat) {
		t.Fatalf("chat tool = %#v, want %#v", chat, wantChat)
	}
	responses, err = openaiResponsesLowerToolChoice(schema.ToolChoice{Type: "tool", Name: &name})
	if err != nil {
		t.Fatalf("responses tool: %v", err)
	}
	if !reflect.DeepEqual(responses, map[string]any{"type": "function", "name": "lookup"}) {
		t.Fatalf("responses tool = %#v", responses)
	}
}

func TestBedrockAndGeminiToolChoiceModes(t *testing.T) {
	name := "lookup"
	bedrock, err := bedrockConverseLowerToolChoice(schema.ToolChoice{Type: "required"})
	if err != nil {
		t.Fatalf("bedrock: %v", err)
	}
	encoded, err := json.Marshal(bedrock)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `{"any":{}}` {
		t.Fatalf("bedrock required = %s", encoded)
	}

	gemini, err := geminiLowerToolConfig(schema.ToolChoice{Type: "tool", Name: &name})
	if err != nil {
		t.Fatalf("gemini: %v", err)
	}
	if gemini.FunctionCallingConfig.Mode != "ANY" || len(gemini.FunctionCallingConfig.AllowedFunctionNames) != 1 ||
		gemini.FunctionCallingConfig.AllowedFunctionNames[0] != "lookup" {
		t.Fatalf("gemini tool = %+v", gemini)
	}
}
