package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// recordingProcess is a core.AppProcess that records the commands it is asked
// to run and always succeeds with empty output.
type recordingProcess struct {
	commands []string
}

func (p *recordingProcess) Run(
	_ context.Context, command string, _ []string, _ ...core.RunOptions,
) (*core.RunResult, error) {
	p.commands = append(p.commands, command)
	return &core.RunResult{Command: command, ExitCode: 0}, nil
}

func (p *recordingProcess) RunStream(
	_ context.Context, command string, _ []string, _ ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	p.commands = append(p.commands, command)
	return func(func(string, error) bool) {}
}

func TestAppProcessFallback(t *testing.T) {
	if appProcess(&ClientOptions{}) == nil {
		t.Fatal("expected the default process when none is injected")
	}
	proc := &recordingProcess{}
	if got := appProcess(&ClientOptions{Process: proc}); got != core.AppProcess(proc) {
		t.Fatalf("injected process was not returned: %T", got)
	}
}

// TestClientUsesInjectedProcess verifies the seam reaches both the built-in
// tools (bash/ripgrep) and the git used for the project location, so a single
// injection applies everywhere.
func TestClientUsesInjectedProcess(t *testing.T) {
	root := t.TempDir()
	// A .git directory makes the runtime discover a repository, exercising the
	// git seam during client construction.
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	proc := &recordingProcess{}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: root, Permission: ApproveAll,
		LLM: fakeLLM{reply: "hello"}, Process: proc,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if client.rt.builtin.process != core.AppProcess(proc) {
		t.Fatal("built-in tools did not receive the injected process")
	}
	if !slices.Contains(proc.commands, "git") {
		t.Fatalf("git did not run through the injected process; calls=%v", proc.commands)
	}
}
