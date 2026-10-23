package sandbox

import (
	"fmt"
	"runtime"
)

// ProcessLauncher takes over the current process, launching command (with args)
// under the sandbox described by policy, which carries the writable roots and
// the read allow-list. It does not return on success. Unlike the per-command
// launcher, it must not cut the network, because the process serves listeners
// and calls the model.
type ProcessLauncher func(policy Policy, command string, args []string) error

// processRegistry maps a sandbox name to its whole-process launcher. It is
// written by init-time registrations and only read after startup.
var processRegistry = map[string]ProcessLauncher{}

// RegisterProcess adds a named whole-process sandbox. Like Register, it is
// called from build-tagged init functions and panics on a nil launcher, a
// duplicate or a reserved name, which are programming errors surfaced at
// startup.
func RegisterProcess(name string, launcher ProcessLauncher) {
	validateName(name)
	if launcher == nil {
		panic("sandbox: nil process launcher for " + name)
	}
	if _, dup := processRegistry[name]; dup {
		panic("sandbox: duplicate process registration: " + name)
	}
	processRegistry[name] = launcher
}

// ResolveProcess returns the whole-process launcher for a name. An empty name or
// "none" returns nil, meaning no wrapping. Unknown names are an error so startup
// fails fast rather than silently running unwrapped.
func ResolveProcess(name string) (ProcessLauncher, error) {
	switch name {
	case "", "none":
		return nil, nil
	}
	launcher, ok := processRegistry[name]
	if !ok {
		return nil, fmt.Errorf("wrap.wrapper %q is not available on %s", name, runtime.GOOS)
	}
	if err := checkAvailable(name); err != nil {
		return nil, err
	}
	return launcher, nil
}
