package main

import (
	"context"
	"iter"
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
