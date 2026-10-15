package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestAgentTempDir(t *testing.T) {
	a := agentTempDir("/state/a")
	if a != agentTempDir("/state/a") {
		t.Fatal("agentTempDir must be deterministic")
	}
	if b := agentTempDir("/state/b"); a == b {
		t.Fatal("different roots must not share a temp dir")
	}
	if !strings.HasPrefix(a, filepath.Clean(os.TempDir())+string(filepath.Separator)) {
		t.Fatalf("temp dir %q must live under the OS temp dir %q", a, os.TempDir())
	}
	if !strings.Contains(a, "go-agent-sdk") {
		t.Fatalf("temp dir %q is missing the app segment", a)
	}
}

// envRecordingProcess captures the RunOptions a call is made with.
type envRecordingProcess struct {
	env       map[string]string
	extendEnv bool
}

func (p *envRecordingProcess) Run(
	_ context.Context, command string, _ []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	if len(options) > 0 {
		p.env = options[0].Env
		p.extendEnv = options[0].ExtendEnv
	}
	return &core.RunResult{Command: command}, nil
}

func (p *envRecordingProcess) RunStream(
	_ context.Context, _ string, _ []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	if len(options) > 0 {
		p.env = options[0].Env
		p.extendEnv = options[0].ExtendEnv
	}
	return func(func(string, error) bool) {}
}

func TestWithTempEnvInjectsVars(t *testing.T) {
	inner := &envRecordingProcess{}
	process := withTempEnv(inner, "/root/tmp")
	// No env on the call (as bash/ripgrep/git do): the base environment must be
	// inherited, so ExtendEnv is forced on.
	if _, err := process.Run(context.Background(), "bash", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		if inner.env[key] != "/root/tmp" {
			t.Errorf("%s = %q, want /root/tmp", key, inner.env[key])
		}
	}
	if !inner.extendEnv {
		t.Error("ExtendEnv must be set so the base environment (PATH) survives")
	}
}

func TestWithTempEnvKeepsCallerEnv(t *testing.T) {
	inner := &envRecordingProcess{}
	process := withTempEnv(inner, "/root/tmp")
	original := map[string]string{"KEEP": "1"}
	if _, err := process.Run(context.Background(), "bash", nil, core.RunOptions{Env: original}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if inner.env["KEEP"] != "1" {
		t.Error("the tool's own env was dropped")
	}
	if inner.extendEnv {
		t.Error("a caller that set its own env keeps its ExtendEnv")
	}
	if _, mutated := original["TMPDIR"]; mutated {
		t.Error("the caller's env map was mutated")
	}
}

func TestWithTempEnvPreservesExtendEnv(t *testing.T) {
	inner := &envRecordingProcess{}
	process := withTempEnv(inner, "/root/tmp")
	options := []core.RunOptions{{Env: map[string]string{"A": "1"}, ExtendEnv: true}}
	if _, err := process.Run(context.Background(), "git", nil, options...); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !inner.extendEnv {
		t.Error("ExtendEnv=true must be preserved")
	}
}

func TestWithTempEnvStream(t *testing.T) {
	inner := &envRecordingProcess{}
	process := withTempEnv(inner, "/root/tmp")
	for range process.RunStream(context.Background(), "rg", nil) {
	}
	if inner.env["TMPDIR"] != "/root/tmp" {
		t.Fatalf("RunStream did not inject TMPDIR: %v", inner.env)
	}
}

func TestWithTempEnvEmptyIsNoop(t *testing.T) {
	inner := &envRecordingProcess{}
	if got := withTempEnv(inner, ""); got != core.AppProcess(inner) {
		t.Fatal("an empty temp directory must be a no-op")
	}
}
