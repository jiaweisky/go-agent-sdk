package config

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandboxmode"
)

// Wrapper selects the sandbox implementation that wraps spawned commands.
type Wrapper string

const (
	// WrapperNone disables the sandbox: the SDK runs commands directly.
	WrapperNone Wrapper = "none"
	// WrapperBwrap is the Linux bubblewrap wrapper.
	WrapperBwrap Wrapper = "bwrap"
	// WrapperSeatbelt is the macOS Seatbelt wrapper.
	WrapperSeatbelt Wrapper = "seatbelt"
	// WrapperAppContainer is the Windows AppContainer wrapper.
	WrapperAppContainer Wrapper = "appcontainer"
)

// Valid reports whether w is a known sandbox wrapper.
func (w Wrapper) Valid() bool {
	switch w {
	case WrapperNone, WrapperBwrap, WrapperSeatbelt, WrapperAppContainer:
		return true
	}
	return false
}

// FailurePolicy selects what happens when a configured sandbox cannot be built.
type FailurePolicy string

const (
	// FailurePolicyFailClosed refuses to start when the sandbox cannot be
	// built.
	FailurePolicyFailClosed FailurePolicy = "fail-closed"
	// FailurePolicyFailOpen warns and runs without the sandbox when it cannot
	// be built.
	FailurePolicyFailOpen FailurePolicy = "fail-open"
)

// Valid reports whether p is a supported failure policy.
func (p FailurePolicy) Valid() bool {
	return p == "" || p == FailurePolicyFailClosed || p == FailurePolicyFailOpen
}

// Profile selects how much of the sandbox policy is applied.
type Profile string

const (
	// ProfileDefault is the unset profile: standard when the sandbox is enabled.
	ProfileDefault Profile = ""
	// ProfileStandard applies the secure defaults.
	ProfileStandard Profile = "standard"
	// ProfileCustom starts from no confinement and applies explicit fields.
	ProfileCustom Profile = "custom"
	// ProfileOff disables the policy and the sandbox itself.
	ProfileOff Profile = "off"
)

// Valid reports whether p is a supported profile.
func (p Profile) Valid() bool {
	switch p {
	case ProfileDefault, ProfileStandard, ProfileCustom, ProfileOff:
		return true
	}
	return false
}

// Enabled reports whether a wrapper and a non-off profile are configured.
func (s *SandboxConfig) Enabled() bool {
	return s != nil && s.Wrapper != "" && s.Wrapper != WrapperNone && s.Profile != ProfileOff
}

// hasPolicyFields reports whether any policy field is set.
func (s *SandboxConfig) hasPolicyFields() bool {
	p := s.SandboxPolicy
	return p.Filesystem != nil || p.Network != nil || p.UnixSockets != nil ||
		p.ProcessInspection != "" || p.Env != nil || p.Violations != nil
}

// Validate checks a sandbox block for unsupported or contradictory settings.
// An unsupported rule is a startup error, never a silent allow-all.
func (s *SandboxConfig) Validate() error {
	if s == nil {
		return nil
	}

	if err := validateSandboxSelection(s); err != nil {
		return err
	}

	if err := validateSandboxPolicy(s.SandboxPolicy); err != nil {
		return err
	}

	return validatePlatformSandbox(s)
}

// validateSandboxSelection checks the wrapper choice and failure policy.
func validateSandboxSelection(s *SandboxConfig) error {
	if s.Wrapper == "" {
		return errors.New(`sandbox.wrapper is required (use "none" to disable it)`)
	}

	// The wrapper is a closed set; whether it can actually run on this host
	// (platform support, wrapper binary) is resolved at startup and governed by
	// failurePolicy.
	if !s.Wrapper.Valid() {
		return fmt.Errorf("sandbox.wrapper must be none, bwrap, seatbelt or appcontainer, got %q", s.Wrapper)
	}

	// A disabled sandbox ignores the policy; reject that instead of silently
	// downgrading it.
	if (s.Wrapper == WrapperNone || s.Profile == ProfileOff) && s.hasPolicyFields() {
		return errors.New("sandbox policy fields are ignored when the sandbox is disabled; remove them or name a wrapper")
	}

	return validateFailurePolicy("sandbox.failurePolicy", s.FailurePolicy)
}

