package tool

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type bashRunCall struct {
	Command string
	Args    []string
	Options core.RunOptions
}

type bashTestProcess struct {
	result *core.RunResult
	err    error
	runs   []bashRunCall
}

func (p *bashTestProcess) Run(ctx context.Context, command string, args []string, options ...core.RunOptions) (*core.RunResult, error) {
	var opts core.RunOptions
	if len(options) > 0 {
		opts = options[0]
	}
	p.runs = append(p.runs, bashRunCall{Command: command, Args: args, Options: opts})
	if p.err != nil {
		return nil, p.err
	}
	return p.result, nil
}

func (p *bashTestProcess) RunStream(ctx context.Context, command string, args []string, options ...core.RunStreamOptions) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {}
}

type bashTestPermission struct {
	assertions []core.PermissionAssertInput
	denyAction string
	onAssert   func(core.PermissionAssertInput)
}

func (p *bashTestPermission) Assert(input core.PermissionAssertInput) error {
	p.assertions = append(p.assertions, input)
	if p.onAssert != nil {
		p.onAssert(input)
	}
	if input.Action == p.denyAction {
		return &core.PermissionBlockedError{}
	}
	return nil
}

func (p *bashTestPermission) Ask(input core.PermissionAssertInput) (*core.PermissionAskResult, error) {
	return nil, nil
}
func (p *bashTestPermission) Reply(input core.PermissionReplyInput) error { return nil }
func (p *bashTestPermission) Get(id core.PermissionID) *core.PermissionRequest {
	return nil
}
func (p *bashTestPermission) ForSession(sessionID string) []core.PermissionRequest { return nil }
func (p *bashTestPermission) List() []core.PermissionRequest                       { return nil }
func (p *bashTestPermission) Close() error                                         { return nil }

func bashSuccessResult() *core.RunResult {
	return &core.RunResult{
		Command:         "mock",
		ExitCode:        0,
		Output:          []byte("hello\n"),
		OutputTruncated: false,
	}
}

func newBashDeps(root string, proc core.AppProcess, perm core.Permission, shell string) BashDeps {
	return BashDeps{
		Mutation:   core.NewLocationMutation(&core.Location{LocationInfo: coreschema.LocationInfo{Directory: coreschema.AbsolutePath(root)}}),
		Process:    proc,
		Permission: perm,
		Shell:      shell,
	}
}

