package core

import (
	"path/filepath"
)

// Global is the directory layout for SDK-managed state.
//
// TS's global.ts is a CLI module: it reads XDG_* (and OPENCODE_CONFIG_DIR) at
// load time. An embeddable SDK must not do that — its state has to follow an
// explicit root so a host can isolate instances and so a test never touches the
// developer's real data. The layout is therefore derived from a root the caller
// supplies; nothing here reads the environment.
//
// Consumers create the paths they use.
type Global struct {
	Home  string
	Data  string
	Cache string
	State string
	Tmp   string
	Bin   string
	Log   string
	Repos string
}

// DefaultGlobalRoot is the state root used when a caller supplies none.
func DefaultGlobalRoot(home string) string {
	return filepath.Join(home, ".local", "share", "opencode")
}

// NewGlobal derives the layout from an explicit state root and home directory.
// An empty root falls back to DefaultGlobalRoot(home).
//
//	data/   snapshots, references, plans, tool output, logs, session database
//	cache/  downloaded binaries and skill content (disposable)
//	state/  runtime state (reserved; TS uses it for flock)
//	tmp/    scratch space
func NewGlobal(root, home string) Global {
	if root == "" {
		root = DefaultGlobalRoot(home)
	}
	data := filepath.Join(root, "data")
	cache := filepath.Join(root, "cache")
	return Global{
		Home:  home,
		Data:  data,
		Cache: cache,
		State: filepath.Join(root, "state"),
		Tmp:   filepath.Join(root, "tmp"),
		Bin:   filepath.Join(cache, "bin"),
		Log:   filepath.Join(data, "log"),
		Repos: filepath.Join(data, "repos"),
	}
}
