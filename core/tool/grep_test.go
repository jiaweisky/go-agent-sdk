package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/schema"
)

type grepRealBinary struct{ path string }

func (b grepRealBinary) Filepath() (string, error) { return b.path, nil }

type grepTestRipgrep struct {
	matches []core.FileSystemMatch
	err     error
	calls   []core.RipgrepGrepInput
}

func (g *grepTestRipgrep) Grep(input core.RipgrepGrepInput) ([]core.FileSystemMatch, error) {
	g.calls = append(g.calls, input)
	if g.err != nil {
		return nil, g.err
	}
	for i := range g.matches {
		if g.matches[i].Submatches == nil {
			g.matches[i].Submatches = []core.FileSystemSubmatch{}
		}
	}
	return g.matches, nil
}

func (g *grepTestRipgrep) Glob(core.RipgrepGlobInput) ([]core.FileSystemEntry, error) {
	return nil, nil
}

func (g *grepTestRipgrep) Find(core.RipgrepFindInput) ([]core.FileSystemEntry, error) {
	return nil, nil
}

func settleGrep(t *testing.T, deps GrepDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterGrep(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  GrepName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func newGrepDeps(root string, rg core.Ripgrep, perm core.Permission) GrepDeps {
	return GrepDeps{Ripgrep: rg, Directory: root, Permission: perm}
}

func TestGrep_ModelOutputResolvesAndFormats(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{matches: []core.FileSystemMatch{
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 1, Offset: 0, Text: "hello world"},
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 3, Offset: 12, Text: "world again"},
		{Entry: core.FileSystemEntry{Path: "sub/b.txt", Type: "file"}, Line: 2, Offset: 4, Text: "world"},
	}}

	result := settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	expected := "Found 3 matches\n" +
		filepath.Join(root, "a.txt") + ":\n" +
		"  Line 1: hello world\n" +
		"  Line 3: world again\n" +
		"\n" +
		filepath.Join(root, "sub", "b.txt") + ":\n" +
		"  Line 2: world"
	if result.Result.Value != expected {
		t.Fatalf("unexpected model output:\n%q\nwant:\n%q", result.Result.Value, expected)
	}
}

func TestGrep_NoMatchesFound(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{}

	result := settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "nope"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q", result.Result.Type)
	}
	if result.Result.Value != "No files found" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestGrep_PassesCwdAndLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	rg := &grepTestRipgrep{}
	deps := newGrepDeps(root, rg, &bashTestPermission{})

	settleGrep(t, deps, map[string]any{"pattern": "world", "path": "src", "include": "*.go", "limit": 5})

	if len(rg.calls) != 1 {
		t.Fatalf("expected 1 ripgrep call, got %d", len(rg.calls))
	}
	call := rg.calls[0]
	if call.Cwd != filepath.Join(root, "src") {
		t.Fatalf("unexpected cwd: %s", call.Cwd)
	}
	if call.Pattern != "world" {
		t.Fatalf("unexpected pattern: %s", call.Pattern)
	}
	if call.Include == nil || *call.Include != "*.go" {
		t.Fatalf("unexpected include: %v", call.Include)
	}
	if call.File != nil {
		t.Fatalf("expected no file, got %v", *call.File)
	}
	if call.Limit != 5 {
		t.Fatalf("unexpected limit: %d", call.Limit)
	}
}

func TestGrep_FileTargetSetsFileAndParentCwd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rg := &grepTestRipgrep{matches: []core.FileSystemMatch{
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 1, Offset: 0, Text: "hello world"},
	}}

	result := settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world", "path": "a.txt"})

	call := rg.calls[0]
	if call.Cwd != root {
		t.Fatalf("unexpected cwd: %s", call.Cwd)
	}
	if call.File == nil || *call.File != "a.txt" {
		t.Fatalf("unexpected file: %v", call.File)
	}
	if result.Result.Value != "Found 1 matches\n"+filepath.Join(root, "a.txt")+":\n  Line 1: hello world" {
		t.Fatalf("unexpected model output: %v", result.Result.Value)
	}
}

func TestGrep_DirectoryTargetUsesDirectoryCwd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	rg := &grepTestRipgrep{}

	settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world", "path": "src"})

	call := rg.calls[0]
	if call.Cwd != filepath.Join(root, "src") {
		t.Fatalf("unexpected cwd: %s", call.Cwd)
	}
	if call.File != nil {
		t.Fatalf("expected no file, got %v", *call.File)
	}
}

func TestGrep_MissingTargetUsesParentCwd(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{}

	settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world", "path": "missing.txt"})

	call := rg.calls[0]
	if call.Cwd != root {
		t.Fatalf("unexpected cwd: %s", call.Cwd)
	}
	if call.File != nil {
		t.Fatalf("expected no file, got %v", *call.File)
	}
}

func TestGrep_DefaultLimitIsMaxSafeInteger(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{}

	settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world"})

	if len(rg.calls) != 1 {
		t.Fatalf("expected 1 ripgrep call, got %d", len(rg.calls))
	}
	if rg.calls[0].Limit != grepDefaultLimit() {
		t.Fatalf("unexpected default limit: %d", rg.calls[0].Limit)
	}
}

