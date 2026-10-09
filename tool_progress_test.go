package agent

import (
	"context"
	"iter"
	"sync"
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// scriptedLLM replays one script per Stream call, so a test can drive a tool
// call and then a continuation text.
type scriptedLLM struct {
	mu      sync.Mutex
	scripts [][]llmschema.LLMEvent
	calls   int
}

func (s *scriptedLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (s *scriptedLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (s *scriptedLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	s.mu.Lock()
	script := s.scripts[min(s.calls, len(s.scripts)-1)]
	s.calls++
	s.mu.Unlock()
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range script {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

// TestUserToolProgressPassthrough guards that a user tool's Progress reports
// reach subscribers as ToolProgress events: DefineTool must forward the core
// progress context into ToolContext.
func TestUserToolProgressPassthrough(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: &scriptedLLM{scripts: [][]llmschema.LLMEvent{
			{
				llmschema.NewToolCall("call-1", "progress_tool", map[string]any{}),
				llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
			},
			{
				llmschema.NewTextStart("t2"),
				llmschema.NewTextDelta("t2", "done"),
				llmschema.NewTextEnd("t2"),
				llmschema.NewStepFinish(0, llmschema.ReasonStop),
			},
		}},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	progressed := make(chan ToolProgress, 4)
	client.On(func(event Event) {
		if progress, ok := event.(ToolProgress); ok {
			progressed <- progress
		}
	})

	tool := DefineTool("progress_tool", "Report progress twice.",
		func(_ struct{}, tc ToolContext) (string, error) {
			for step := 1; step <= 2; step++ {
				if tc.Progress == nil {
					t.Error("ToolContext.Progress is nil")
					break
				}
				if err := tc.Progress(map[string]any{"step": step}, nil); err != nil {
					return "", err
				}
			}
			return "ok", nil
		})
	if _, err := client.RegisterTools(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "go"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	assertProgressStep(t, progressed, 1)
	assertProgressStep(t, progressed, 2)
}

// assertProgressStep waits for one ToolProgress carrying the given step.
func assertProgressStep(t *testing.T, progressed <-chan ToolProgress, want int) {
	t.Helper()
	progress := waitFor(t, progressed, "tool progress")
	structured, ok := progress.Structured.(map[string]any)
	if !ok {
		t.Fatalf("structured = %#v, want a map", progress.Structured)
	}
	if step, ok := structured["step"]; !ok || toInt(step) != want {
		t.Fatalf("structured = %#v, want step %d", structured, want)
	}
}

func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