func settleBash(t *testing.T, deps BashDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterBash(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_1",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call_1",
			Name:  BashName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func bashResultContent(t *testing.T, settled *Settlement) []schema.ToolContent {
	t.Helper()
	content, ok := settled.Result.Value.([]schema.ToolContent)
	if !ok {
		t.Fatalf("expected content result, got %T (%v)", settled.Result.Value, settled.Result.Value)
	}
	return content
}

func bashStructured(t *testing.T, settled *Settlement) map[string]any {
	t.Helper()
	if settled.Output == nil {
		t.Fatalf("expected structured output")
	}
	structured, ok := settled.Output.Structured.(map[string]any)
	if !ok {
		t.Fatalf("expected structured map, got %T", settled.Output.Structured)
	}
	return structured
}

func TestBash_StructuredOutput(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	proc := &bashTestProcess{result: bashSuccessResult()}
	perm := &bashTestPermission{}
	deps := newBashDeps(root, proc, perm, "")

	registry := NewRegistry()
	if _, err := RegisterBash(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	mat := registry.Materialize(nil)
	if len(mat.Definitions) != 1 || mat.Definitions[0].Name != BashName {
		t.Fatalf("unexpected definitions: %#v", mat.Definitions)
	}
	if mat.Definitions[0].OutputSchema == nil {
		t.Fatalf("expected structured output schema")
	}
	properties, _ := (*mat.Definitions[0].OutputSchema)["properties"].(map[string]any)
	if _, ok := properties["output"]; ok {
		t.Fatalf("structured output schema must not expose output")
	}
	inputProperties, _ := mat.Definitions[0].InputSchema["properties"].(map[string]any)
	if _, ok := inputProperties["background"]; ok {
		t.Fatalf("input schema must not expose background")
	}
	if defs := registry.Materialize([]PermissionRule{{Action: "bash", Resource: "*", Effect: "deny"}}).Definitions; len(defs) != 0 {
		t.Fatalf("expected bash tool to be filtered when denied, got %d", len(defs))
	}

	settled := settleBash(t, deps, map[string]any{"command": "pwd"})
	if settled.Result.Type != "content" {
		t.Fatalf("expected content result, got %q (%v)", settled.Result.Type, settled.Result.Value)
	}
	content := bashResultContent(t, settled)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	if content[0].(schema.ToolTextContent).Text != "hello\n" {
		t.Fatalf("unexpected output text: %#v", content[0])
	}
	if content[1].(schema.ToolTextContent).Text != "Command exited with code 0." {
		t.Fatalf("unexpected summary text: %#v", content[1])
	}
	structured := bashStructured(t, settled)
	if structured["truncated"] != false {
		t.Fatalf("unexpected truncated: %v", structured["truncated"])
	}
	if structured["exit"] != float64(0) {
		t.Fatalf("unexpected exit: %v", structured["exit"])
	}

	if len(proc.runs) != 1 {
		t.Fatalf("expected 1 process run, got %d", len(proc.runs))
	}
	run := proc.runs[0]
	if run.Command != bashDefaultShell() {
		t.Fatalf("unexpected shell: %q", run.Command)
	}
	if len(run.Args) != 2 || run.Args[0] != "-c" || run.Args[1] != "pwd" {
		t.Fatalf("unexpected shell args: %#v", run.Args)
	}
	if run.Options.Dir != realRoot {
		t.Fatalf("unexpected cwd: %q", run.Options.Dir)
	}
	if !run.Options.CombineOutput {
		t.Fatalf("expected combineOutput")
	}
	if run.Options.MaxOutputBytes == nil || *run.Options.MaxOutputBytes != BashMaxCaptureBytes {
		t.Fatalf("unexpected maxOutputBytes: %v", run.Options.MaxOutputBytes)
	}

	if len(perm.assertions) != 1 {
		t.Fatalf("expected 1 permission assertion, got %d", len(perm.assertions))
	}
	if perm.assertions[0].Action != BashName || perm.assertions[0].Resources[0] != "pwd" {
		t.Fatalf("unexpected assertion: %#v", perm.assertions[0])
	}
}

func TestBash_UsesConfiguredShell(t *testing.T) {
	root := t.TempDir()
	shell := "/bin/bash"
	proc := &bashTestProcess{result: bashSuccessResult()}
	deps := newBashDeps(root, proc, &bashTestPermission{}, shell)
	settleBash(t, deps, map[string]any{"command": "pwd"})
	if proc.runs[0].Command != shell {
		t.Fatalf("expected configured shell %q, got %q", shell, proc.runs[0].Command)
	}
}

func TestBash_RelativeWorkdir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	realSrc, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatal(err)
	}
	proc := &bashTestProcess{result: bashSuccessResult()}
	deps := newBashDeps(root, proc, &bashTestPermission{}, "")

	settleBash(t, deps, map[string]any{"command": "pwd", "workdir": "src"})
	if len(proc.runs) != 1 || proc.runs[0].Options.Dir != realSrc {
		t.Fatalf("unexpected cwd: %#v", proc.runs)
	}
}

func TestBash_RejectsWorkdirThatStopsBeingDirectory(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "src")
	if err := os.Mkdir(workdir, 0755); err != nil {
		t.Fatal(err)
	}
	proc := &bashTestProcess{result: bashSuccessResult()}
	perm := &bashTestPermission{}
	perm.onAssert = func(input core.PermissionAssertInput) {
		if input.Action != BashName {
			return
		}
		os.RemoveAll(workdir)
		os.WriteFile(workdir, []byte("not a directory"), 0644)
	}
	deps := newBashDeps(root, proc, perm, "")

	settled := settleBash(t, deps, map[string]any{"command": "pwd", "workdir": "src"})
	if settled.Result.Type != "error" {
		t.Fatalf("expected error result, got %q (%v)", settled.Result.Type, settled.Result.Value)
	}
	if len(proc.runs) != 0 {
		t.Fatalf("expected no process runs, got %d", len(proc.runs))
	}
	if len(perm.assertions) != 1 || perm.assertions[0].Action != BashName {
		t.Fatalf("unexpected assertions: %#v", perm.assertions)
	}
}

