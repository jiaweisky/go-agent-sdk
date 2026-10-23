// Package sandbox resolves a named command-execution seam for agentd.
//
// The SDK exposes an execution seam (agent.ClientOptions.Process) that wraps
// every spawned command. This package maps a configuration name to a concrete
// core.AppProcess so a host can select a wrapper (bubblewrap, Seatbelt, …)
// without the config layer importing any platform package. No wrapper is
// registered by default; "none" (or empty) selects the SDK's pass-through.
package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// Factory builds a process seam for one sandbox and policy.
type Factory func(Policy) core.AppProcess

// registry maps a sandbox name to its per-command factory. It is written by
// init-time registrations and only read after startup.
var registry = map[string]Factory{}

// probes decide whether a registered sandbox can actually launch (the wrapper
// binary exists on the host). A name without a probe is always available.
var probes = map[string]func() error{}

// validateName rejects registration names a sandbox may not use, so a
// programming error surfaces at startup.
func validateName(name string) {
	if name == "" || name == "none" {
		panic(fmt.Sprintf("sandbox: %q is a reserved name", name))
	}
}

// RegisterAvailable sets the availability probe for a named sandbox; both
// Resolve and ResolveProcess run it so a missing wrapper binary fails at
// startup (fail-closed refuses, fail-open warns) instead of failing per
// command. It is intended for the wrapper's own init function.
func RegisterAvailable(name string, probe func() error) {
	validateName(name)
	if probe == nil {
		panic("sandbox: nil availability probe for " + name)
	}
	if _, dup := probes[name]; dup {
		panic("sandbox: duplicate availability registration: " + name)
	}
	probes[name] = probe
}

// Register adds a named sandbox. It is intended to be called from init
// functions of build-tagged wrapper packages. It panics on a nil factory, a
// duplicate name or a reserved name ("", "none"), which are programming errors
// surfaced at startup.
func Register(name string, factory Factory) {
	validateName(name)
	if factory == nil {
		panic("sandbox: nil factory for " + name)
	}
	if _, dup := registry[name]; dup {
		panic("sandbox: duplicate registration: " + name)
	}
	registry[name] = factory
}

// checkAvailable runs the name's availability probe, so a missing wrapper
// binary surfaces at startup.
func checkAvailable(name string) error {
	if probe := probes[name]; probe != nil {
		if err := probe(); err != nil {
			return fmt.Errorf("sandbox %q is unavailable: %w", name, err)
		}
	}
	return nil
}

// validateViolationMode rejects an unknown violation-reporting mode and checks
// that precise mode can run on this host.
func validateViolationMode(mode string) error {
	switch mode {
	case "", ViolationHeuristic, ViolationOff:
		return nil
	case ViolationPrecise:
		if runtime.GOOS != "linux" {
			return fmt.Errorf("sandbox.violations.mode precise is only supported on linux")
		}
		if _, err := exec.LookPath("strace"); err != nil {
			return fmt.Errorf("sandbox.violations.mode precise requires strace: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("sandbox.violations.mode must be heuristic, precise or off, got %q", mode)
	}
}

// Resolve returns the process seam for a name and policy. An empty name or
// "none" returns nil, meaning the SDK's default pass-through. Unknown names are
// an error so startup fails fast rather than silently running unsandboxed. When
// the policy scrubs environment variables, the returned process scrubs them from
// every command, whichever wrapper produced it.
func Resolve(name string, policy Policy) (core.AppProcess, error) {
	switch name {
	case "", "none":
		return nil, nil
	}
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("sandbox wrapper %q is not available on %s", name, runtime.GOOS)
	}
	if err := checkAvailable(name); err != nil {
		return nil, err
	}
	if err := validateViolationMode(policy.ViolationMode); err != nil {
		return nil, err
	}

	process := factory(policy)
	if len(policy.ScrubEnv) > 0 {
		process = scrubbedProcess{inner: process, patterns: policy.ScrubEnv}
	}
	if policy.ViolationMode != "" && policy.ViolationMode != ViolationOff {
		process = reportingProcess{
			inner: process, precise: policy.ViolationMode == ViolationPrecise,
			reportToModel: policy.ReportToModel,
		}
	}
	return process, nil
}
