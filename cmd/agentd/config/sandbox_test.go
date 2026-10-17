package config

import (
	"runtime"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
)

func TestSandboxStandardPolicy(t *testing.T) {
	cfg := &SandboxConfig{Process: "seatbelt"}
	policy := cfg.Policy("/work")
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

func TestSandboxProfileOff(t *testing.T) {
	cfg := &SandboxConfig{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "off"}}
	if cfg.Enabled() {
		t.Fatal("profile off must disable the sandbox")
	}
	if policy := cfg.Policy("/work"); policy.ReadMode == sandbox.FSModeAllow {
		t.Fatalf("profile off must not confine reads: %+v", policy)
	}
}

func TestSandboxCustomOff(t *testing.T) {
	cfg := &SandboxConfig{
		Process: "seatbelt",
		SandboxPolicy: SandboxPolicy{
			Profile: "custom",
			Filesystem: &SandboxFilesystem{
				Read:  "off",
				Write: "allow",
			},
			Network: &SandboxNetwork{Mode: "off"},
		},
	}
	policy := cfg.Policy("/work")
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

func TestValidateSandboxPolicy(t *testing.T) {
	valid := []*SandboxConfig{
		{Process: "none"},
		{Process: "seatbelt"},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "off"}},
		{Process: "bwrap", SandboxPolicy: SandboxPolicy{Profile: "custom", Filesystem: &SandboxFilesystem{Read: "allow", Write: "allow"}}},
	}
	for _, cfg := range valid {
		if err := ValidateSandbox(cfg); err != nil {
			t.Fatalf("valid sandbox rejected: %v (%+v)", err, cfg)
		}
	}

	invalid := []*SandboxConfig{
		{},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Network: &SandboxNetwork{Mode: "allowlist"}}},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{Read: "maybe"}}},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "bogus"}},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "allowlist"}}},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "inherit", Allow: []string{"/run/x.sock"}}}},
		{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Network: &SandboxNetwork{AllowedDomains: []string{"api.deepseek.com"}}}},
	}
	for _, cfg := range invalid {
		if err := ValidateSandbox(cfg); err == nil {
			t.Fatalf("invalid sandbox accepted: %+v", cfg)
		}
	}

	if runtime.GOOS == "linux" {
		if err := ValidateSandbox(&SandboxConfig{
			Process: "bwrap",
			SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{
				DenyWrite: []string{"**/.env"},
			}},
		}); err == nil {
			t.Fatal("a write glob must be rejected on linux")
		}
	}
}

func TestSandboxViolationConfig(t *testing.T) {
	cfg := &SandboxConfig{
		Process: "seatbelt",
		SandboxPolicy: SandboxPolicy{
			Violations: &SandboxViolations{Mode: "heuristic"},
		},
	}
	policy := cfg.Policy("/work")
	if policy.ViolationMode != sandbox.ViolationHeuristic || !policy.ReportToModel {
		t.Fatalf("violation policy = %+v", policy)
	}
	off := false
	cfg.Violations.ReportToModel = &off
	if cfg.Policy("/work").ReportToModel {
		t.Fatal("reportToModel: false must be honored")
	}

	if runtime.GOOS != "linux" {
		precise := &SandboxConfig{
			Process:       "seatbelt",
			SandboxPolicy: SandboxPolicy{Violations: &SandboxViolations{Mode: "precise"}},
		}
		if err := ValidateSandbox(precise); err == nil {
			t.Fatalf("precise must be rejected on %s", runtime.GOOS)
		}
	}
}

// TestValidateSandboxDisabledPolicyFields guards against silently ignoring a
// policy when the sandbox is disabled.
func TestValidateSandboxDisabledPolicyFields(t *testing.T) {
	bad := &SandboxConfig{
		Process:       "none",
		SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{Read: "off"}},
	}
	if err := ValidateSandbox(bad); err == nil {
		t.Fatal("policy fields with process none must be rejected")
	}
	off := &SandboxConfig{
		Process:       "seatbelt",
		SandboxPolicy: SandboxPolicy{Profile: "off", Network: &SandboxNetwork{Mode: "off"}},
	}
	if err := ValidateSandbox(off); err == nil {
		t.Fatal("policy fields with profile off must be rejected")
	}
}

// TestSandboxSocketInheritAlias checks the pre-rename "deny" value normalizes to
// inherit.
func TestSandboxSocketInheritAlias(t *testing.T) {
	cfg := &SandboxConfig{
		Process:       "seatbelt",
		SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "deny"}},
	}
	if err := ValidateSandbox(cfg); err != nil {
		t.Fatalf("the deny alias must validate: %v", err)
	}
	if mode := cfg.Policy("/work").SocketMode; mode != sandbox.SocketInherit {
		t.Fatalf("socket mode = %q, want inherit", mode)
	}
}

// TestSandboxCustomDefaults checks the custom profile starts from no
// confinement and opts in explicitly.
func TestSandboxCustomDefaults(t *testing.T) {
	cfg := &SandboxConfig{Process: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "custom"}}
	policy := cfg.Policy("/work")
	if policy.ReadMode != sandbox.FSModeOff || policy.WriteMode != sandbox.FSModeOff {
		t.Fatalf("custom read/write = %q/%q, want off", policy.ReadMode, policy.WriteMode)
	}
	if policy.NetworkMode != sandbox.NetworkOff || policy.SocketMode != sandbox.SocketAllowAll {
		t.Fatalf("custom network/socket = %q/%q", policy.NetworkMode, policy.SocketMode)
	}
}

// TestValidateSandboxPlatformOff guards the platform-specific "off" checks on
// every host by passing the OS explicitly.
func TestValidateSandboxPlatformOff(t *testing.T) {
	for _, fs := range []*SandboxFilesystem{{Read: "off"}, {Write: "off"}} {
		cfg := &SandboxConfig{Process: "appcontainer", SandboxPolicy: SandboxPolicy{Filesystem: fs}}
		if err := validatePlatformSandboxFor("windows", cfg); err == nil {
			t.Fatalf("windows must reject filesystem off: %+v", fs)
		}
	}
	cfg := &SandboxConfig{Process: "appcontainer", SandboxPolicy: SandboxPolicy{ProcessInspection: "allow"}}
	if err := validatePlatformSandboxFor("windows", cfg); err == nil {
		t.Fatal("windows must reject processInspection: allow")
	}

	bad := &SandboxConfig{Process: "bwrap", SandboxPolicy: SandboxPolicy{
		Filesystem: &SandboxFilesystem{Write: "off"},
	}}
	if err := validatePlatformSandboxFor("linux", bad); err == nil {
		t.Fatal("linux must reject write off with read allow")
	}
	ok := &SandboxConfig{Process: "bwrap", SandboxPolicy: SandboxPolicy{
		Filesystem: &SandboxFilesystem{Read: "off", Write: "off"},
	}}
	if err := validatePlatformSandboxFor("linux", ok); err != nil {
		t.Fatalf("read off + write off must validate on linux: %v", err)
	}
	if err := validatePlatformSandboxFor("darwin", bad); err != nil {
		t.Fatalf("darwin must allow write off with read allow: %v", err)
	}
}

func TestSandboxGuardsForPolicies(t *testing.T) {
	cfg := &SandboxConfig{Process: "seatbelt"}
	policy := cfg.Policy("/work")
	if policy.ReadMode == sandbox.FSModeOff || policy.WriteMode == sandbox.FSModeOff {
		t.Fatalf("standard policy unexpectedly off: %+v", policy)
	}
}
