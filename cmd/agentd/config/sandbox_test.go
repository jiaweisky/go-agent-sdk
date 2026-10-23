package config

import (
	"runtime"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandboxmode"
)

func TestSandboxProfileOffDisabled(t *testing.T) {
	cfg := &SandboxConfig{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "off"}}
	if cfg.Enabled() {
		t.Fatal("profile off must disable the sandbox")
	}
}

func TestValidateSandboxPolicy(t *testing.T) {
	valid := []*SandboxConfig{
		{Wrapper: "none"},
		{Wrapper: "seatbelt"},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "off"}},
		{Wrapper: "bwrap", SandboxPolicy: SandboxPolicy{Profile: "custom", Filesystem: &SandboxFilesystem{Read: "allow", Write: "allow"}}},
	}
	for _, cfg := range valid {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid sandbox rejected: %v (%+v)", err, cfg)
		}
	}

	invalid := []*SandboxConfig{
		{},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Network: &SandboxNetwork{Mode: "allowlist"}}},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{Read: "maybe"}}},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "bogus"}},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "allowlist"}}},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "inherit", Allow: []string{"/run/x.sock"}}}},
		{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Network: &SandboxNetwork{AllowedDomains: []string{"api.deepseek.com"}}}},
	}
	for _, cfg := range invalid {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid sandbox accepted: %+v", cfg)
		}
	}

	if runtime.GOOS == "linux" {
		if err := (&SandboxConfig{
			Wrapper: "bwrap",
			SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{
				DenyWrite: []string{"**/.env"},
			}},
		}).Validate(); err == nil {
			t.Fatal("a write glob must be rejected on linux")
		}
	}
}

// TestValidateSandboxDisabledPolicyFields guards against silently ignoring a
// policy when the sandbox is disabled.
func TestValidateSandboxDisabledPolicyFields(t *testing.T) {
	bad := &SandboxConfig{
		Wrapper:       "none",
		SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{Read: "off"}},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("policy fields with wrapper none must be rejected")
	}
	off := &SandboxConfig{
		Wrapper:       "seatbelt",
		SandboxPolicy: SandboxPolicy{Profile: "off", Network: &SandboxNetwork{Mode: "off"}},
	}
	if err := off.Validate(); err == nil {
		t.Fatal("policy fields with profile off must be rejected")
	}
}

// TestSandboxSocketInheritAlias checks the pre-rename "deny" value validates.
func TestSandboxSocketInheritAlias(t *testing.T) {
	cfg := &SandboxConfig{
		Wrapper:       "seatbelt",
		SandboxPolicy: SandboxPolicy{UnixSockets: &SandboxUnixSockets{Mode: "deny"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the deny alias must validate: %v", err)
	}
}

// TestValidateSandboxPlatformOff guards the platform-specific "off" checks on
// every host by passing the OS explicitly.
func TestValidateSandboxPlatformOff(t *testing.T) {
	for _, fs := range []*SandboxFilesystem{{Read: "off"}, {Write: "off"}} {
		cfg := &SandboxConfig{Wrapper: "appcontainer", SandboxPolicy: SandboxPolicy{Filesystem: fs}}
		if err := validatePlatformSandboxFor("windows", cfg); err == nil {
			t.Fatalf("windows must reject filesystem off: %+v", fs)
		}
	}
	cfg := &SandboxConfig{Wrapper: "appcontainer", SandboxPolicy: SandboxPolicy{ProcessInspection: "allow"}}
	if err := validatePlatformSandboxFor("windows", cfg); err == nil {
		t.Fatal("windows must reject processInspection: allow")
	}

	bad := &SandboxConfig{Wrapper: "bwrap", SandboxPolicy: SandboxPolicy{
		Filesystem: &SandboxFilesystem{Write: "off"},
	}}
	if err := validatePlatformSandboxFor("linux", bad); err == nil {
		t.Fatal("linux must reject write off with read allow")
	}
	ok := &SandboxConfig{Wrapper: "bwrap", SandboxPolicy: SandboxPolicy{
		Filesystem: &SandboxFilesystem{Read: "off", Write: "off"},
	}}
	if err := validatePlatformSandboxFor("linux", ok); err != nil {
		t.Fatalf("read off + write off must validate on linux: %v", err)
	}
	if err := validatePlatformSandboxFor("darwin", bad); err != nil {
		t.Fatalf("darwin must allow write off with read allow: %v", err)
	}
}

func TestSandboxViolationConfigValidation(t *testing.T) {
	if runtime.GOOS != "linux" {
		precise := &SandboxConfig{
			Wrapper:       "seatbelt",
			SandboxPolicy: SandboxPolicy{Violations: &SandboxViolations{Mode: "precise"}},
		}
		if err := precise.Validate(); err == nil {
			t.Fatalf("precise must be rejected on %s", runtime.GOOS)
		}
	}
}

func TestSandboxEffectiveAccess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cfg   SandboxConfig
		read  string
		write string
	}{
		{"standard", SandboxConfig{Wrapper: "seatbelt"}, sandboxmode.FSModeAllow, sandboxmode.FSModeAllow},
		{"custom", SandboxConfig{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Profile: "custom"}}, sandboxmode.FSModeOff, sandboxmode.FSModeOff},
		{"override", SandboxConfig{Wrapper: "seatbelt", SandboxPolicy: SandboxPolicy{Filesystem: &SandboxFilesystem{Read: "off"}}}, sandboxmode.FSModeOff, sandboxmode.FSModeAllow},
	} {
		read, write := tc.cfg.SandboxPolicy.EffectiveAccess()
		if read != tc.read || write != tc.write {
			t.Errorf("%s: access = %q/%q, want %q/%q", tc.name, read, write, tc.read, tc.write)
		}
	}
}