// validateSandboxPolicy checks the inline policy fields.
func validateSandboxPolicy(p SandboxPolicy) error {
	if err := validateSandboxProfile(p.Profile); err != nil {
		return err
	}

	if err := validateSandboxFilesystem(p.Filesystem); err != nil {
		return err
	}

	if err := validateSandboxNetwork(p.Network); err != nil {
		return err
	}

	if err := validateSandboxUnixSockets(p.UnixSockets); err != nil {
		return err
	}

	if err := validateSandboxProcessInspection(p.ProcessInspection); err != nil {
		return err
	}

	if err := validateSandboxEnv(p.Env); err != nil {
		return err
	}

	return validateSandboxViolations(p.Violations)
}

// validateFailurePolicy checks a fail-closed/fail-open setting.
func validateFailurePolicy(field string, p FailurePolicy) error {
	if p.Valid() {
		return nil
	}
	return fmt.Errorf("%s must be fail-closed or fail-open, got %q", field, p)
}

func validateSandboxProfile(profile Profile) error {
	if profile.Valid() {
		return nil
	}
	return fmt.Errorf("sandbox.profile must be off, standard or custom, got %q", profile)
}

func validateSandboxFilesystem(f *SandboxFilesystem) error {
	if f == nil {
		return nil
	}

	if err := validateFileAccess("read", f.Read); err != nil {
		return err
	}

	if err := validateFileAccess("write", f.Write); err != nil {
		return err
	}

	if runtime.GOOS != "linux" {
		return nil
	}

	for _, entry := range append(append([]string{}, f.DenyRead...), f.DenyWrite...) {
		if strings.ContainsAny(entry, "*?[") {
			return fmt.Errorf("sandbox.filesystem deny glob %q is unsupported on linux; use a literal path", entry)
		}
	}

	return nil
}

// validateFileAccess checks one filesystem access mode.
func validateFileAccess(field, mode string) error {
	switch mode {
	case "", sandboxmode.FSModeAllow, sandboxmode.FSModeOff:
		return nil
	}

	return fmt.Errorf("sandbox.filesystem.%s must be allow or off, got %q", field, mode)
}

func validateSandboxNetwork(n *SandboxNetwork) error {
	if n == nil {
		return nil
	}

	if len(n.AllowedDomains) > 0 {
		return errors.New("sandbox.network.allowedDomains is not implemented; use no-egress or a host proxy")
	}

	switch n.Mode {
	case "", sandboxmode.NetworkOff, sandboxmode.NetworkNoEgress:
		return nil
	case "allowlist":
		return errors.New("sandbox.network.mode allowlist is not implemented; use no-egress or a host proxy")
	default:
		return fmt.Errorf("sandbox.network.mode must be off or no-egress, got %q", n.Mode)
	}
}

func validateSandboxUnixSockets(u *SandboxUnixSockets) error {
	if u == nil {
		return nil
	}

	if u.Mode != sandboxmode.SocketAllowlist && len(u.Allow) > 0 {
		return errors.New("sandbox.unixSockets.allow requires mode allowlist")
	}

	switch u.Mode {
	case "", sandboxmode.SocketInherit, sandboxmode.SocketDeny, sandboxmode.SocketAllowAll:
		return nil
	case sandboxmode.SocketAllowlist:
		if len(u.Allow) == 0 {
			return errors.New("sandbox.unixSockets.mode allowlist requires at least one allow path")
		}
		return nil
	default:
		return fmt.Errorf("sandbox.unixSockets.mode must be inherit, allowAll or allowlist, got %q", u.Mode)
	}
}

