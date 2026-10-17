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
	sandbox.Register("agentd-resolve-test", func(sandbox.Policy) core.AppProcess { return resolveNoopProcess{} })
}

func TestResolveSandbox(t *testing.T) {
	// No sandbox config selects the SDK default (nil).
	if p, _, err := resolveSandbox(config.Server{}); p != nil || err != nil {
		t.Fatalf("no sandbox = %v, %v", p, err)
	}
	// "none" also selects the SDK default.
	if p, _, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "none"}}); p != nil || err != nil {
		t.Fatalf("none = %v, %v", p, err)
	}
	// A registered wrapper resolves to its process seam.
	p, policy, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "agentd-resolve-test"}})
	if err != nil || p == nil {
		t.Fatalf("registered = %v, %v", p, err)
	}
	if policy.ReadMode != sandbox.FSModeAllow || policy.WriteMode != sandbox.FSModeAllow {
		t.Fatalf("standard policy = %+v, want allow-list reads and writes", policy)
	}
	// An unknown wrapper with the default (fail-closed) refuses to start.
	if _, _, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "no-such"}}); err == nil {
		t.Fatal("expected fail-closed to reject an unknown sandbox")
	}
	// fail-open warns and runs without the sandbox, but keeps the guard policy.
	if p, policy, err := resolveSandbox(config.Server{
		Sandbox: &config.SandboxConfig{Process: "no-such", FailurePolicy: "fail-open"},
	}); p != nil || err != nil || policy.WriteMode != sandbox.FSModeAllow {
		t.Fatalf("fail-open = %v, %+v, %v", p, policy, err)
	}
	// An explicit profile off disables both the wrapper and the guards.
	if p, policy, err := resolveSandbox(config.Server{
		Sandbox: &config.SandboxConfig{Process: "agentd-resolve-test", SandboxPolicy: config.SandboxPolicy{Profile: "off"}},
	}); p != nil || err != nil || policy.WriteMode == sandbox.FSModeAllow {
		t.Fatalf("profile off = %v, %+v, %v", p, policy, err)
	}
}

func TestSandboxGuards(t *testing.T) {
	read, write := sandboxGuards(config.Server{Sandbox: &config.SandboxConfig{Process: "none"}}, sandbox.Policy{})
	if read != nil || write != nil {
		t.Fatal("a disabled sandbox must not install guards")
	}
	policy := sandbox.StandardPolicy(t.TempDir())
	read, write = sandboxGuards(config.Server{Sandbox: &config.SandboxConfig{Process: "seatbelt"}}, policy)
	if read == nil || write == nil {
		t.Fatal("the standard policy must install both guards")
	}

	custom := policy
	custom.ReadMode = sandbox.FSModeOff
	read, write = sandboxGuards(config.Server{Sandbox: &config.SandboxConfig{Process: "seatbelt"}}, custom)
	if read != nil || write == nil {
		t.Fatal("read: off must drop the read guard only")
	}
}

// TestResolveSandboxBlocksOutsideWrite runs the sandbox a config actually
// resolves to and checks it enforces the boundary end to end: a write outside
// the working directory is denied, a write inside succeeds.
func TestResolveSandboxBlocksOutsideWrite(t *testing.T) {
	wrappers := map[string]string{"darwin": "seatbelt", "linux": "bwrap", "windows": "appcontainer"}
	shells := map[string]string{"darwin": "/bin/sh", "linux": "/bin/sh", "windows": "cmd"}
	binaries := map[string]string{"seatbelt": "sandbox-exec", "bwrap": "bwrap"}
	wrapper, ok := wrappers[runtime.GOOS]
	if !ok {
		t.Skip("no per-command sandbox for " + runtime.GOOS)
	}
	if binary, needs := binaries[wrapper]; needs {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is not available", binary)
		}
	}
	shell := shells[runtime.GOOS]
	quote := func(command string) []string {
		if runtime.GOOS == "windows" {
			return []string{"/c", command}
		}
		return []string{"-c", command}
	}
	dir := t.TempDir()
	process, _, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: wrapper}, WorkingDirectory: dir})
	if err != nil || process == nil {
		t.Fatalf("resolveSandbox(%s) = %v, %v", wrapper, process, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}

	outside := filepath.Join(home, ".agentd-resolve-sandbox-deny-test")
	blocked, err := process.Run(context.Background(), shell,
		quote("echo bad > "+outside), core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if blocked.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatalf("the resolved sandbox allowed a write outside the workdir: %s", outside)
	}

	inside, err := process.Run(context.Background(), shell,
		quote("echo ok > inside.txt"), core.RunOptions{Dir: dir})
	if err != nil || inside.ExitCode != 0 {
		t.Fatalf("the workdir became unusable: %v (exit %d)", err, inside.ExitCode)
	}
}
