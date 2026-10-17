package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
)

// wrappedEnv marks a process already inside the whole-process sandbox so the
// re-exec does not recurse.
const wrappedEnv = "AGENTD_SANDBOXED"

// wrapProcess re-executes agentd inside the configured whole-process sandbox, so
// every server and every tool shares one confinement. It is a no-op when no wrap
// is configured or the process is already wrapped; otherwise it does not return
// (it replaces the process image).
func wrapProcess(cfg config.Config) error {
	w := cfg.Wrap
	if w == nil || w.Process == "" || w.Process == "none" {
		return nil
	}
	if os.Getenv(wrappedEnv) != "" {
		return nil
	}
	launcher, err := sandbox.ResolveProcess(w.Process)
	if err != nil {
		if w.FailurePolicy == "fail-open" {
			fmt.Fprintf(os.Stderr, "agentd: whole-process sandbox %q unavailable (%v); running unwrapped\n", w.Process, err)
			return nil
		}
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	roots := wrapRoots(cfg)
	policy := w.Policy()
	// The wrapped process must still read its own executable, config dirs and
	// state roots (the bootstrap exception), and write the shared roots.
	policy.AllowRead = append(policy.AllowRead, executable)
	policy.AllowRead = append(policy.AllowRead, roots...)
	policy.AllowRead = append(policy.AllowRead, configDirs(cfg)...)
	policy.AllowWrite = append(policy.AllowWrite, roots...)
	// The sentinel marks the child as already wrapped so it does not recurse.
	os.Setenv(wrappedEnv, w.Process)
	fmt.Fprintf(os.Stderr, "agentd: entering whole-process sandbox %q\n", w.Process)
	return launcher(roots, policy, executable, os.Args[1:])
}

// configDirs collects the configured agent/skill directories, which the wrapped
// process must read to start.
func configDirs(cfg config.Config) []string {
	var dirs []string
	for _, s := range cfg.Servers {
		dirs = append(dirs, s.ConfigDirs...)
	}
	return dirs
}

// wrapRoots is the set of directories the wrapped process may write: every
// server's state root, working directory and isolation base, plus the system
// temp dir. Config dirs are read-only for the agent, so they are not included.
// Order is stable and duplicates are removed.
func wrapRoots(cfg config.Config) []string {
	var roots []string
	add := func(path string) {
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if !slices.Contains(roots, path) {
			roots = append(roots, path)
		}
	}
	add(os.TempDir())
	for _, s := range cfg.Servers {
		add(s.Root)
		add(s.WorkingDirectory)
		if s.Isolation != nil {
			add(s.Isolation.BaseDir)
		}
	}
	return roots
}