func TestBash_RealShellCommand(t *testing.T) {
	if bashDefaultShell() == "cmd.exe" {
		t.Skip("posix shell only")
	}
	root := t.TempDir()
	deps := newBashDeps(root, core.NewAppProcess(), &bashTestPermission{}, "")

	settled := settleBash(t, deps, map[string]any{"command": "printf core-bash"})
	content := bashResultContent(t, settled)
	if content[0].(schema.ToolTextContent).Text != "core-bash" {
		t.Fatalf("unexpected output: %#v", content[0])
	}
	structured := bashStructured(t, settled)
	if structured["exit"] != float64(0) {
		t.Fatalf("unexpected exit: %v", structured["exit"])
	}
	if _, ok := structured["output"]; ok {
		t.Fatalf("structured output must not expose output")
	}
}

func TestBash_ExternalWorkdirApproval(t *testing.T) {
	active := t.TempDir()
	outside := t.TempDir()
	realOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	proc := &bashTestProcess{result: bashSuccessResult()}
	perm := &bashTestPermission{}
	deps := newBashDeps(active, proc, perm, "")

	settleBash(t, deps, map[string]any{"command": "pwd", "workdir": outside})
	if len(perm.assertions) != 2 {
		t.Fatalf("expected 2 assertions, got %d", len(perm.assertions))
	}
	if perm.assertions[0].Action != "external_directory" || perm.assertions[1].Action != BashName {
		t.Fatalf("unexpected assertion order: %#v", perm.assertions)
	}
	expected := strings.ReplaceAll(filepath.Join(realOutside, "*"), "\\", "/")
	if perm.assertions[0].Resources[0] != expected {
		t.Fatalf("unexpected external resource: %q", perm.assertions[0].Resources[0])
	}
	if len(proc.runs) != 1 {
		t.Fatalf("expected 1 process run, got %d", len(proc.runs))
	}
}

func TestBash_DenialDoesNotExecute(t *testing.T) {
	active := t.TempDir()
	outside := t.TempDir()

	proc := &bashTestProcess{result: bashSuccessResult()}
	perm := &bashTestPermission{denyAction: "external_directory"}
	deps := newBashDeps(active, proc, perm, "")
	settleBash(t, deps, map[string]any{"command": "pwd", "workdir": outside})
	if len(perm.assertions) != 1 || perm.assertions[0].Action != "external_directory" {
		t.Fatalf("unexpected assertions: %#v", perm.assertions)
	}
	if len(proc.runs) != 0 {
		t.Fatalf("expected no process runs, got %d", len(proc.runs))
	}

	proc = &bashTestProcess{result: bashSuccessResult()}
	perm = &bashTestPermission{denyAction: BashName}
	deps = newBashDeps(active, proc, perm, "")
	settleBash(t, deps, map[string]any{"command": "pwd"})
	if len(perm.assertions) != 1 || perm.assertions[0].Action != BashName {
		t.Fatalf("unexpected assertions: %#v", perm.assertions)
	}
	if len(proc.runs) != 0 {
		t.Fatalf("expected no process runs, got %d", len(proc.runs))
	}
}

