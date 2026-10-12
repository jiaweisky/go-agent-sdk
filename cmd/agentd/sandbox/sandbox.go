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

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// Factory builds a process seam for one sandbox.
type Factory func() core.AppProcess

var registry = map[string]Factory{}

// Register adds a named sandbox. It is intended to be called from init
// functions of build-tagged wrapper packages. It panics on a duplicate name or a
// reserved name ("", "none"), which are programming errors surfaced at startup.
func Register(name string, factory Factory) {
	if name == "" || name == "none" {
		panic("sandbox: " + name + " is a reserved name")
	}
	if _, dup := registry[name]; dup {
		panic("sandbox: duplicate registration: " + name)
	}
	registry[name] = factory
}

// Resolve returns the process seam for a name. An empty name or "none" returns
// nil, meaning the SDK's default pass-through. Unknown names are an error so
// startup fails fast rather than silently running unsandboxed.
func Resolve(name string) (core.AppProcess, error) {
	switch name {
	case "", "none":
		return nil, nil
	}
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown sandbox.process %q", name)
	}
	return factory(), nil
}
