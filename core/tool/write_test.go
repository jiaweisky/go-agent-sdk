package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type testSessions struct{ agent string }

func (s testSessions) Get(sessionID string) (*core.PermissionSessionInfo, bool, error) {
	return &core.PermissionSessionInfo{ID: sessionID, Agent: s.agent}, true, nil
}

type testAgents struct{ rules core.PermissionRuleset }

func (a testAgents) Resolve(agentID string) (*core.PermissionAgentInfo, bool) {
	return &core.PermissionAgentInfo{ID: agentID, Permissions: a.rules}, true
}

type testSaved struct{}

func (testSaved) List(projectID string) ([]core.PermissionSaved, error)  { return nil, nil }
func (testSaved) Add(projectID, action string, resources []string) error { return nil }

type testEvents struct{}

func (testEvents) PublishAsked(request core.PermissionRequest) error { return nil }
func (testEvents) PublishReplied(sessionID string, requestID core.PermissionID, reply core.PermissionReply) error {
	return nil
}

func newWriteDeps(root string) WriteDeps {
	allow := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectAllow},
	}
	return WriteDeps{
		Mutation: core.NewLocationMutation(&core.Location{LocationInfo: coreschema.LocationInfo{Directory: coreschema.AbsolutePath(root)}}),
		Files:    core.NewFileMutation(),
		Permission: core.NewPermission(core.PermissionConfig{
			Sessions:  testSessions{agent: "build"},
			Agents:    testAgents{rules: allow},
			Saved:     testSaved{},
			Events:    testEvents{},
			ProjectID: "proj_1",
		}),
	}
}

func TestWrite_CreatesFile(t *testing.T) {
	root := t.TempDir()
	deps := newWriteDeps(root)

	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	mat := registry.Materialize(nil)
	if len(mat.Definitions) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(mat.Definitions))
	}
	if mat.Definitions[0].Name != WriteName {
		t.Fatalf("expected definition name %q, got %q", WriteName, mat.Definitions[0].Name)
	}

	result, err := mat.Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": "hello.txt", "content": "hello world"},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	if result.Result.Value != "Created file successfully: hello.txt" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}

	got, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestWrite_OverwritesExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := newWriteDeps(root)
	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": "existing.txt", "content": "new"},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if result.Result.Value != "Wrote file successfully: existing.txt" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestWrite_PreservesBom(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bom.txt")
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte("old")...), 0644); err != nil {
		t.Fatal(err)
	}

	deps := newWriteDeps(root)
	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": "bom.txt", "content": "new"},
		},
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	got, _ := os.ReadFile(path)
	if len(got) < 3 || got[0] != 0xef || got[1] != 0xbb || got[2] != 0xbf {
		t.Fatalf("expected BOM preserved, got %v", got)
	}
	if string(got[3:]) != "new" {
		t.Fatalf("unexpected content after BOM: %q", got[3:])
	}
}

func TestWrite_AbsolutePathInsideLocation(t *testing.T) {
	root := t.TempDir()
	deps := newWriteDeps(root)
	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	abs := filepath.Join(root, "sub", "inside.txt")
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": abs, "content": "inside"},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if result.Result.Value != "Created file successfully: sub/inside.txt" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "inside" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestWrite_AbsoluteExternalPath(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	deps := newWriteDeps(root)
	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	target := filepath.Join(external, "outside.txt")
	canonicalExternal, err := filepath.EvalSymlinks(external)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": target, "content": "outside"},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	expectedResource := filepath.Join(canonicalExternal, "outside.txt")
	if result.Result.Value != "Created file successfully: "+expectedResource {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "outside" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestWrite_DeniedPermission(t *testing.T) {
	root := t.TempDir()
	deny := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectDeny},
	}
	deps := WriteDeps{
		Mutation: core.NewLocationMutation(&core.Location{LocationInfo: coreschema.LocationInfo{Directory: coreschema.AbsolutePath(root)}}),
		Files:    core.NewFileMutation(),
		Permission: core.NewPermission(core.PermissionConfig{
			Sessions:  testSessions{agent: "build"},
			Agents:    testAgents{rules: deny},
			Saved:     testSaved{},
			Events:    testEvents{},
			ProjectID: "proj_1",
		}),
	}

	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  WriteName,
			Input: map[string]any{"path": "nope.txt", "content": "x"},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to write nope.txt" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
	if _, err := os.Stat(filepath.Join(root, "nope.txt")); !os.IsNotExist(err) {
		t.Fatalf("file should not have been written")
	}
}