func TestGrep_DefaultCwdIsLocation(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{}

	settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world"})

	if rg.calls[0].Cwd != root {
		t.Fatalf("unexpected default cwd: %s", rg.calls[0].Cwd)
	}
}

func TestGrep_PermissionAssertion(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{}
	perm := &bashTestPermission{}

	settleGrep(t, newGrepDeps(root, rg, perm), map[string]any{"pattern": "world", "path": "src", "include": "*.go", "limit": 3})

	if len(perm.assertions) != 1 {
		t.Fatalf("expected 1 permission assertion, got %d", len(perm.assertions))
	}
	assertion := perm.assertions[0]
	if assertion.Action != GrepName {
		t.Fatalf("unexpected action: %s", assertion.Action)
	}
	if len(assertion.Resources) != 1 || assertion.Resources[0] != "world" {
		t.Fatalf("unexpected resources: %v", assertion.Resources)
	}
	if len(assertion.Save) != 1 || assertion.Save[0] != "*" {
		t.Fatalf("unexpected save: %v", assertion.Save)
	}
	if assertion.Metadata["root"] != "." {
		t.Fatalf("unexpected metadata root: %v", assertion.Metadata["root"])
	}
	if assertion.Metadata["path"] != "src" {
		t.Fatalf("unexpected metadata path: %v", assertion.Metadata["path"])
	}
	if assertion.Metadata["include"] != "*.go" {
		t.Fatalf("unexpected metadata include: %v", assertion.Metadata["include"])
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

func TestGrep_DefaultMetadataRoot(t *testing.T) {
	root := t.TempDir()
	perm := &bashTestPermission{}

	settleGrep(t, newGrepDeps(root, &grepTestRipgrep{}, perm), map[string]any{"pattern": "world"})

	if perm.assertions[0].Metadata["root"] != "." {
		t.Fatalf("unexpected metadata root: %v", perm.assertions[0].Metadata["root"])
	}
	if _, ok := perm.assertions[0].Metadata["path"]; ok {
		t.Fatalf("expected no path metadata key, got %v", perm.assertions[0].Metadata["path"])
	}
	if _, ok := perm.assertions[0].Metadata["include"]; ok {
		t.Fatalf("expected no include metadata key, got %v", perm.assertions[0].Metadata["include"])
	}
	if _, ok := perm.assertions[0].Metadata["limit"]; ok {
		t.Fatalf("expected no limit metadata key, got %v", perm.assertions[0].Metadata["limit"])
	}
}

func TestGrep_DeniedPermission(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{matches: []core.FileSystemMatch{
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 1, Text: "world"},
	}}
	perm := &bashTestPermission{denyAction: GrepName}

	result := settleGrep(t, newGrepDeps(root, rg, perm), map[string]any{"pattern": "world"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to grep for world" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
	if len(rg.calls) != 0 {
		t.Fatalf("expected ripgrep not to run, got %d calls", len(rg.calls))
	}
}

func TestGrep_RipgrepError(t *testing.T) {
	root := t.TempDir()
	rg := &grepTestRipgrep{err: errors.New("boom")}

	result := settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world"})
	if result.Result.Type != "error" {
		t.Fatalf("expected error result, got %q", result.Result.Type)
	}
	if result.Result.Value != "Unable to grep for world" {
		t.Fatalf("unexpected error: %v", result.Result.Value)
	}
}

func TestGrep_RealRipgrep(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep (rg) not available")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"a.txt":     "hello world\nsecond line\n",
		"sub/b.txt": "no match here\nworld again\n",
	} {
		target := filepath.Join(root, name)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rg := core.NewRipgrep(core.NewAppProcess(), grepRealBinary{path: rgPath})
	result := settleGrep(t, newGrepDeps(root, rg, &bashTestPermission{}), map[string]any{"pattern": "world"})
	if result.Result.Type != "text" {
		t.Fatalf("expected text result, got %q (%v)", result.Result.Type, result.Result.Value)
	}
	text, _ := result.Result.Value.(string)
	if !strings.HasPrefix(text, "Found 2 matches\n") {
		t.Fatalf("unexpected header: %q", text)
	}
	for _, block := range []string{
		filepath.Join(root, "a.txt") + ":\n  Line 1: hello world\n",
		filepath.Join(root, "sub", "b.txt") + ":\n  Line 2: world again\n",
	} {
		if !strings.Contains(text, block) {
			t.Fatalf("missing block %q in %q", block, text)
		}
	}
}

func TestGrepToModelOutput_Empty(t *testing.T) {
	if got := GrepToModelOutput(nil); got != "No files found" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestGrepToModelOutput_GroupsByFile(t *testing.T) {
	got := GrepToModelOutput([]core.FileSystemMatch{
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 1, Text: "one"},
		{Entry: core.FileSystemEntry{Path: "a.txt", Type: "file"}, Line: 2, Text: "two"},
		{Entry: core.FileSystemEntry{Path: "b.txt", Type: "file"}, Line: 3, Text: "three"},
	})
	expected := "Found 3 matches\na.txt:\n  Line 1: one\n  Line 2: two\n\nb.txt:\n  Line 3: three"
	if got != expected {
		t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, expected)
	}
}