func validateSandboxProcessInspection(mode string) error {
	switch mode {
	case "", "deny", "allow":
		return nil
	}

	return fmt.Errorf("sandbox.processInspection must be deny or allow, got %q", mode)
}

func validateSandboxEnv(e *SandboxEnv) error {
	if e == nil {
		return nil
	}

	switch e.Mode {
	case "", sandboxmode.EnvScrub, sandboxmode.EnvInherit:
		return nil
	}

	return fmt.Errorf("sandbox.env.mode must be scrub or inherit, got %q", e.Mode)
}

func validateSandboxViolations(v *SandboxViolations) error {
	if v == nil {
		return nil
	}

	switch v.Mode {
	case "", sandboxmode.ViolationHeuristic, sandboxmode.ViolationOff:
		return nil
	case sandboxmode.ViolationPrecise:
		if runtime.GOOS != "linux" {
			return fmt.Errorf("sandbox.violations.mode precise is only supported on linux (this host is %s)", runtime.GOOS)
		}
		return nil
	default:
		return fmt.Errorf("sandbox.violations.mode must be heuristic, precise or off, got %q", v.Mode)
	}
}

// validatePlatformSandbox rejects a policy the host cannot enforce. Windows
// AppContainer cannot disable read/write confinement, allow process
// introspection or carve explicit deny paths out of granted roots; bwrap cannot
// allow writes everywhere while confining reads.
func validatePlatformSandbox(s *SandboxConfig) error {
	return validatePlatformSandboxFor(runtime.GOOS, s)
}

// validatePlatformSandboxFor is validatePlatformSandbox with an explicit OS, so
// every platform's rules are testable on any host.
func validatePlatformSandboxFor(goos string, s *SandboxConfig) error {
	if !s.Enabled() {
		return nil
	}

	readMode, writeMode := s.SandboxPolicy.EffectiveAccess()

	switch goos {
	case "darwin":
		// Seatbelt can express every supported combination: reads and writes may
		// be independently off, and process introspection may be allowed.
	case "windows":
		if readMode == sandboxmode.FSModeOff {
			return errors.New("sandbox.filesystem.read: off is not supported on windows; use read: allow or wrapper: none")
		}

		if writeMode == sandboxmode.FSModeOff {
			return errors.New("sandbox.filesystem.write: off is not supported on windows; use write: allow or wrapper: none")
		}

		if s.ProcessInspection == "allow" {
			return errors.New("sandbox.processInspection: allow is not supported on windows; the AppContainer cannot be relaxed")
		}

		if fs := s.Filesystem; fs != nil && (len(fs.DenyRead) > 0 || len(fs.DenyWrite) > 0) {
			return errors.New("sandbox.filesystem denyRead/denyWrite are not supported on windows; " +
				"the AppContainer only grants allow-list access")
		}
	case "linux":
		if writeMode == sandboxmode.FSModeOff && readMode == sandboxmode.FSModeAllow {
			return errors.New("sandbox.filesystem.write: off with read: allow is contradictory on linux; " +
				"set read: off or use wrapper: none")
		}
	}

	return nil
}

// EffectiveAccess returns the profile's read/write modes after explicit overrides.
func (p SandboxPolicy) EffectiveAccess() (readMode, writeMode string) {
	switch p.Profile {
	case ProfileDefault, ProfileStandard:
		readMode, writeMode = sandboxmode.FSModeAllow, sandboxmode.FSModeAllow
	case ProfileCustom:
		readMode, writeMode = sandboxmode.FSModeOff, sandboxmode.FSModeOff
	}

	if p.Filesystem != nil {
		if p.Filesystem.Read != "" {
			readMode = p.Filesystem.Read
		}

		if p.Filesystem.Write != "" {
			writeMode = p.Filesystem.Write
		}
	}

	return readMode, writeMode
}
