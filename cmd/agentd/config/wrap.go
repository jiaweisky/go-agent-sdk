package config

import (
	"errors"
	"fmt"
)

// WrapConfig selects a whole-process sandbox for the agentd process.
type WrapConfig struct {
	// Wrapper selects the whole-process sandbox implementation. WrapperNone
	// disables it; omit the whole block to leave the process unwrapped.
	Wrapper Wrapper `yaml:"wrapper"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// FailurePolicyFailClosed (default) refuses to start; FailurePolicyFailOpen
	// warns and runs unwrapped.
	FailurePolicy FailurePolicy `yaml:"failurePolicy"`
	// The policy block (profile, filesystem, …) is inlined. The whole-process
	// wrapper also allows the configured roots and config dirs.
	SandboxPolicy `yaml:",inline"`
}

// Enabled reports whether a whole-process wrapper and a non-off profile are
// configured.
func (w *WrapConfig) Enabled() bool {
	return w != nil && w.Wrapper != "" && w.Wrapper != WrapperNone && w.Profile != ProfileOff
}

// Validate checks the whole-process sandbox block. Nil is valid (wrap disabled).
func (w *WrapConfig) Validate() error {
	if w == nil {
		return nil
	}

	if w.Wrapper == "" {
		return errors.New(`wrap.wrapper is required (use "none" to disable)`)
	}

	if !w.Wrapper.Valid() {
		return fmt.Errorf("wrap.wrapper must be none, bwrap, seatbelt or appcontainer, got %q", w.Wrapper)
	}

	if err := validateFailurePolicy("wrap.failurePolicy", w.FailurePolicy); err != nil {
		return err
	}

	cfg := &SandboxConfig{
		Wrapper:       w.Wrapper,
		FailurePolicy: w.FailurePolicy,
		SandboxPolicy: w.SandboxPolicy,
	}

	return cfg.Validate()
}
