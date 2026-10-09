package agent

import (
	"context"
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TestRunTaskAdmitsFiles guards that a host/structured caller can forward
// attachments to a child session, and the child's model receives them.
func TestRunTaskAdmitsFiles(t *testing.T) {
	llm := &capturingLLM{}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"), Model: "m",
		Root: t.TempDir(), Permission: ApproveAll, LLM: llm,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register parent: %v", err)
	}
	sub := schema.AgentModeSubagent
	if err := client.RegisterAgent("worker", AgentConfig{System: "w", Mode: &sub}); err != nil {
		t.Fatalf("register subagent: %v", err)
	}
	parent, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer parent.Close()

	file := schema.PromptFileAttachment{URI: "data:image/png;base64,aGVsbG8=", Mime: "image/png"}
	if _, err := client.rt.runTask(context.Background(), TaskRunInput{
		ParentSessionID: parent.ID(), SubagentType: "worker", Description: "d", Prompt: "look",
		Files: []schema.PromptFileAttachment{file},
	}); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if llm.request == nil {
		t.Fatal("no child request captured")
	}
	found := false
	for _, message := range llm.request.Messages {
		for _, part := range message.Content {
			if media, ok := part.(llmschema.MediaPart); ok && media.MediaType == "image/png" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the child request did not carry the admitted media")
	}
}
