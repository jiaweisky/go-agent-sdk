package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandboxpolicy"
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
	if !w.Enabled() {
		return nil
	}

	if os.Getenv(wrappedEnv) != "" {
		return nil
	}

	launcher, err := sandbox.ResolveProcess(string(w.Wrapper))
	if err != nil {
		if w.FailurePolicy == config.FailurePolicyFailOpen {
			fmt.Fprintf(os.Stderr, "agentd: whole-process sandbox %q unavailable (%v); running unwrapped\n", w.Wrapper, err)
			return nil
		}
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	roots := wrapRoots(cfg)
	policy := sandboxpolicy.ForWrap(w)
	// The wrapped process must still read its own executable, config file, DNS
	// and CA-trust files, state roots and config dirs to start and serve;
	// writes stay limited to the shared roots. Skip paths already covered by an
	// allowed directory (for example /etc), but keep a symlink's resolved
	// target when it lives outside the allowed directory (for example
	// /etc/resolv.conf -> /run/systemd/resolve/...).
	policy.AllowRead = addWrapReadPaths(policy.AllowRead, wrapReadPaths(cfg, executable, configPath, roots))
	policy.AllowWrite = append(policy.AllowWrite, roots...)

	// The sentinel marks the child as already wrapped so it does not recurse.
	if err := os.Setenv(wrappedEnv, string(w.Wrapper)); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "agentd: entering whole-process sandbox %q\n", w.Wrapper)

	return launcher(policy, executable, os.Args[1:])
}

// addWrapReadPaths appends extra read paths unless an allowed directory already
// covers them. A symlink whose target is outside the allowed directories adds
// the resolved target instead of the symlink path: binding the symlink over a
// read-only parent is fragile, while binding the target keeps the symlink
// usable.
func addWrapReadPaths(allowed []string, extra []string) []string {
	for _, path := range extra {
		if sandbox.UnderAny(path, allowed) {
			if resolved, err := filepath.EvalSymlinks(path); err == nil && !sandbox.UnderAny(resolved, allowed) {
				allowed = append(allowed, resolved)
			}
			continue
		}
		allowed = append(allowed, path)
	}

	return allowed
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

// bootstrapReadPaths returns the host files the wrapped process must reach
// through the allow-list view: the /etc DNS files for hostname resolution (the
// model API call) and the CA trust stores for TLS certificate verification. Each
// is included only when it exists on the host.
func bootstrapReadPaths() []string {
	return []string{
		"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf",
		"/etc/ssl/certs", "/etc/pki/tls/certs", "/etc/pki/ca-trust/extracted",
	}
}

// wrapConfigDirs collects the configured agent/skill directories, which the
// wrapped process must read to start.
func wrapConfigDirs(cfg config.Config) []string {
	var dirs []string
	for _, s := range cfg.Servers {
		dirs = append(dirs, s.ConfigDirs...)
	}

	return dirs
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

	for _, path := range bootstrapReadPaths() {
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}

	return append(append(paths, roots...), wrapConfigDirs(cfg)...)
}
