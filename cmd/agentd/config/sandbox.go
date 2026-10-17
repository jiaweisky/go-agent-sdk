package config

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
)

// Enabled reports whether a wrapper and a non-off profile are configured.
func (s *SandboxConfig) Enabled() bool {
	return s != nil && s.Process != "" && s.Process != "none" && s.Profile != "off"
}

// Policy resolves the effective sandbox policy for a working directory. A
// disabled sandbox yields the zero policy (the guards become no-ops).
func (s *SandboxConfig) Policy(workingDir string) sandbox.Policy {
	if !s.Enabled() {
		return sandbox.Policy{WorkingDirectory: workingDir}
	}
	return s.SandboxPolicy.resolve(workingDir, true)
}

// Policy resolves the whole-process sandbox policy. A disabled wrap yields the
// zero policy.
func (w *WrapConfig) Policy() sandbox.Policy {
	if w == nil || w.Process == "" || w.Process == "none" || w.Profile == "off" {
		return sandbox.Policy{}
	}
	return w.SandboxPolicy.resolve("", true)
}

// resolve builds a policy from a profile and the explicit overrides.
func (p SandboxPolicy) resolve(workingDir string, enabled bool) sandbox.Policy {
	if !enabled || p.Profile == "off" {
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
		case "deny":
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
		case "scrub":
			policy.ScrubEnv = append(sandbox.DefaultScrubEnv(), e.Scrub...)
		case "inherit":
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

// ValidateSandbox checks a sandbox block for unsupported or contradictory
// settings. An unsupported rule is a startup error, never a silent allow-all.
func ValidateSandbox(s *SandboxConfig) error {
	if s == nil {
		return nil
	}
	// An unknown process name is resolved at startup, so fail-open can downgrade
	// it with a warning; only an empty name (with a sandbox block present) is a
	// configuration error.
	if s.Process == "" {
		return errors.New(`sandbox.process is required (use "none" to disable it)`)
	}
	// A disabled sandbox ignores the policy; reject that instead of silently
	// downgrading it.
	if (s.Process == "none" || s.Profile == "off") && s.hasPolicyFields() {
		return errors.New("sandbox policy fields are ignored when the sandbox is disabled; remove them or name a process")
	}
	switch s.FailurePolicy {
	case "", "fail-closed", "fail-open":
	default:
		return fmt.Errorf("sandbox.failurePolicy must be fail-closed or fail-open, got %q", s.FailurePolicy)
	}
	switch s.Profile {
	case "", "off", "standard", "custom":
	default:
		return fmt.Errorf("sandbox.profile must be off, standard or custom, got %q", s.Profile)
	}
	if f := s.Filesystem; f != nil {
		for name, mode := range map[string]string{"read": f.Read, "write": f.Write} {
			switch mode {
			case "", "allow", "off":
			default:
				return fmt.Errorf("sandbox.filesystem.%s must be allow or off, got %q", name, mode)
			}
		}
		if runtime.GOOS == "linux" {
			for _, entry := range append(append([]string{}, f.DenyRead...), f.DenyWrite...) {
				if strings.ContainsAny(entry, "*?[") {
					return fmt.Errorf("sandbox.filesystem deny glob %q is unsupported on linux; use a literal path", entry)
				}
			}
		}
	}
	if n := s.Network; n != nil {
		if len(n.AllowedDomains) > 0 {
			return errors.New("sandbox.network.allowedDomains is not implemented; use no-egress or a host proxy")
		}
		switch n.Mode {
		case "", "off", "no-egress":
		case "allowlist":
			return errors.New("sandbox.network.mode allowlist is not implemented; use no-egress or a host proxy")
		default:
			return fmt.Errorf("sandbox.network.mode must be off or no-egress, got %q", n.Mode)
		}
	}
	if u := s.UnixSockets; u != nil {
		if u.Mode != "allowlist" && len(u.Allow) > 0 {
			return errors.New("sandbox.unixSockets.allow requires mode allowlist")
		}
		switch u.Mode {
		case "", "inherit", "deny", "allowAll":
		case "allowlist":
			if len(u.Allow) == 0 {
				return errors.New("sandbox.unixSockets.mode allowlist requires at least one allow path")
			}
		default:
			return fmt.Errorf("sandbox.unixSockets.mode must be inherit, allowAll or allowlist, got %q", u.Mode)
		}
	}
	switch s.ProcessInspection {
	case "", "deny", "allow":
	default:
		return fmt.Errorf("sandbox.processInspection must be deny or allow, got %q", s.ProcessInspection)
	}
	if e := s.Env; e != nil {
		switch e.Mode {
		case "", "scrub", "inherit":
		default:
			return fmt.Errorf("sandbox.env.mode must be scrub or inherit, got %q", e.Mode)
		}
	}
	if v := s.Violations; v != nil {
		switch v.Mode {
		case "", "heuristic", "off":
		case "precise":
			if runtime.GOOS != "linux" {
				return fmt.Errorf("sandbox.violations.mode precise is only supported on linux (this host is %s)", runtime.GOOS)
			}
		default:
			return fmt.Errorf("sandbox.violations.mode must be heuristic, precise or off, got %q", v.Mode)
		}
	}
	return validatePlatformSandbox(s)
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
	case "windows":
		if policy.ReadMode == sandbox.FSModeOff {
			return errors.New("sandbox.filesystem.read: off is not supported on windows; use read: allow or process: none")
		}
		if policy.WriteMode == sandbox.FSModeOff {
			return errors.New("sandbox.filesystem.write: off is not supported on windows; use write: allow or process: none")
		}
		if s.ProcessInspection == "allow" {
			return errors.New("sandbox.processInspection: allow is not supported on windows; the AppContainer cannot be relaxed")
		}
	case "linux":
		if policy.WriteMode == sandbox.FSModeOff && policy.ReadMode == sandbox.FSModeAllow {
			return errors.New("sandbox.filesystem.write: off with read: allow is contradictory on linux; set read: off or use process: none")
		}
	}
	return nil
}
