package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/schema"
)

func newEditDeps(root string) EditDeps {
	allow := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectAllow},
	}
	return EditDeps{
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

func settleEdit(t *testing.T, deps EditDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterEdit(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  EditName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestEdit_ReplacesText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "world",
		"newString": "there",
	})

	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	want := "Edited file successfully: file.txt\n" +
		"Replacements: 1\n" +
		"```diff\n" +
		"-world\n" +
		"+there\n" +
		"```"
	if result.Result.Value != want {
		t.Fatalf("unexpected model output:\n%q\nwant:\n%q", result.Result.Value, want)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "hello there\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestEdit_IdenticalStrings(t *testing.T) {
	root := t.TempDir()
	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "same",
		"newString": "same",
	})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "No changes to apply: oldString and newString are identical." {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestEdit_EmptyOldString(t *testing.T) {
	root := t.TempDir()
	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "",
		"newString": "x",
	})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "oldString must not be empty. Use write to create or overwrite a file." {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestEdit_NoMatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "missing",
		"newString": "x",
	})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Could not find oldString in the file. It must match exactly, including whitespace and indentation." {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestEdit_MultipleMatchesRequiresReplaceAll(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("x\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "x",
		"newString": "y",
	})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Found multiple exact matches for oldString. Provide more surrounding context or set replaceAll to true." {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("x\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":       "file.txt",
		"oldString":  "x",
		"newString":  "y",
		"replaceAll": true,
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	if value, _ := result.Result.Value.(string); !strings.Contains(value, "Replacements: 2") {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "y\ny\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestEdit_PreservesCrlf(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("a\r\nold\r\nb\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "old",
		"newString": "new",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\r\nnew\r\nb\r\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestEdit_PreservesBom(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte("old\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "old",
		"newString": "new",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	if len(got) < 3 || got[0] != 0xef || got[1] != 0xbb || got[2] != 0xbf {
		t.Fatalf("expected BOM preserved, got %v", got)
	}
	if string(got[3:]) != "new\n" {
		t.Fatalf("unexpected content after BOM: %q", got[3:])
	}
}

func TestEdit_StructuredOutputHasPatchAndCounts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "b",
		"newString": "B",
	})
	if result.Output == nil {
		t.Fatalf("expected structured output")
	}
	structured, ok := result.Output.Structured.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured output type: %T", result.Output.Structured)
	}
	if structured["replacements"] != float64(1) {
		t.Fatalf("unexpected replacements: %v", structured["replacements"])
	}
	files, ok := structured["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("unexpected files: %v", structured["files"])
	}
	info := files[0].(map[string]any)
	if info["file"] != "file.txt" {
		t.Fatalf("unexpected file: %v", info["file"])
	}
	if info["additions"] != float64(1) || info["deletions"] != float64(1) {
		t.Fatalf("unexpected counts: +%v -%v", info["additions"], info["deletions"])
	}
	if info["status"] != "modified" {
		t.Fatalf("unexpected status: %v", info["status"])
	}
	patch, _ := info["patch"].(string)
	if !strings.Contains(patch, "@@ -1,3 +1,3 @@") || !strings.Contains(patch, "-b\n") || !strings.Contains(patch, "+B\n") {
		t.Fatalf("unexpected patch:\n%s", patch)
	}
}

func TestEdit_AbsolutePathInsideLocation(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "sub", "inside.txt")
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      abs,
		"oldString": "old",
		"newString": "new",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	if value, _ := result.Result.Value.(string); !strings.Contains(value, "Edited file successfully: sub/inside.txt") {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestEdit_DeniedPermission(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	deny := core.PermissionRuleset{
		{Action: "*", Resource: "*", Effect: core.PermissionEffectDeny},
	}
	deps := EditDeps{
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

	result := settleEdit(t, deps, map[string]any{
		"path":      "file.txt",
		"oldString": "old",
		"newString": "new",
	})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to edit file.txt" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Fatalf("file should not have changed: %q", got)
	}
}

func TestEdit_AbsoluteExternalPath(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	target := filepath.Join(external, "outside.txt")
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      target,
		"oldString": "old",
		"newString": "new",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	canonicalExternal, err := filepath.EvalSymlinks(external)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	expectedResource := filepath.Join(canonicalExternal, "outside.txt")
	if value, _ := result.Result.Value.(string); !strings.Contains(value, "Edited file successfully: "+expectedResource) {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestEdit_InvalidUtf8ReplacedWithReplacementChar(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte{'a', 0xff, 'b'}, 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "a",
		"newString": "x",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	want := []byte{'x', 0xef, 0xbf, 0xbd, 'b'}
	if string(got) != string(want) {
		t.Fatalf("unexpected content: %v, want %v", got, want)
	}
}

func TestEdit_MultilineCrlfInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("a\r\nb\r\nc\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := settleEdit(t, newEditDeps(root), map[string]any{
		"path":      "file.txt",
		"oldString": "a\nb",
		"newString": "x\ny",
	})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "x\r\ny\r\nc\r\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}
