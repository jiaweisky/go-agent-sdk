package config

import "errors"

// Config is the agentd configuration: a global model catalog plus a list of
// servers, each exposing one model subset. The file is envsubst-expanded before
// decoding, so it may reference environment variables explicitly (`$VAR` /
// `${VAR}` / `${VAR:-default}`); none are read implicitly.
type Config struct {
	Models  []ModelConfig `yaml:"models"`
	Servers []Server      `yaml:"servers"`
	// Control, when set, exposes a control API that creates and destroys
	// isolated project instances at runtime.
	Control *ControlConfig `yaml:"control"`
	// Wrap, when set, re-executes agentd inside a whole-process sandbox so every
	// server and every tool shares one confinement. It is stronger than the
	// per-server sandbox.wrapper (which wraps only spawned commands: bash,
	// ripgrep, git) and covers the SDK's own file writes too. Unix only.
	Wrap *WrapConfig `yaml:"wrap"`
}

// Catalog builds the global model catalog from the configured entries.
func (c Config) Catalog() (Catalog, error) {
	if len(c.Models) == 0 {
		return Catalog{}, errors.New("models is required")
	}
	return NewCatalog(c.Models[0], c.Models[1:])
}
