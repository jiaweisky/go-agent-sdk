package main

import (
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// TestResolveSandbox covers the sandbox failure policy: no sandbox and "none"
// yield the SDK default (nil); an unknown wrapper fails closed by default and
// degrades to unsandboxed only under fail-open.
func TestResolveSandbox(t *testing.T) {
	if p, err := resolveSandbox(config.Server{}); err != nil || p != nil {
		t.Fatalf("no sandbox: p=%v err=%v", p, err)
	}
	if p, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "none"}}); err != nil || p != nil {
		t.Fatalf("sandbox=none: p=%v err=%v", p, err)
	}
	if _, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "bwrap"}}); err == nil {
		t.Fatal("expected an unknown sandbox to fail closed")
	}
	if p, err := resolveSandbox(config.Server{Sandbox: &config.SandboxConfig{Process: "bwrap", FailurePolicy: "fail-open"}}); err != nil || p != nil {
		t.Fatalf("fail-open: p=%v err=%v", p, err)
	}
}
