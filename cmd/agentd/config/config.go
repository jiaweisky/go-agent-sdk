package config

import "errors"

// Config is the agentd configuration: a global model registry plus a list of
// servers, each exposing one model subset. The file is envsubst-expanded before
// decoding, so it may reference environment variables explicitly (`$VAR` /
// `${VAR}` / `${VAR:-default}`); none are read implicitly.
type Config struct {
	Models []ModelConfig `yaml:"models"`
	// ProviderFileAPIs are reusable provider Files API configurations referenced
	// by models[].providerFileAPI.
	ProviderFileAPIs map[string]*ProviderFileAPI `yaml:"providerFileAPIs"`
	Servers          []Server                    `yaml:"servers"`
	// Control, when set, exposes a control API that creates and destroys
	// isolated project instances at runtime.
	Control *ControlConfig `yaml:"control"`
	// Wrap, when set, re-executes agentd inside a whole-process sandbox so every
	// server and every tool shares one confinement. It is stronger than the
	// per-server sandbox.wrapper (which wraps only spawned commands: bash,
	// ripgrep, git) and covers the SDK's own file writes too. Unix only.
	Wrap *WrapConfig `yaml:"wrap"`
}

// errModelsRequired is returned when a config has no model entries. It is a
// sentinel so validation can keep the concise message without repeating the
// length check.
var errModelsRequired = errors.New("models is required")

// ModelRegistry builds the global model registry from the configured entries.
func (c Config) ModelRegistry() (ModelRegistry, error) {
	if len(c.Models) == 0 {
		return ModelRegistry{}, errModelsRequired
	}
	return NewModelRegistry(c.Models[0], c.Models[1:])
}
