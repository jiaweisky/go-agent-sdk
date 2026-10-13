package sandbox

import "fmt"

// ProcessLauncher rewrites the whole-process invocation into its sandboxed
// equivalent. Unlike the per-command launcher, it takes every directory the
// wrapped process may write (its state roots, working directories and config
// dirs), and it must not cut the network, because the process serves listeners
// and calls the model. command is the executable to wrap; the returned argv
// excludes argv[0].
type ProcessLauncher func(roots []string, command string, args []string) (string, []string)

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
	return launcher, nil
}
