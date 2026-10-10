package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
)

func newA2ATestSession(t *testing.T, mode string) *a2aSession {
	t.Helper()
	dir := t.TempDir()
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-compatible-chat", Credentials: APIKey("k"), Model: "m",
		Root: dir, WorkingDirectory: dir, Permission: ApproveAll, LLM: &capturingLLM{},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &a2aSession{session: session, inboundMedia: mode, turns: map[string]*a2aTurn{}}
}

func TestMaterializeInlineDeliversMedia(t *testing.T) {
	session := newA2ATestSession(t, "inline")
	_, files, err := session.materialize("t1", server.SendRequest{
		Prompt: "look",
		Files:  []server.File{{Name: "a.png", MIME: "image/png", Data: []byte{1, 2, 3}}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v, want 1", files)
	}
	if !strings.HasPrefix(files[0].URI, "data:image/png;base64,") {
		t.Fatalf("URI = %q, want a data URL", files[0].URI)
	}
	if files[0].Name == nil || *files[0].Name != "a.png" || files[0].Mime != "image/png" {
		t.Fatalf("file = %+v", files[0])
	}
}

func TestMaterializeSpoolWritesPaths(t *testing.T) {
	session := newA2ATestSession(t, "spool")
	prompt, files, err := session.materialize("t2", server.SendRequest{
		Prompt: "look",
		Files:  []server.File{{Name: "a.png", MIME: "image/png", Data: []byte{1, 2, 3}}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("spool should not inline media, got %v", files)
	}
	if !strings.Contains(prompt, ".a2a") || !strings.Contains(prompt, "a.png") {
		t.Fatalf("prompt = %q, want a spooled path", prompt)
	}
}

func TestMaterializeInlineKeepsURLParts(t *testing.T) {
	session := newA2ATestSession(t, "inline")
	_, files, err := session.materialize("t3", server.SendRequest{
		Prompt: "look",
		Files:  []server.File{{Name: "remote.png", MIME: "image/png", URI: "https://example.com/a.png"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(files) != 1 || files[0].URI != "https://example.com/a.png" {
		t.Fatalf("files = %v, want the URL part carried", files)
	}
}

func TestA2AEventFromAttachmentOmitted(t *testing.T) {
	event, ok := a2aEventFrom(AttachmentOmitted{Name: "a.png", MIME: "image/png", Reason: "unsupported_modality"})
	if !ok || event.Kind != server.EventNotice {
		t.Fatalf("event = %+v, ok = %v", event, ok)
	}
	if !strings.Contains(event.Text, "a.png") || !strings.Contains(event.Text, "unsupported_modality") {
		t.Fatalf("notice text = %q", event.Text)
	}
}

func TestA2AEventFromPermissionAskedCarriesDetails(t *testing.T) {
	event, ok := a2aEventFrom(PermissionAsked{Request: PermissionRequest{
		ID:        "per_1",
		SessionID: "ses_1",
		Action:    "bash",
		Resources: []string{"docker ps"},
		Save:      []string{"docker *"},
	}})
	if !ok || event.Kind != server.EventAwaitInput {
		t.Fatalf("event = %+v, ok = %v", event, ok)
	}
	if event.RequestID != "per_1" || event.IsQuestion {
		t.Fatalf("request id = %q, isQuestion = %v", event.RequestID, event.IsQuestion)
	}
	if event.Permission == nil {
		t.Fatal("permission details were dropped")
	}
	if event.Permission.Action != "bash" || len(event.Permission.Resources) != 1 || event.Permission.Resources[0] != "docker ps" {
		t.Fatalf("permission = %+v", event.Permission)
	}
	if len(event.Permission.Save) != 1 || event.Permission.Save[0] != "docker *" {
		t.Fatalf("permission save = %v", event.Permission.Save)
	}
}
