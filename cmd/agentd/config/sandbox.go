package config

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
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

// Enabled reports whether a wrapper and a non-off profile are configured.
func (s *SandboxConfig) Enabled() bool {
	return s != nil && s.Wrapper != "" && s.Wrapper != WrapperNone && s.Profile != "off"
}

// Policy resolves the effective sandbox policy for a working directory. A
// disabled sandbox yields a policy with only the working directory set, so the
// guards become no-ops.
func (s *SandboxConfig) Policy(workingDir string) sandbox.Policy {
	if !s.Enabled() {
		return sandbox.Policy{WorkingDirectory: workingDir}
	}
	return s.SandboxPolicy.resolve(workingDir)
}

// Policy resolves the whole-process sandbox policy. A disabled wrap yields the
// zero policy.
func (w *WrapConfig) Policy() sandbox.Policy {
	if w == nil || w.Wrapper == "" || w.Wrapper == WrapperNone || w.Profile == "off" {
		return sandbox.Policy{}
	}
	return w.SandboxPolicy.resolve("")
}

// resolve builds a policy from a profile and the explicit overrides.
func (p SandboxPolicy) resolve(workingDir string) sandbox.Policy {
	if p.Profile == "off" {
		return sandbox.Policy{WorkingDirectory: workingDir}
	}
	var policy sandbox.Policy
	switch p.Profile {
	case "", "standard":
		policy = sandbox.StandardPolicy(workingDir)
	case "custom":
		// Start from no confinement; explicit fields opt in.
		policy = sandbox.Policy{
			WorkingDirectory: workingDir,
			ReadMode:         sandbox.FSModeOff,
			WriteMode:        sandbox.FSModeOff,
			NetworkMode:      sandbox.NetworkOff,
			SocketMode:       sandbox.SocketAllowAll,
		}
	}

	if f := p.Filesystem; f != nil {
		if f.Read != "" {
			policy.ReadMode = f.Read
		}
		if f.Write != "" {
			policy.WriteMode = f.Write
		}
		policy.DenyRead = append(policy.DenyRead, f.DenyRead...)
		policy.AllowRead = append(policy.AllowRead, f.AllowRead...)
		policy.DenyWrite = append(policy.DenyWrite, f.DenyWrite...)
		policy.AllowWrite = append(policy.AllowWrite, f.AllowWrite...)
	}
	if n := p.Network; n != nil && n.Mode != "" {
		policy.NetworkMode = n.Mode
	}
	if u := p.UnixSockets; u != nil {
		switch u.Mode {
		case sandbox.SocketDeny:
			// Pre-rename alias: the real semantics are "follow the file policy".
			policy.SocketMode = sandbox.SocketInherit
		case "":
		default:
			policy.SocketMode = u.Mode
		}
		policy.SocketAllow = append(policy.SocketAllow, u.Allow...)
	}
	switch p.ProcessInspection {
	case "deny":
		policy.DenyProcessInfo = true
	case "allow":
		policy.DenyProcessInfo = false
	}
	if e := p.Env; e != nil {
		switch e.Mode {
		case sandbox.EnvScrub:
			policy.ScrubEnv = append(sandbox.DefaultScrubEnv(), e.Scrub...)
		case sandbox.EnvInherit:
			policy.ScrubEnv = nil
		}
		if e.Mode == "" && len(e.Scrub) > 0 {
			policy.ScrubEnv = append(policy.ScrubEnv, e.Scrub...)
		}
	}
	// Violation reporting defaults to heuristic and appends a note to the tool
	// output; both are explicitly overridable.
	policy.ViolationMode = sandbox.ViolationHeuristic
	policy.ReportToModel = true
	if v := p.Violations; v != nil {
		if v.Mode != "" {
			policy.ViolationMode = v.Mode
		}
		if v.ReportToModel != nil {
			policy.ReportToModel = *v.ReportToModel
		}
	}
	return policy
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
	if (s.Wrapper == WrapperNone || s.Profile == "off") && s.hasPolicyFields() {
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

func validateSandboxProfile(profile string) error {
	switch profile {
	case "", "off", "standard", "custom":
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
	case "", sandbox.FSModeAllow, sandbox.FSModeOff:
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
	case "", sandbox.NetworkOff, sandbox.NetworkNoEgress:
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
	if u.Mode != sandbox.SocketAllowlist && len(u.Allow) > 0 {
		return errors.New("sandbox.unixSockets.allow requires mode allowlist")
	}
	switch u.Mode {
	case "", sandbox.SocketInherit, sandbox.SocketDeny, sandbox.SocketAllowAll:
		return nil
	case sandbox.SocketAllowlist:
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
	case "", sandbox.EnvScrub, sandbox.EnvInherit:
		return nil
	}
	return fmt.Errorf("sandbox.env.mode must be scrub or inherit, got %q", e.Mode)
}

func validateSandboxViolations(v *SandboxViolations) error {
	if v == nil {
		return nil
	}
	switch v.Mode {
	case "", sandbox.ViolationHeuristic, sandbox.ViolationOff:
		return nil
	case sandbox.ViolationPrecise:
		if runtime.GOOS != "linux" {
			return fmt.Errorf("sandbox.violations.mode precise is only supported on linux (this host is %s)", runtime.GOOS)
		}
		return nil
	default:
		return fmt.Errorf("sandbox.violations.mode must be heuristic, precise or off, got %q", v.Mode)
	}
}

// validatePlatformSandbox rejects a policy the host cannot enforce. Windows
// AppContainer cannot disable read/write confinement or allow process
// introspection; bwrap cannot allow writes everywhere while confining reads.
func validatePlatformSandbox(s *SandboxConfig) error {
	return validatePlatformSandboxFor(runtime.GOOS, s)
}

// validatePlatformSandboxFor is validatePlatformSandbox with an explicit OS, so
// every platform's rules are testable on any host.
func validatePlatformSandboxFor(goos string, s *SandboxConfig) error {
	policy := s.Policy("")
	switch goos {
	case "darwin":
		// Seatbelt can express every supported combination: reads and writes may
		// be independently off, and process introspection may be allowed.
	case "windows":
		if policy.ReadMode == sandbox.FSModeOff {
			return errors.New("sandbox.filesystem.read: off is not supported on windows; use read: allow or wrapper: none")
		}
		if policy.WriteMode == sandbox.FSModeOff {
			return errors.New("sandbox.filesystem.write: off is not supported on windows; use write: allow or wrapper: none")
		}
		if s.ProcessInspection == "allow" {
			return errors.New("sandbox.processInspection: allow is not supported on windows; the AppContainer cannot be relaxed")
		}
	case "linux":
		if policy.WriteMode == sandbox.FSModeOff && policy.ReadMode == sandbox.FSModeAllow {
			return errors.New("sandbox.filesystem.write: off with read: allow is contradictory on linux; " +
				"set read: off or use wrapper: none")
		}
	}
	return nil
}
