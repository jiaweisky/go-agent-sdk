package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
	"github.com/user/go-agent-sdk/llm/schema"
)

func TestPrepareValidatesProtocolBody(t *testing.T) {
	cases := []struct {
		name  string
		route route.Route
	}{
		{"openai-chat", protocols.OpenAIChatHTTPRoute},
		{"openai-responses", protocols.OpenAIResponsesHTTPRoute},
		{"anthropic", protocols.AnthropicMessagesHTTPRoute},
		{"gemini", protocols.GeminiHTTPRoute},
	}
	client := llm.NewClient(route.TransportRuntime{})
	for _, tc := range cases {
		r := tc.route.With(route.RoutePatch{Provider: "test"})
		request := &schema.LLMRequest{
			Model: r.Model(route.RouteModelInput{ID: "test-model"}),
			Messages: []schema.Message{{Role: schema.RoleUser, Content: []schema.ContentPart{
				schema.TextPart{Type: "text", Text: "hello"},
			}}},
		}
		if _, err := client.Prepare(context.Background(), request); err != nil {
			t.Fatalf("%s: prepare: %v", tc.name, err)
		}
	}
}

func TestPrepareRejectsBodyFailingItsSchema(t *testing.T) {
	protocol := route.MakeProtocol(route.Protocol{
		ID: "test-schema",
		Body: route.ProtocolBody{
			Schema: jsonschema.Object(jsonschema.Required("model", jsonschema.String())),
			From:   func(*schema.LLMRequest) (any, error) { return map[string]any{}, nil },
		},
	})
	r := route.MakeFromTransport(route.MakeTransportInput{
		ID:        "test-schema",
		Provider:  "test",
		Protocol:  protocol,
		Endpoint:  route.NewStaticPath("/v1", route.WithBaseURL("https://api.test")),
		Transport: transport.SSEJson(),
	})

	client := llm.NewClient(route.TransportRuntime{})
	_, err := client.Prepare(context.Background(), &schema.LLMRequest{
		Model: r.Model(route.RouteModelInput{ID: "test-model"}),
	})
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("err = %v, want a body validation error", err)
	}
}
