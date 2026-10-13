package config

import "errors"

// Config is the agentd configuration: a global model catalog plus a list of
// servers, each exposing one model subset. String values may reference
// environment variables explicitly (`${VAR}` / `${VAR:-default}`); none are read
// implicitly.
type Config struct {
	Models  []ModelConfig `yaml:"models"`
	Servers []Server      `yaml:"servers"`
	// Control, when set, exposes an admin API that creates and destroys isolated
	// project instances at runtime.
	Control *ControlConfig `yaml:"control"`
	// Wrap, when set, re-executes agentd inside a whole-process sandbox so every
	// server and every tool shares one confinement. It is stronger than the
	// per-server sandbox.process (which wraps only spawned commands: bash,
	// ripgrep, git) and covers the SDK's own file writes too. Unix only.
	Wrap *WrapConfig `yaml:"wrap"`
}

// WrapConfig selects a whole-process sandbox for the agentd process.
type WrapConfig struct {
	// Process names the whole-process sandbox: none (default) or a wrapper
	// registered for whole-process use (bwrap on Linux, seatbelt on macOS).
	Process string `yaml:"process"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// fail-closed (default) refuses to start; fail-open warns and runs unwrapped.
	FailurePolicy string `yaml:"failurePolicy"`
}

// ControlConfig configures the optional project admin API.
type ControlConfig struct {
	// Listen is the admin API bind address (required to enable it).
	Listen string `yaml:"listen"`
	// ProjectListen is the host:port dynamic project instances bind to; the
	// default (127.0.0.1:0) picks a free loopback port per instance.
	ProjectListen string `yaml:"projectListen"`
	// MaxLeases caps the number of live project instances; 0 (the default) is
	// unlimited. It bounds a control-API caller from exhausting resources.
	MaxLeases int `yaml:"maxLeases"`
	// Auth, when set, guards the admin API.
	Auth *AuthConfig `yaml:"auth"`
}

// Catalog builds the global model catalog from the configured entries.
func (c Config) Catalog() (Catalog, error) {
	if len(c.Models) == 0 {
		return Catalog{}, errors.New("models is required")
	}
	return NewCatalog(c.Models[0], c.Models[1:])
}
