package agent

import (
	"context"
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// modelInfoFor returns a ModelInfo provider whose capabilities are chosen by
// model id (falling back to fallback), so a matrix can vary the target model.
func modelInfoFor(byID map[string][]string, fallback []string) ModelInfoProvider {
	return func(id string) (schema.ModelInfo, error) {
		inputs := fallback
		if c, ok := byID[id]; ok {
			inputs = c
		}
		info, err := NewModelInfo("openai-compatible-chat", id, "http://127.0.0.1:1/v1", schema.ModelLimit{})
		if err != nil {
			return schema.ModelInfo{}, err
		}
		info.Capabilities.Input = inputs
		return info, nil
	}
}

func requestHasMedia(request *llmschema.LLMRequest, mime string) bool {
	if request == nil {
		return false
	}
	for _, message := range request.Messages {
		for _, part := range message.Content {
			if media, ok := part.(llmschema.MediaPart); ok && media.MediaType == mime {
				return true
			}
		}
	}
	return false
}

// TestMediaCapabilityMatrix checks that, for the target session's model, an
// attachment whose modality it supports is delivered; otherwise it is omitted
// and the turn still completes.
func TestMediaCapabilityMatrix(t *testing.T) {
	cases := []struct {
		name      string
		input     []string
		mime      string
		delivered bool
	}{
		{"text rejects image", []string{"text"}, "image/png", false},
		{"TI delivers image", []string{"text", "image"}, "image/png", true},
		{"TIAV delivers audio", []string{"text", "image", "audio", "video"}, "audio/wav", true},
		{"TA delivers audio", []string{"text", "audio"}, "audio/wav", true},
		{"TV delivers video", []string{"text", "video"}, "video/mp4", true},
		{"T rejects video", []string{"text"}, "video/mp4", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			llm := &capturingLLM{}
			client, err := NewClient(&ClientOptions{
				Root: t.TempDir(), Protocol: "openai-compatible-chat", Credentials: APIKey("k"), Model: "m",
				Permission: ApproveAll, LLM: llm, ModelInfo: modelInfoFor(nil, tc.input),
			})
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			defer client.Close()
			if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
				t.Fatalf("register agent: %v", err)
			}
			session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			defer session.Close()

			file := schema.PromptFileAttachment{URI: "data:" + tc.mime + ";base64,aGVsbG8=", Mime: tc.mime}
			for _, err := range session.Send(context.Background(), MessageOptions{
				Prompt: "look", Files: []schema.PromptFileAttachment{file},
			}) {
				if err != nil {
					t.Fatalf("send: %v", err)
				}
			}
			if got := requestHasMedia(llm.request, tc.mime); got != tc.delivered {
				t.Fatalf("delivered = %v, want %v", got, tc.delivered)
			}
		})
	}
}

// TestMediaCapabilityMatrixSubagent checks the subagent form: the gate is
// evaluated on the child's model, so an attachment the child lacks is omitted
// (and the task still completes) while the parent's own model is untouched.
func TestMediaCapabilityMatrixSubagent(t *testing.T) {
	llm := &capturingLLM{}
	client, err := NewClient(&ClientOptions{
		Root: t.TempDir(), Protocol: "openai-compatible-chat", Credentials: APIKey("k"), Model: "vision",
		Permission: ApproveAll, LLM: llm,
		ModelInfo: modelInfoFor(map[string][]string{"text-only": {"text"}}, []string{"text", "image"}),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register parent: %v", err)
	}
	sub := schema.AgentModeSubagent
	if err := client.RegisterAgent("reader", AgentConfig{System: "r", Mode: &sub, Model: "text-only"}); err != nil {
		t.Fatalf("register subagent: %v", err)
	}
	parent, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer parent.Close()

	file := schema.PromptFileAttachment{URI: "data:image/png;base64,aGVsbG8=", Mime: "image/png"}
	if _, err := client.rt.runTask(context.Background(), TaskRunInput{
		ParentSessionID: parent.ID(), SubagentType: "reader", Description: "d", Prompt: "look",
		Files: []schema.PromptFileAttachment{file},
	}); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if requestHasMedia(llm.request, "image/png") {
		t.Fatal("a text-only subagent must not receive an image")
	}
}
