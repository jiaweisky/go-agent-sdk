package config

// WrapConfig selects a whole-process sandbox for the agentd process.
type WrapConfig struct {
	// Process names the whole-process sandbox: none (default) or a wrapper
	// registered for whole-process use (bwrap on Linux, seatbelt on macOS).
	Process string `yaml:"process"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// fail-closed (default) refuses to start; fail-open warns and runs unwrapped.
	FailurePolicy string `yaml:"failurePolicy"`
	// The policy block (profile, filesystem, …) is inlined. The whole-process
	// wrapper also allows the configured roots and config dirs.
	SandboxPolicy `yaml:",inline"`
}
