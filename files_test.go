package agent

import (
	"context"
	"iter"
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// capturingLLM records the request the runner builds, so a test can inspect it.
type capturingLLM struct{ request *llmschema.LLMRequest }

func (c *capturingLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (c *capturingLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (c *capturingLLM) Stream(_ context.Context, request *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	c.request = request
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", "ok"),
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

// TestSessionSendAttachesFiles guards that MessageOptions.Files reach the model
// request as media parts.
func TestSessionSendAttachesFiles(t *testing.T) {
	llm := &capturingLLM{}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"), Model: "test-model",
		Root: t.TempDir(), Permission: ApproveAll, LLM: llm,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	file := schema.PromptFileAttachment{URI: "data:image/png;base64,iVBORw0KGgo=", Mime: "image/png"}
	for _, err := range session.Send(context.Background(), MessageOptions{
		Prompt: "see this", Files: []schema.PromptFileAttachment{file},
	}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	if llm.request == nil {
		t.Fatal("no request captured")
	}
	found := false
	for _, message := range llm.request.Messages {
		for _, part := range message.Content {
			if media, ok := part.(llmschema.MediaPart); ok {
				found = true
				if media.MediaType != "image/png" {
					t.Fatalf("media type = %q, want image/png", media.MediaType)
				}
			}
		}
	}
	if !found {
		t.Fatal("the attachment was not sent as a media part")
	}
}
