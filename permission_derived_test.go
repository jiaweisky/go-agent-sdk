package agent

import (
	"context"
	"testing"
	"time"
)

// TestDerivedSessionRunsTools guards that a session with a customized system
// prompt — which runs a session-derived agent whose id ("build#ses_x") is not in
// the registry — still resolves its base agent's permissions instead of the
// deny-all fallback, so its tools run.
func TestDerivedSessionRunsTools(t *testing.T) {
	calls := 0
	executed := false
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		Root:       t.TempDir(),
		Permission: ApproveAll,
		LLM:        toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	echo := DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) {
		executed = true
		return "ok", nil
	})
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent:        "build",
		Tools:        []Tool{echo},
		SystemPrompt: &SystemPromptConfig{Mode: SystemPromptAppend, Content: "be concise"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			break
		}
	}
	if !executed {
		t.Fatal("tool was denied for a session-derived agent")
	}
}
