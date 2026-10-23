package sandboxpolicy

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
)

func TestForServerStandard(t *testing.T) {
	cfg := &config.SandboxConfig{Wrapper: "seatbelt"}
	policy := ForServer(cfg, "/work")
	if policy.ReadMode != sandbox.FSModeAllow || policy.WriteMode != sandbox.FSModeAllow {
		t.Fatalf("standard policy modes = %q/%q", policy.ReadMode, policy.WriteMode)
	}
	if policy.NetworkMode != sandbox.NetworkNoEgress || !policy.DenyProcessInfo {
		t.Fatalf("standard policy = %+v", policy)
	}
	if len(policy.ScrubEnv) == 0 {
		t.Fatal("the standard policy must scrub secret environment variables")
	}
}

func TestForServerProfileOff(t *testing.T) {
	cfg := &config.SandboxConfig{Wrapper: "seatbelt", SandboxPolicy: config.SandboxPolicy{Profile: "off"}}
	if policy := ForServer(cfg, "/work"); policy.ReadMode == sandbox.FSModeAllow {
		t.Fatalf("profile off must not confine reads: %+v", policy)
	}
}

func TestForServerCustom(t *testing.T) {
	cfg := &config.SandboxConfig{
		Wrapper: "seatbelt",
		SandboxPolicy: config.SandboxPolicy{
			Profile: "custom",
			Filesystem: &config.SandboxFilesystem{
				Read:  "off",
				Write: "allow",
			},
			Network: &config.SandboxNetwork{Mode: "off"},
		},
	}
	policy := ForServer(cfg, "/work")
	if policy.ReadMode != sandbox.FSModeOff {
		t.Fatalf("custom read: off = %q", policy.ReadMode)
	}
	if policy.WriteMode != sandbox.FSModeAllow {
		t.Fatalf("custom write: allow = %q", policy.WriteMode)
	}
	if policy.NetworkMode != sandbox.NetworkOff {
		t.Fatalf("custom network: off = %q", policy.NetworkMode)
	}
}

func TestForServerCustomDefaults(t *testing.T) {
	cfg := &config.SandboxConfig{Wrapper: "seatbelt", SandboxPolicy: config.SandboxPolicy{Profile: "custom"}}
	policy := ForServer(cfg, "/work")
	if policy.ReadMode != sandbox.FSModeOff || policy.WriteMode != sandbox.FSModeOff {
		t.Fatalf("custom read/write = %q/%q, want off", policy.ReadMode, policy.WriteMode)
	}
	if policy.NetworkMode != sandbox.NetworkOff || policy.SocketMode != sandbox.SocketAllowAll {
		t.Fatalf("custom network/socket = %q/%q", policy.NetworkMode, policy.SocketMode)
	}
}

func TestForServerViolations(t *testing.T) {
	cfg := &config.SandboxConfig{
		Wrapper: "seatbelt",
		SandboxPolicy: config.SandboxPolicy{
			Violations: &config.SandboxViolations{Mode: "heuristic"},
		},
	}
	policy := ForServer(cfg, "/work")
	if policy.ViolationMode != sandbox.ViolationHeuristic || !policy.ReportToModel {
		t.Fatalf("violation policy = %+v", policy)
	}
	off := false
	cfg.Violations.ReportToModel = &off
	if ForServer(cfg, "/work").ReportToModel {
		t.Fatal("reportToModel: false must be honored")
	}
}

func TestForServerSocketAlias(t *testing.T) {
	cfg := &config.SandboxConfig{
		Wrapper:       "seatbelt",
		SandboxPolicy: config.SandboxPolicy{UnixSockets: &config.SandboxUnixSockets{Mode: "deny"}},
	}
	if mode := ForServer(cfg, "/work").SocketMode; mode != sandbox.SocketInherit {
		t.Fatalf("socket mode = %q, want inherit", mode)
	}
}

func TestForWrapDisabled(t *testing.T) {
	if policy := ForWrap(nil); policy.ReadMode != "" {
		t.Fatalf("nil wrap policy = %+v, want zero", policy)
	}
	if policy := ForWrap(&config.WrapConfig{Wrapper: "none"}); policy.ReadMode != "" {
		t.Fatalf("wrapper none policy = %+v, want zero", policy)
	}
}

func TestForWrapEnabled(t *testing.T) {
	policy := ForWrap(&config.WrapConfig{Wrapper: "bwrap"})
	if policy.ReadMode != sandbox.FSModeAllow || policy.WorkingDirectory != "" {
		t.Fatalf("wrap policy = %+v", policy)
	}
}
