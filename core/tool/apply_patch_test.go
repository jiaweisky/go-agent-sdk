package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type recordingPermission struct {
	assertions []core.PermissionAssertInput
	denyAction string
	onEdit     func()
}

func (p *recordingPermission) Assert(input core.PermissionAssertInput) error {
	p.assertions = append(p.assertions, input)
	if input.Action == "edit" && p.onEdit != nil {
		p.onEdit()
	}
	if input.Action == p.denyAction {
		return &core.PermissionBlockedError{}
	}
	return nil
}

func (p *recordingPermission) Ask(input core.PermissionAssertInput) (*core.PermissionAskResult, error) {
	panic("unused")
}

func (p *recordingPermission) Reply(input core.PermissionReplyInput) error {
	panic("unused")
}

func (p *recordingPermission) Get(id core.PermissionID) *core.PermissionRequest {
	panic("unused")
}

func (p *recordingPermission) ForSession(sessionID string) []core.PermissionRequest {
	panic("unused")
}

func (p *recordingPermission) List() []core.PermissionRequest {
	panic("unused")
}

func (p *recordingPermission) Close() error {
	panic("unused")
}

type failingRemoveFileMutation struct {
	core.FileMutation
	failBasename string
}

func (f *failingRemoveFileMutation) Remove(input core.FileMutationRemoveInput) (*core.FileMutationRemoveResult, error) {
	if filepath.Base(input.Target.Canonical) == f.failBasename {
		return nil, errors.New("forced remove failure")
	}
	return f.FileMutation.Remove(input)
}

func newApplyPatchDeps(root string, permission core.Permission, files core.FileMutation) ApplyPatchDeps {
	if files == nil {
		files = core.NewFileMutation()
	}
	return ApplyPatchDeps{
		Mutation: core.NewLocationMutation(&core.Location{
			LocationInfo: coreschema.LocationInfo{Directory: coreschema.AbsolutePath(root)},
		}),
		Files:      files,
		Permission: permission,
	}
}

