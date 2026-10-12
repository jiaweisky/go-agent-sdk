package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// guardProbe records the ops it is asked about and (when deny is set) rejects
// every mutation.
type guardProbe struct {
	deny bool
	ops  []core.FileOp
}

func (g *guardProbe) Allow(_ context.Context, _ string, op core.FileOp) error {
	g.ops = append(g.ops, op)
	if g.deny {
		return errors.New("guard denied")
	}
	return nil
}

func settleCall(t *testing.T, registry *Registry, name string, input map[string]any) *Settlement {
	t.Helper()
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID: "ses_1", Agent: "build", AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{ID: "call_1", Name: name, Input: input},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestWriteGuardBlocksMutation(t *testing.T) {
	root := t.TempDir()
	deps := newWriteDeps(root)
	probe := &guardProbe{deny: true}
	deps.Guard = probe

	registry := NewRegistry()
	if _, err := RegisterWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result := settleCall(t, registry, WriteName, map[string]any{"path": "x.txt", "content": "hi"})
	if result.Result.Type != "error" {
		t.Fatalf("expected the guard to fail the write, got %v", result.Result)
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); !os.IsNotExist(err) {
		t.Fatal("the guard was bypassed: the file was written")
	}
	if len(probe.ops) != 1 || probe.ops[0] != core.FileOpWrite {
		t.Fatalf("guard ops = %v, want [write]", probe.ops)
	}
}

func TestWriteGuardNilAllows(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistry()
	if _, err := RegisterWrite(registry, newWriteDeps(root)); err != nil {
		t.Fatalf("register: %v", err)
	}
	result := settleCall(t, registry, WriteName, map[string]any{"path": "ok.txt", "content": "hi"})
	if result.Result.Type != "text" {
		t.Fatalf("a nil guard must allow the write, got %v", result.Result)
	}
	if _, err := os.Stat(filepath.Join(root, "ok.txt")); err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
}

func TestEditGuardBlocksMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := newEditDeps(root)
	probe := &guardProbe{deny: true}
	deps.Guard = probe

	registry := NewRegistry()
	if _, err := RegisterEdit(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	input := map[string]any{"path": "file.txt", "oldString": "hello", "newString": "bye"}
	result := settleCall(t, registry, EditName, input)
	if result.Result.Type != "error" {
		t.Fatalf("expected the guard to fail the edit, got %v", result.Result)
	}
	got, err := os.ReadFile(filepath.Join(root, "file.txt"))
	if err != nil || string(got) != "hello world\n" {
		t.Fatalf("the file changed despite the guard: %q (%v)", got, err)
	}
	if len(probe.ops) != 1 || probe.ops[0] != core.FileOpEdit {
		t.Fatalf("guard ops = %v, want [edit]", probe.ops)
	}
}

func TestApplyPatchGuardBlocksMutation(t *testing.T) {
	root := t.TempDir()
	deps := newApplyPatchDeps(root, &recordingPermission{}, nil)
	probe := &guardProbe{deny: true}
	deps.Guard = probe

	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	patch := "*** Begin Patch\n*** Add File: created.txt\n+created\n*** End Patch"
	result := settleCall(t, registry, ApplyPatchName, map[string]any{"patchText": patch})
	if result.Result.Type != "error" {
		t.Fatalf("expected the guard to fail the patch, got %v", result.Result)
	}
	if _, err := os.Stat(filepath.Join(root, "created.txt")); !os.IsNotExist(err) {
		t.Fatal("the guard was bypassed: the patch was applied")
	}
	if len(probe.ops) != 1 || probe.ops[0] != core.FileOpApplyPatch {
		t.Fatalf("guard ops = %v, want [apply_patch]", probe.ops)
	}
}
