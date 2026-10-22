package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
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
	return &a2aSession{session: session, inboundMedia: mode, spoolDir: core.A2ASpoolDir(client.rt.global.Data), turns: map[string]*a2aTurn{}}
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

func TestA2AFactoryAllowsReadingSpool(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-compatible-chat", Credentials: APIKey("k"), Model: "m",
		Root: t.TempDir(), WorkingDirectory: t.TempDir(), Permission: ApproveAll, LLM: &capturingLLM{},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	factory := NewA2AFactory(client).(*a2aFactory)
	spooled := filepath.Join(core.A2ASpoolDir(client.rt.global.Data), "turn-1", "a.png")
	// Inbound sessions deny external_directory, but the spool lives outside the
	// workspace, so reading it must still be allowed or the feature is dead.
	effective := core.PermissionMerge(core.DefaultAgentPermissions(client.rt.global), factory.permissionsFor(server.Workspace{}))
	if got := core.PermissionEvaluate("external_directory", spooled, effective).Effect; got != schema.PermissionEffectAllow {
		t.Fatalf("reading a spooled attachment must be allowed, got %s", got)
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
	if !strings.Contains(prompt, session.spoolDir) || !strings.Contains(prompt, "a.png") {
		t.Fatalf("prompt = %q, want a spooled path under %q", prompt, session.spoolDir)
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

func TestMaterializeInlineKeepsNonHTTPURIAsNote(t *testing.T) {
	session := newA2ATestSession(t, "inline")
	prompt, files, err := session.materialize("t4", server.SendRequest{
		Prompt: "look",
		Files:  []server.File{{Name: "stored.pdf", MIME: "application/pdf", URI: "s3://bucket/key"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want no attachment for a non-http URI", files)
	}
	if !strings.Contains(prompt, "s3://bucket/key") {
		t.Fatalf("prompt = %q, want the URI note", prompt)
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

// TestA2AFactoryDeclinesQuestions guards the "decline" questions policy: a
// question ask resolves immediately with a rejection instead of parking, while
// permissions stay held.
func TestA2AFactoryDeclinesQuestions(t *testing.T) {
	client := newTestClient(t)
	factory := NewA2AFactory(client, WithInboundQuestions("decline"))
	session, err := factory.CreateSession(context.Background(), server.SessionRequest{AgentID: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	if client.rt.question.hold.Load() {
		t.Fatal("questions should leave hold mode under the decline policy")
	}
	if !client.rt.permission.hold.Load() {
		t.Fatal("permissions must stay held under the decline policy")
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.rt.question.Ask(context.Background(), core.QuestionAskInput{
			SessionID: session.ID(),
			Questions: []schema.QuestionInfo{{Question: "Pick", Options: []schema.QuestionOption{{Label: "a"}}}},
		})
		done <- err
	}()
	select {
	case err := <-done:
		var rejected *core.QuestionRejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("ask error = %v, want QuestionRejectedError", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("question ask was held instead of declined")
	}
}

// TestA2AFactoryParksQuestionsByDefault guards the default: the factory holds
// questions, so the question tool parks the turn for a client answer.
func TestA2AFactoryParksQuestionsByDefault(t *testing.T) {
	client := newTestClient(t)
	factory := NewA2AFactory(client)
	session, err := factory.CreateSession(context.Background(), server.SessionRequest{AgentID: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	if !client.rt.question.hold.Load() || !client.rt.permission.hold.Load() {
		t.Fatal("park policy should hold both questions and permissions")
	}
}
