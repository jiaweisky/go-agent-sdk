package sandbox

import "fmt"

// ProcessLauncher takes over the current process, launching command (with args)
// under the sandbox with write access to roots. It does not return on success.
// Unlike the per-command launcher, it must not cut the network, because the
// process serves listeners and calls the model.
type ProcessLauncher func(roots []string, policy Policy, command string, args []string) error

var processRegistry = map[string]ProcessLauncher{}

// RegisterProcess adds a named whole-process sandbox. Like Register, it is
// called from build-tagged init functions and panics on a duplicate or reserved
// name, which are programming errors surfaced at startup.
func RegisterProcess(name string, launcher ProcessLauncher) {
	if name == "" || name == "none" {
		panic("sandbox: " + name + " is a reserved name")
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
		return nil, fmt.Errorf("unknown wrap.process %q", name)
	}
	if err := checkAvailable(name); err != nil {
		return nil, err
	}
	return launcher, nil
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
