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
// (it replaces the process image). configPath is the -config file the re-exec'd
// child reads again after the sandbox is up.
func wrapProcess(cfg config.Config, configPath string) error {
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
	// The wrapped process must still read its own executable, config file, DNS
	// and CA-trust files, state roots and config dirs to start and serve;
	// writes stay limited to the shared roots.
	policy.AllowRead = append(policy.AllowRead, wrapReadPaths(cfg, executable, configPath, roots)...)
	policy.AllowWrite = append(policy.AllowWrite, roots...)
	// The sentinel marks the child as already wrapped so it does not recurse.
	os.Setenv(wrappedEnv, w.Process)
	fmt.Fprintf(os.Stderr, "agentd: entering whole-process sandbox %q\n", w.Process)
	return launcher(roots, policy, executable, os.Args[1:])
}

// bootstrapReadPaths are host files the wrapped process must reach through the
// allow-list view: the /etc DNS files for hostname resolution (the model API
// call) and the CA trust stores for TLS certificate verification. Each is
// included only when it exists on the host.
var bootstrapReadPaths = []string{
	"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf",
	"/etc/ssl/certs", "/etc/pki/tls/certs", "/etc/pki/ca-trust/extracted",
}

// wrapReadPaths are the paths the wrapped process must be able to read: its own
// executable, the config file, the DNS and CA-trust bootstrap files, every
// server's state root and working directory, and the config dirs. The config
// path is normalized to an absolute one, because the re-exec'd child resolves a
// relative -config against a working directory the sandbox view does not carry.
func wrapReadPaths(cfg config.Config, executable, configPath string, roots []string) []string {
	paths := []string{executable}
	if configPath != "" {
		if abs, err := filepath.Abs(configPath); err == nil {
			configPath = abs
		}
		paths = append(paths, configPath)
	}
	for _, path := range bootstrapReadPaths {
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return append(append(paths, roots...), configDirs(cfg)...)
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