func settleApplyPatch(t *testing.T, registry *Registry, patchText string) *Settlement {
	t.Helper()
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_apply_patch_tool_test",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call-apply-patch",
			Name:  ApplyPatchName,
			Input: map[string]any{"patchText": patchText},
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func applyPatchStructured(t *testing.T, result *Settlement) ApplyPatchOutput {
	t.Helper()
	if result.Output == nil {
		t.Fatalf("missing output")
	}
	b, err := json.Marshal(result.Output.Structured)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var parsed ApplyPatchOutput
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return parsed
}

func TestApplyPatch_SequentialAddUpdateDelete(t *testing.T) {
	root := t.TempDir()
	update := filepath.Join(root, "update.txt")
	remove := filepath.Join(root, "remove.txt")
	if err := os.WriteFile(update, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remove, []byte("remove\n"), 0644); err != nil {
		t.Fatal(err)
	}

	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	mat := registry.Materialize(nil)
	if len(mat.Definitions) != 1 || mat.Definitions[0].Name != ApplyPatchName {
		t.Fatalf("unexpected definitions: %#v", mat.Definitions)
	}

	result := settleApplyPatch(t, registry,
		"*** Begin Patch\n*** Add File: nested/new.txt\n+created\n*** Update File: update.txt\n@@\n-before\n+after\n*** Delete File: remove.txt\n*** End Patch",
	)
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	wantOutput := "Applied patch sequentially:\nA nested/new.txt\nM update.txt\nD remove.txt"
	if result.Result.Value != wantOutput {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}

	structured := applyPatchStructured(t, result)
	wantApplied := []ApplyPatchApplied{
		{Type: "add", Resource: "nested/new.txt"},
		{Type: "update", Resource: "update.txt"},
		{Type: "delete", Resource: "remove.txt"},
	}
	if len(structured.Applied) != len(wantApplied) {
		t.Fatalf("expected %d applied entries, got %d", len(wantApplied), len(structured.Applied))
	}
	for i, item := range wantApplied {
		got := structured.Applied[i]
		if got.Type != item.Type || got.Resource != item.Resource {
			t.Fatalf("applied[%d] = %#v, want %#v", i, got, item)
		}
	}

	if len(structured.Files) != 3 {
		t.Fatalf("expected 3 file diffs, got %d", len(structured.Files))
	}
	checks := []struct {
		file      string
		status    coreschema.FileDiffStatus
		additions int
		deletions int
		patch     string
	}{
		{"nested/new.txt", coreschema.FileDiffStatusAdded, 1, 0, "+created"},
		{"update.txt", coreschema.FileDiffStatusModified, 1, 1, "-before\n+after"},
		{"remove.txt", coreschema.FileDiffStatusDeleted, 0, 1, "-remove"},
	}
	for i, check := range checks {
		got := structured.Files[i]
		if got.File == nil || *got.File != check.file {
			t.Fatalf("files[%d].file = %v, want %q", i, got.File, check.file)
		}
		if got.Status == nil || *got.Status != check.status {
			t.Fatalf("files[%d].status = %v, want %q", i, got.Status, check.status)
		}
		if got.Additions != check.additions || got.Deletions != check.deletions {
			t.Fatalf("files[%d] counts = %d/%d, want %d/%d", i, got.Additions, got.Deletions, check.additions, check.deletions)
		}
		if got.Patch == nil || !strings.Contains(*got.Patch, check.patch) {
			t.Fatalf("files[%d].patch = %v, want containing %q", i, got.Patch, check.patch)
		}
	}

	if len(permission.assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(permission.assertions))
	}
	edit := permission.assertions[0]
	if edit.Action != "edit" {
		t.Fatalf("expected edit action, got %q", edit.Action)
	}
	if !reflect.DeepEqual(edit.Resources, []string{"nested/new.txt", "update.txt", "remove.txt"}) {
		t.Fatalf("unexpected resources: %#v", edit.Resources)
	}
	if !reflect.DeepEqual(edit.Save, []string{"*"}) {
		t.Fatalf("unexpected save: %#v", edit.Save)
	}

	got, err := os.ReadFile(filepath.Join(root, "nested", "new.txt"))
	if err != nil || string(got) != "created\n" {
		t.Fatalf("nested/new.txt = %q (%v)", got, err)
	}
	got, err = os.ReadFile(update)
	if err != nil || string(got) != "after\n" {
		t.Fatalf("update.txt = %q (%v)", got, err)
	}
	if _, err := os.Stat(remove); !os.IsNotExist(err) {
		t.Fatalf("remove.txt should be gone")
	}
}

func TestApplyPatch_RejectsMovesBeforeApplying(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old.txt")
	if err := os.WriteFile(source, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}

	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry,
		"*** Begin Patch\n*** Add File: created.txt\n+created\n*** Update File: old.txt\n*** Move to: moved.txt\n@@\n-before\n+after\n*** End Patch",
	)
	if result.Result.Type != "error" || result.Result.Value != "apply_patch moves are not supported yet" {
		t.Fatalf("unexpected result: %#v", result.Result)
	}
	if _, err := os.Stat(filepath.Join(root, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("created.txt should not exist")
	}
	if len(permission.assertions) != 0 {
		t.Fatalf("expected no assertions, got %#v", permission.assertions)
	}
}

func TestApplyPatch_ExternalDirectoryApprovedBeforeEdit(t *testing.T) {
	active := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "external.txt")
	if err := os.WriteFile(target, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}

	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(active, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry, "*** Begin Patch\n*** Update File: "+target+"\n@@\n-before\n+after\n*** End Patch")
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %#v", result.Result)
	}

	actions := make([]string, len(permission.assertions))
	for i, input := range permission.assertions {
		actions[i] = input.Action
	}
	if !reflect.DeepEqual(actions, []string{"external_directory", "edit"}) {
		t.Fatalf("unexpected assertion order: %#v", actions)
	}

	got, err := os.ReadFile(target)
	if err != nil || string(got) != "after\n" {
		t.Fatalf("external.txt = %q (%v)", got, err)
	}
}

