package core

import (
	"os"
	"path/filepath"
)

// Global mirrors core/global.ts. The module-level directory creation performed
// by the TS module is intentionally not reproduced here; consumers create the
// paths they use.
type Global struct {
	Home   string
	Data   string
	Cache  string
	Config string
	State  string
	Tmp    string
	Bin    string
	Log    string
	Repos  string
}

func xdgDir(env, fallback string) string {
	if value := os.Getenv(env); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fallback
	}
	return filepath.Join(home, fallback)
}

// NewGlobal mirrors Global.make, including the OPENCODE_CONFIG_DIR override.
func NewGlobal(overrides ...func(*Global)) Global {
	home := os.Getenv("OPENCODE_TEST_HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	data := filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "opencode")
	cache := filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), "opencode")
	config := filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "opencode")
	state := filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "opencode")
	tmp := filepath.Join(os.TempDir(), "opencode")

	global := Global{
		Home:   home,
		Data:   data,
		Bin:    filepath.Join(cache, "bin"),
		Log:    filepath.Join(data, "log"),
		Repos:  filepath.Join(data, "repos"),
		Cache:  cache,
		Config: config,
		State:  state,
		Tmp:    tmp,
	}
	if flag := os.Getenv("OPENCODE_CONFIG_DIR"); flag != "" {
		global.Config = flag
	}
	for _, override := range overrides {
		override(&global)
	}
	return global
}

// WithDataRoot overrides Global.Data and the paths derived from it.
func WithDataRoot(root string) func(*Global) {
	return func(global *Global) {
		global.Data = root
		global.Log = filepath.Join(root, "log")
		global.Repos = filepath.Join(root, "repos")
	}
}
