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

func TestSessionListResumeDelete(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	session, err := client.CreateSession(ctx, &SessionConfig{Agent: "build", Title: "one"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	infos, err := client.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != session.ID() || infos[0].Title != "one" {
		t.Fatalf("infos = %+v", infos)
	}

	resumed, err := client.ResumeSession(ctx, session.ID(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer resumed.Close()
	if resumed.ID() != session.ID() {
		t.Fatalf("resumed id = %s", resumed.ID())
	}

	if err := client.DeleteSession(ctx, session.ID()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	infos, err = client.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("infos after delete = %+v", infos)
	}
}

func TestSessionToolIsolation(t *testing.T) {
	client := newTestClient(t)
	toolA := DefineTool("shared", "from A", func(struct{}, ToolContext) (string, error) { return "a", nil })
	toolB := DefineTool("shared", "from B", func(struct{}, ToolContext) (string, error) { return "b", nil })

	first, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{toolA}})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	defer first.Close()
	second, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{toolB}})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	defer second.Close()

	if got := sharedToolDescription(client.rt.dispatcher.MaterializeFor(first.ID(), nil).Definitions); got != "from A" {
		t.Fatalf("first session tool = %q", got)
	}
	if got := sharedToolDescription(client.rt.dispatcher.MaterializeFor(second.ID(), nil).Definitions); got != "from B" {
		t.Fatalf("second session tool = %q", got)
	}
}

func sharedToolDescription(definitions []llmschema.ToolDefinition) string {
	for _, definition := range definitions {
		if definition.Name == "shared" {
			return definition.Description
		}
	}
	return ""
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