func TestApplyPatch_ExternalDirectoryScopeDeduplicated(t *testing.T) {
	active := t.TempDir()
	outside := t.TempDir()
	first := filepath.Join(outside, "first.txt")
	second := filepath.Join(outside, "second.txt")
	if err := os.WriteFile(first, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}

	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(active, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry,
		"*** Begin Patch\n*** Update File: "+first+"\n@@\n-before\n+after\n*** Update File: "+second+"\n@@\n-before\n+after\n*** End Patch",
	)
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %#v", result.Result)
	}

	actions := make([]string, len(permission.assertions))
	for i, input := range permission.assertions {
		actions[i] = input.Action
	}
	if !reflect.DeepEqual(actions, []string{"external_directory", "edit"}) {
		t.Fatalf("unexpected assertion order: %#v", actions)
	}

	canonical, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	wantResource := filepath.Join(canonical, "*")
	if !reflect.DeepEqual(permission.assertions[0].Resources, []string{wantResource}) {
		t.Fatalf("unexpected external resources: %#v", permission.assertions[0].Resources)
	}
}

func TestApplyPatch_RejectsLaterInvalidUpdateBeforeEarlierAdd(t *testing.T) {
	root := t.TempDir()
	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry,
		"*** Begin Patch\n*** Add File: created.txt\n+created\n*** Update File: missing.txt\n@@\n-before\n+after\n*** End Patch",
	)
	if result.Result.Type != "error" || result.Result.Value != "Unable to apply patch at missing.txt" {
		t.Fatalf("unexpected result: %#v", result.Result)
	}
	if _, err := os.Stat(filepath.Join(root, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("created.txt should not exist")
	}
}

func TestApplyPatch_RejectsAddTargetingExistingFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(target, []byte("sentinel\n"), 0644); err != nil {
		t.Fatal(err)
	}

	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry, "*** Begin Patch\n*** Add File: existing.txt\n+replacement\n*** End Patch")
	if result.Result.Type != "error" || result.Result.Value != "Unable to apply patch at existing.txt" {
		t.Fatalf("unexpected result: %#v", result.Result)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "sentinel\n" {
		t.Fatalf("existing.txt = %q (%v)", got, err)
	}
}

func TestApplyPatch_RejectsAddTargetAppearingDuringApproval(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "appeared.txt")

	permission := &recordingPermission{
		onEdit: func() {
			if err := os.WriteFile(target, []byte("winner\n"), 0644); err != nil {
				t.Fatalf("write winner: %v", err)
			}
		},
	}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, nil)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry, "*** Begin Patch\n*** Add File: appeared.txt\n+replacement\n*** End Patch")
	if result.Result.Type != "error" || result.Result.Value != "Unable to apply patch at appeared.txt" {
		t.Fatalf("unexpected result: %#v", result.Result)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "winner\n" {
		t.Fatalf("appeared.txt = %q (%v)", got, err)
	}
}

func TestApplyPatch_ReportsPartialApplicationOnLaterDefect(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}

	files := &failingRemoveFileMutation{
		FileMutation: core.NewFileMutation(),
		failBasename: filepath.Base(second),
	}
	permission := &recordingPermission{}
	registry := NewRegistry()
	if _, err := RegisterApplyPatch(registry, newApplyPatchDeps(root, permission, files)); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := settleApplyPatch(t, registry, "*** Begin Patch\n*** Delete File: first.txt\n*** Delete File: second.txt\n*** End Patch")
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %#v", result.Result)
	}
	if result.Result.Value != "Patch partially applied before failing at second.txt. Applied: first.txt" {
		t.Fatalf("unexpected error message: %v", result.Result.Value)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first.txt should have been removed")
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second.txt should still exist: %v", err)
	}
}
