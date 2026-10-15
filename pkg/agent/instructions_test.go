package agent

import (
	"context"
	"strings"
	"testing"
)

// TestSystemContextEnvBlock proves the built-in environment source reaches the
// model: the captured system prompt names the working directory and the
// preferred temp directory under the state root.
func TestSystemContextEnvBlock(t *testing.T) {
	root := t.TempDir()
	recorder := &capturingLLM{}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: root, Permission: ApproveAll, LLM: recorder,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

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
	if recorder.request == nil {
		t.Fatal("no request was captured")
	}

	var system strings.Builder
	for _, part := range recorder.request.System {
		system.WriteString(part.Text)
		system.WriteString("\n")
	}
	text := system.String()
	for _, want := range []string{
		"<env>",
		"Working directory: ",
		"Is directory a git repo: ",
		"Platform: ",
		"Prefer " + agentTempDir(root) + " over generic system temporary directories such as /tmp",
		"</env>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("system prompt missing %q:\n%s", want, text)
		}
	}
}
