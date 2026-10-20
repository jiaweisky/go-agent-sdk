//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("bwrap", func(policy Policy) core.AppProcess {
		return newSandboxed(bwrapLaunch, policy)
	})
	RegisterProcess("bwrap", bwrapProcess)
	RegisterAvailable("bwrap", func() error {
		_, err := exec.LookPath("bwrap")
		return err
	})
}

// bwrapProcess wraps the whole process in bubblewrap under the policy. Unlike the
// per-command wrapper it does not unshare the network or the PID namespace,
// because the process serves listeners, calls the model and manages its children.
func bwrapProcess(roots []string, policy Policy, command string, args []string) error {
	return execReplace("bwrap", bwrapArgs(roots, policy, false, command, args))
}

// bwrapLaunch wraps a command in bubblewrap under the policy: reads are bound
// read-only from the allow-list (an allow-only view), writes are bound from the
// write allow-list, and /tmp is a private tmpfs.
func bwrapLaunch(dir string, policy Policy, command string, args []string) (string, []string) {
	return "bwrap", bwrapArgs([]string{dir}, policy, true, command, args)
}

// bwrapArgs renders the bubblewrap argument vector (excluding the program name).
func bwrapArgs(dirs []string, policy Policy, perCommand bool, command string, args []string) []string {
	argv := bwrapReadArgs(dirs, policy)
	argv = append(argv, bwrapWriteArgs(dirs, policy)...)
	argv = append(argv, "--die-with-parent")
	if perCommand && policy.NetworkMode == NetworkNoEgress {
		argv = append(argv, "--unshare-net")
	}
	if perCommand && policy.DenyProcessInfo {
		argv = append(argv, "--unshare-pid")
	}
	argv = append(argv, "--")
	if perCommand && policy.ViolationMode == ViolationPrecise {
		// Trace the sandboxed command's failed syscalls for precise denials.
		argv = append(argv, "strace", "-f", "-qq", "-e", "trace=openat,connect", "--")
	}
	argv = append(argv, command)
	return append(argv, args...)
}

func bwrapReadArgs(dirs []string, policy Policy) []string {
	if policy.ReadMode != FSModeAllow {
		// read: off means no read confinement; no credential masks either.
		return []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp"}
	}
	// A fresh root: only the allow-listed paths exist beyond /dev /proc /tmp.
	argv := []string{"--tmpfs", "/"}
	for _, root := range bwrapBinds(bwrapReadRoots(dirs, policy)) {
		argv = append(argv, "--ro-bind", root, root)
	}
	argv = append(argv, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
	for _, path := range policy.DeniedRead() {
		if strings.ContainsAny(path, "*?[") {
			continue // validated as unsupported on Linux
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			argv = append(argv, "--tmpfs", path)
		}
	}
	return argv
}

// bwrapReadRoots are the read bind candidates: the dirs, the policy's working
// directory and temp, the explicitly allowed read paths, the legacy temp
// compatibility root when there is no per-run temp, and the platform's run-time
// paths. Entries keep their literal spelling — bwrapBinds adds the resolved
// forms.
func bwrapReadRoots(dirs []string, policy Policy) []string {
	paths := append(bwrapRootPaths(dirs, policy), policy.AllowRead...)
	if policy.PrivateTemp == "" {
		// The shared temp keeps the legacy /tmp compatibility root. With a
		// per-run temp only that directory is exposed.
		paths = append(paths, legacyTempReadRoots()...)
	}
	return append(paths, platformReadRoots()...)
}

// bwrapRootPaths merges the dirs with the policy's working directory and temp.
func bwrapRootPaths(dirs []string, policy Policy) []string {
	return append(append([]string{}, dirs...), policy.WorkingDirectory, policy.tempPath())
}

func bwrapWriteArgs(dirs []string, policy Policy) []string {
	if policy.WriteMode != FSModeAllow {
		// write: off is only meaningful together with read: off; the config
		// layer rejects read: allow + write: off. The rw root overrides the
		// read-only view with a fully writable one.
		if policy.WriteMode == FSModeOff && policy.ReadMode != FSModeAllow {
			return []string{"--bind", "/", "/"}
		}
		return nil
	}
	paths := append(bwrapRootPaths(dirs, policy), policy.AllowWrite...)
	// The sockets are bound in their canonical form and as configured: the
	// fresh root lacks the compatibility symlinks (/var/run → /run), so a
	// command only reaches the socket through a spelling that is bound.
	sockets := policy.SocketPaths()
	if policy.SocketMode == SocketAllowlist {
		sockets = append(sockets, policy.SocketAllow...)
	}
	var argv []string
	for _, root := range bwrapBinds(paths, sockets) {
		argv = append(argv, "--bind", root, root)
	}
	return argv
}

// bwrapBinds expands the bind list: each path is bound at its canonical
// (symlink-resolved) spelling and at its literal one. The canonical form alone
// loses the usrmerge aliases (/bin → usr/bin, /var/run → /run): the fresh
// tmpfs root contains only what is bound and commands reference the literal
// paths, so every spelling must be bound in turn. A configured path that does
// not exist is kept, so bwrap itself refuses to launch — the same fail-closed
// outcome as any other missing root.
func bwrapBinds(pathGroups ...[]string) []string {
	var out []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if !contains(out, path) {
			out = append(out, path)
		}
	}
	for _, group := range pathGroups {
		for _, path := range group {
			if path == "" {
				continue // an empty path would Clean/EvalSymlinks to "."
			}
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				add(resolved)
			}
			add(path)
		}
	}
	return out
}