func TestBash_ExternalArgumentWarnings(t *testing.T) {
	active := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	proc := &bashTestProcess{result: bashSuccessResult()}
	perm := &bashTestPermission{denyAction: "external_directory"}
	deps := newBashDeps(active, proc, perm, "")

	settled := settleBash(t, deps, map[string]any{"command": "cat " + target})
	if len(perm.assertions) != 1 || perm.assertions[0].Action != BashName {
		t.Fatalf("unexpected assertions: %#v", perm.assertions)
	}
	if len(proc.runs) != 1 {
		t.Fatalf("expected 1 process run, got %d", len(proc.runs))
	}
	structured := bashStructured(t, settled)
	if structured["truncated"] != false {
		t.Fatalf("unexpected truncated: %v", structured["truncated"])
	}
	if _, ok := structured["warnings"]; ok {
		t.Fatalf("structured output must not expose warnings")
	}
	content := bashResultContent(t, settled)
	if !strings.Contains(content[1].(schema.ToolTextContent).Text, "Warnings:") {
		t.Fatalf("expected warnings in summary, got %#v", content[1])
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	root := t.TempDir()
	result := bashSuccessResult()
	result.ExitCode = 7
	result.Output = []byte("HEAD full output TAIL")
	proc := &bashTestProcess{result: result}
	deps := newBashDeps(root, proc, &bashTestPermission{}, "")

	settled := settleBash(t, deps, map[string]any{"command": "false"})
	content := bashResultContent(t, settled)
	if !strings.Contains(content[1].(schema.ToolTextContent).Text, "Command exited with code 7") {
		t.Fatalf("unexpected summary: %#v", content[1])
	}
	if content[0].(schema.ToolTextContent).Text != "HEAD full output TAIL" {
		t.Fatalf("unexpected output: %#v", content[0])
	}
	structured := bashStructured(t, settled)
	if structured["exit"] != float64(7) || structured["truncated"] != false {
		t.Fatalf("unexpected structured output: %#v", structured)
	}
}

func TestBash_Truncation(t *testing.T) {
	root := t.TempDir()
	result := bashSuccessResult()
	result.OutputTruncated = true
	proc := &bashTestProcess{result: result}
	deps := newBashDeps(root, proc, &bashTestPermission{}, "")

	settled := settleBash(t, deps, map[string]any{"command": "verbose"})
	structured := bashStructured(t, settled)
	if structured["truncated"] != true {
		t.Fatalf("expected truncated structured output: %#v", structured)
	}
	content := bashResultContent(t, settled)
	if !strings.Contains(content[0].(schema.ToolTextContent).Text, "output capture truncated") {
		t.Fatalf("expected truncation notice, got %#v", content[0])
	}
}

func TestBash_Timeout(t *testing.T) {
	root := t.TempDir()
	proc := &bashTestProcess{err: &core.AppProcessError{Command: "sleep", Cause: core.ErrTimedOut}}
	deps := newBashDeps(root, proc, &bashTestPermission{}, "")

	settled := settleBash(t, deps, map[string]any{"command": "sleep 60", "timeout": 10})
	content := bashResultContent(t, settled)
	if !strings.Contains(content[1].(schema.ToolTextContent).Text, "Command timed out") {
		t.Fatalf("unexpected summary: %#v", content[1])
	}
	structured := bashStructured(t, settled)
	if structured["timeout"] != true || structured["truncated"] != false {
		t.Fatalf("unexpected structured output: %#v", structured)
	}
}

func TestBashModelOutput(t *testing.T) {
	if got := BashModelOutput(BashOutput{}); got != "Command exited with code 0." {
		t.Fatalf("unexpected output: %q", got)
	}
	timedOut := true
	got := BashModelOutput(BashOutput{Timeout: &timedOut, Warnings: []string{"a", "b"}})
	want := "Warnings:\n- a\n- b\n\nCommand timed out before completion."
	if got != want {
		t.Fatalf("unexpected output:\n got %q\nwant %q", got, want)
	}
}

func TestBashExternalCommandDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	realOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	command := `cat "` + filepath.Join(outside, "a.txt") + `" '` + filepath.Join(outside, "b.txt") + `' ;`
	directories, err := bashExternalCommandDirectories(command, root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	expected := strings.ReplaceAll(realOutside, "\\", "/")
	if len(directories) != 1 || strings.ReplaceAll(directories[0], "\\", "/") != expected {
		t.Fatalf("unexpected directories: %#v", directories)
	}
}
