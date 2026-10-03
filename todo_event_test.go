package agent

import (
	"context"
	"iter"
	"sync"
	"testing"

	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

// todoToolLLM calls todowrite on its first turn, then answers.
type todoToolLLM struct{ calls int }

func (l *todoToolLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (l *todoToolLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (l *todoToolLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	l.calls++
	var events []llmschema.LLMEvent
	if l.calls == 1 {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewToolCall("call-1", "todowrite", map[string]any{"todos": []any{
				map[string]any{"content": "read code", "status": "in_progress", "priority": "high"},
			}}),
			llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
			llmschema.NewFinish(llmschema.ReasonToolCalls),
		}
	} else {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewTextStart("t1"),
			llmschema.NewTextDelta("t1", "done"),
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		}
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

// TestClientOnReceivesTodoUpdated guards the todo.updated event end to end: the
// todowrite tool publishes it and Client.On surfaces it as TodoUpdated.
func TestClientOnReceivesTodoUpdated(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"), Model: "test-model",
		Root: t.TempDir(), Permission: ApproveAll, LLM: &todoToolLLM{},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	var mu sync.Mutex
	var got []schema.SessionTodoInfo
	client.On(func(event Event) {
		if todo, ok := event.(TodoUpdated); ok {
			mu.Lock()
			got = todo.Todos
			mu.Unlock()
		}
	})

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Content != "read code" || got[0].Status != "in_progress" {
		t.Fatalf("todo events = %+v", got)
	}
}

// TestMapTodoEventStructPayload guards decoding the publisher's struct payload,
// which is not a map like every other event.
func TestMapTodoEventStructPayload(t *testing.T) {
	type todoEvent struct {
		SessionID string                   `json:"sessionID"`
		Todos     []schema.SessionTodoInfo `json:"todos"`
	}
	event, ok := mapEvent(schema.EventPayload{
		Type: schema.SessionTodoEventUpdated.Type,
		Data: todoEvent{SessionID: "ses_1", Todos: []schema.SessionTodoInfo{{Content: "x", Status: "pending", Priority: "low"}}},
	})
	if !ok {
		t.Fatal("todo.updated was not mapped")
	}
	todo, ok := event.(TodoUpdated)
	if !ok {
		t.Fatalf("mapped %T, want TodoUpdated", event)
	}
	if todo.SessionID != "ses_1" || len(todo.Todos) != 1 || todo.Todos[0].Content != "x" {
		t.Fatalf("todo = %+v", todo)
	}
}
