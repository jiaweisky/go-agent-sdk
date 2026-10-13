package main

import (
	"context"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// resolveNoopProcess is a stand-in wrapper for the registered-sandbox path.
type resolveNoopProcess struct{}

func (resolveNoopProcess) Run(context.Context, string, []string, ...core.RunOptions) (*core.RunResult, error) {
	return &core.RunResult{}, nil
}

func (resolveNoopProcess) RunStream(
	context.Context, string, []string, ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return func(func(string, error) bool) {}
}

func init() {
	sandbox.Register("agentd-resolve-test", func() core.AppProcess { return resolveNoopProcess{} })
}

func TestResolveSandbox(t *testing.T) {
	// No sandbox config selects the SDK default (nil).
	if p, err := resolveSandbox(config.Server{}); p != nil || err != nil {
		t.Fatalf("no sandbox = %v, %v", p, err)
	}
	// "none" also selects the SDK default.
	if p, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "none"}}); p != nil || err != nil {
		t.Fatalf("none = %v, %v", p, err)
	}
	// A registered wrapper resolves to its process seam.
	p, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "agentd-resolve-test"}})
	if err != nil || p == nil {
		t.Fatalf("registered = %v, %v", p, err)
	}
	// An unknown wrapper with the default (fail-closed) refuses to start.
	if _, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "no-such"}}); err == nil {
		t.Fatal("expected fail-closed to reject an unknown sandbox")
	}
	// fail-open warns and runs without the sandbox.
	if p, err := resolveSandbox(config.Server{
		Sandbox: &config.SandboxConfig{Process: "no-such", FailurePolicy: "fail-open"},
	}); p != nil || err != nil {
		t.Fatalf("fail-open = %v, %v (want nil, nil)", p, err)
	}
}

// TestResolveSandboxBlocksOutsideWrite runs the sandbox a config actually
// resolves to and checks it enforces the boundary end to end: a write outside
// the working directory is denied, a write inside succeeds.
func TestResolveSandboxBlocksOutsideWrite(t *testing.T) {
	wrappers := map[string]string{"darwin": "seatbelt", "linux": "bwrap"}
	binaries := map[string]string{"seatbelt": "sandbox-exec", "bwrap": "bwrap"}
	wrapper, ok := wrappers[runtime.GOOS]
	if !ok {
		t.Skip("no per-command sandbox for " + runtime.GOOS)
	}
	if _, err := exec.LookPath(binaries[wrapper]); err != nil {
		t.Skipf("%s is not available", binaries[wrapper])
	}
	process, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: wrapper}})
	if err != nil || process == nil {
		t.Fatalf("resolveSandbox(%s) = %v, %v", wrapper, process, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir := t.TempDir()

	outside := filepath.Join(home, ".agentd-resolve-sandbox-deny-test")
	blocked, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo bad > " + outside}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if blocked.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatalf("the resolved sandbox allowed a write outside the workdir: %s", outside)
	}

	inside, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo ok > inside.txt"}, core.RunOptions{Dir: dir})
	if err != nil || inside.ExitCode != 0 {
		t.Fatalf("the workdir became unusable: %v (exit %d)", err, inside.ExitCode)
	}
}
