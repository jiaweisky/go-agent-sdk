package tool

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

type globRealBinary struct{ path string }

func (b globRealBinary) Filepath() (string, error) { return b.path, nil }

type globTestRipgrep struct {
	entries []core.FileSystemEntry
	err     error
	calls   []core.RipgrepGlobInput
}

func (g *globTestRipgrep) Glob(input core.RipgrepGlobInput) ([]core.FileSystemEntry, error) {
	g.calls = append(g.calls, input)
	if g.err != nil {
		return nil, g.err
	}
	return g.entries, nil
}

func (g *globTestRipgrep) Find(core.RipgrepFindInput) ([]core.FileSystemEntry, error) {
	return nil, nil
}

func (g *globTestRipgrep) Grep(core.RipgrepGrepInput) ([]core.FileSystemMatch, error) {
	return nil, nil
}

func settleGlob(t *testing.T, deps GlobDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterGlob(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  GlobName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func newGlobDeps(root string, rg core.Ripgrep, perm core.Permission) GlobDeps {
	return GlobDeps{Ripgrep: rg, Directory: root, Permission: perm}
}

func TestGlob_ModelOutputResolvesAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{entries: []core.FileSystemEntry{
		{Path: "a.go", Type: "file"},
		{Path: "sub/b.go", Type: "file"},
	}}

	result := settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "**/*.go"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	expected := filepath.Join(root, "a.go") + "\n" + filepath.Join(root, "sub", "b.go")
	if result.Result.Value != expected {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestGlob_NoFilesFound(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{}

	result := settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "*.nope"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q", result.Result.Type)
	}
	if result.Result.Value != "No files found" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestGlob_PassesCwdAndLimit(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{}
	deps := newGlobDeps(root, rg, &bashTestPermission{})

	settleGlob(t, deps, map[string]any{"pattern": "*.go", "path": "src", "limit": 5})

	if len(rg.calls) != 1 {
		t.Fatalf("expected 1 ripgrep call, got %d", len(rg.calls))
	}
	call := rg.calls[0]
	if call.Cwd != filepath.Join(root, "src") {
		t.Fatalf("unexpected cwd: %s", call.Cwd)
	}
	if call.Pattern != "*.go" {
		t.Fatalf("unexpected pattern: %s", call.Pattern)
	}
	if call.Limit != 5 {
		t.Fatalf("unexpected limit: %d", call.Limit)
	}
}

func TestGlob_DefaultLimitIsMaxSafeInteger(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{}

	settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "*.go"})

	if len(rg.calls) != 1 {
		t.Fatalf("expected 1 ripgrep call, got %d", len(rg.calls))
	}
	if rg.calls[0].Limit != globDefaultLimit() {
		t.Fatalf("unexpected default limit: %d", rg.calls[0].Limit)
	}
}

func TestGlob_DefaultCwdIsLocation(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{}

	settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "*.go"})

	if rg.calls[0].Cwd != root {
		t.Fatalf("unexpected default cwd: %s", rg.calls[0].Cwd)
	}
}

func TestGlob_PermissionAssertion(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{}
	perm := &bashTestPermission{}

	settleGlob(t, newGlobDeps(root, rg, perm), map[string]any{"pattern": "*.go", "path": "src", "limit": 3})

	if len(perm.assertions) != 1 {
		t.Fatalf("expected 1 permission assertion, got %d", len(perm.assertions))
	}
	assertion := perm.assertions[0]
	if assertion.Action != GlobName {
		t.Fatalf("unexpected action: %s", assertion.Action)
	}
	if len(assertion.Resources) != 1 || assertion.Resources[0] != "*.go" {
		t.Fatalf("unexpected resources: %v", assertion.Resources)
	}
	if len(assertion.Save) != 1 || assertion.Save[0] != "*" {
		t.Fatalf("unexpected save: %v", assertion.Save)
	}
	if assertion.Metadata["root"] != "src" || assertion.Metadata["path"] != "src" {
		t.Fatalf("unexpected metadata: %v", assertion.Metadata)
	}
	if assertion.Metadata["limit"] != 3 {
		t.Fatalf("unexpected metadata limit: %v", assertion.Metadata["limit"])
	}
	if assertion.SessionID != "ses_1" {
		t.Fatalf("unexpected session: %s", assertion.SessionID)
	}
	if assertion.Agent == nil || *assertion.Agent != "build" {
		t.Fatalf("unexpected agent: %v", assertion.Agent)
	}
	if assertion.Source == nil || assertion.Source.Type != core.PermissionSourceTypeTool {
		t.Fatalf("unexpected source: %#v", assertion.Source)
	}
	if assertion.Source.MessageID != "msg_1" || assertion.Source.CallID != "call_1" {
		t.Fatalf("unexpected source identifiers: %#v", assertion.Source)
	}
}

func TestGlob_DefaultMetadataRoot(t *testing.T) {
	root := t.TempDir()
	perm := &bashTestPermission{}

	settleGlob(t, newGlobDeps(root, &globTestRipgrep{}, perm), map[string]any{"pattern": "*.go"})

	if perm.assertions[0].Metadata["root"] != "." {
		t.Fatalf("unexpected metadata root: %v", perm.assertions[0].Metadata["root"])
	}
	if _, ok := perm.assertions[0].Metadata["path"]; ok {
		t.Fatalf("expected no path metadata key, got %v", perm.assertions[0].Metadata["path"])
	}
	if _, ok := perm.assertions[0].Metadata["limit"]; ok {
		t.Fatalf("expected no limit metadata key, got %v", perm.assertions[0].Metadata["limit"])
	}
}

func TestGlob_DeniedPermission(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{entries: []core.FileSystemEntry{{Path: "a.go", Type: "file"}}}
	perm := &bashTestPermission{denyAction: GlobName}

	result := settleGlob(t, newGlobDeps(root, rg, perm), map[string]any{"pattern": "*.go"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to find files matching *.go" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
	if len(rg.calls) != 0 {
		t.Fatalf("expected ripgrep not to run, got %d calls", len(rg.calls))
	}
}

func TestGlob_RipgrepError(t *testing.T) {
	root := t.TempDir()
	rg := &globTestRipgrep{err: errors.New("boom")}

	result := settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "*.go"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to find files matching *.go" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestGlob_RealRipgrep(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep (rg) not available")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"a.go":     "package a\n",
		"sub/b.go": "package b\n",
		"c.txt":    "c\n",
	} {
		target := filepath.Join(root, name)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rg := core.NewRipgrep(core.NewAppProcess(), globRealBinary{path: rgPath})
	result := settleGlob(t, newGlobDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "**/*.go"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	text, _ := result.Result.Value.(string)
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 results, got %q", text)
	}
	want := map[string]bool{
		filepath.Join(root, "a.go"):        false,
		filepath.Join(root, "sub", "b.go"): false,
	}
	for _, line := range lines {
		if _, ok := want[line]; !ok {
			t.Fatalf("unexpected result path: %q", line)
		}
		want[line] = true
	}
	for path, seen := range want {
		if !seen {
			t.Fatalf("missing result path: %q", path)
		}
	}
}

func TestGlobToModelOutput_Empty(t *testing.T) {
	if got := GlobToModelOutput(nil); got != "No files found" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestGlobToModelOutput_JoinsPaths(t *testing.T) {
	got := GlobToModelOutput([]core.FileSystemEntry{
		{Path: "a.go", Type: "file"},
		{Path: "b.go", Type: "file"},
	})
	if got != "a.go\nb.go" {
		t.Fatalf("unexpected output: %q", got)
	}
}
