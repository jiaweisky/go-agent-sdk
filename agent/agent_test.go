package agent

import (
	"context"
	"iter"
	"reflect"
	"testing"

	llmschema "github.com/user/go-agent-sdk/llm/schema"
)

type fakeLLM struct{ reply string }

func (f fakeLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f fakeLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f fakeLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", f.reply),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
		llmschema.NewFinish(llmschema.ReasonStop),
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(&ClientOptions{
		Provider:   ProviderConfig{Type: "openai", APIKey: "test"},
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		llm:        fakeLLM{reply: "hello world"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	return client
}

func TestSessionSend(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var text string
	var idle bool
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case TextDelta:
			text += e.Delta
		case Idle:
			idle = true
		}
	}
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
	if !idle {
		t.Fatal("expected Idle event")
	}
}

func TestSessionOn(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var observed int
	unsubscribe := session.On(func(event Event) {
		if _, ok := event.(TextDelta); ok {
			observed++
		}
	})
	defer unsubscribe()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if observed == 0 {
		t.Fatal("expected On handler to observe a TextDelta")
	}
}

func TestDefineToolSchema(t *testing.T) {
	type params struct {
		City string `json:"city" jsonschema:"city name"`
		Unit string `json:"unit,omitempty"`
	}
	definition := SchemaForType(reflect.TypeOf((*params)(nil)).Elem())
	model := definition.Model()
	if model["additionalProperties"] != false {
		t.Fatalf("model = %#v", model)
	}
	required := model["required"].([]any)
	if len(required) != 1 || required[0] != "city" {
		t.Fatalf("required = %#v", required)
	}
}
