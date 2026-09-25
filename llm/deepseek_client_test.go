package llm_test

import (
	"context"
	"iter"
	"net/http"
	"os"
	"testing"

	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/provider"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

type testClient struct {
	Route route.Route
	RT    route.TransportRuntime
}

func (c *testClient) Prepare(ctx context.Context, req *schema.LLMRequest) (*schema.PreparedRequest, error) {
	body, err := c.Route.Body.From(req)
	if err != nil {
		return nil, err
	}
	_, err = c.Route.PrepareTransport(body, req)
	if err != nil {
		return nil, err
	}
	return &schema.PreparedRequest{
		Model: req.Model,
		Route: schema.RouteID(c.Route.ID),
	}, nil
}

func (c *testClient) Stream(ctx context.Context, req *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error) {
	body, err := c.Route.Body.From(req)
	if err != nil {
		return nil, err
	}
	prepared, err := c.Route.PrepareTransport(body, req)
	if err != nil {
		return nil, err
	}
	return func(yield func(schema.LLMEvent, error) bool) {
		for evt, streamErr := range c.Route.StreamPrepared(ctx, prepared, req, c.RT) {
			if streamErr != nil {
				yield(schema.LLMEvent{}, streamErr)
				return
			}
			if !yield(evt.(schema.LLMEvent), nil) {
				return
			}
		}
	}, nil
}

func (c *testClient) Generate(ctx context.Context, req *schema.LLMRequest) (*schema.LLMResponse, error) {
	stream, err := c.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var state = schema.EmptyResponseState()
	for evt, err := range stream {
		if err != nil {
			return nil, err
		}
		schema.ReduceResponseState(&state, evt)
	}
	return schema.LLMResponseComplete(&state), nil
}

func chatClient() (*testClient, route.Route) {
	rt := route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)}
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer(os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
	return &testClient{Route: r, RT: rt}, r
}

func ptr[T any](v T) *T { return &v }

func TestClient_StreamAndGenerate(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	client, _ := chatClient()
	req := llm.NewRequest(llm.RequestInput{
		Model:  schema.ModelID("deepseek-flash"),
		Prompt: "Say hello in one word",
	})

	t.Run("Generate", func(t *testing.T) {
		resp, err := client.Generate(context.Background(), req)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp == nil {
			t.Fatal("response is nil")
		}
		if resp.Text() == "" {
			t.Error("response text is empty")
		}
	})
}

func TestClient_GenerateObject(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	client, _ := chatClient()
	jsonSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city":    map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
		},
		"required": []any{"city", "country"},
	}
	resp, err := llm.GenerateObject(context.Background(), client, llm.GenerateObjectOptions{
		Request: llm.RequestInput{
			Model:  schema.ModelID("deepseek-flash"),
			Prompt: "What is the capital of France? Return as JSON with keys: city, country",
			ProviderOptions: schema.ProviderOptions{
				"openai": {"reasoningEffort": "none"},
			},
		},
		Schema: jsonschema.Raw(jsonSchema),
	})
	if err != nil {
		t.Fatalf("GenerateObject: %v", err)
	}
	if resp.Object == nil {
		t.Fatal("object is nil")
	}
	if resp.Response == nil {
		t.Fatal("response is nil")
	}
}

func TestProvider_OpenAI_Chat(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	prov := provider.NewOpenAI(protocols.OpenAIChatHTTPRoute, protocols.OpenAIChatHTTPRoute, provider.OpenAIConfig{
		CommonConfig: provider.CommonConfig{APIKey: &apiKey, BaseURL: "https://api.deepseek.com/v1"},
	})
	model := prov.Model("deepseek-flash")
	req := llm.NewRequest(llm.RequestInput{
		Model:  model,
		Prompt: "Hello",
	})
	client, _ := chatClient()
	resp, err := client.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Text() == "" {
		t.Error("response text is empty")
	}
}
