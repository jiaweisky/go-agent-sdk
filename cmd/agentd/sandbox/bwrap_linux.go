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
func bwrapProcess(policy Policy, command string, args []string) error {
	return execReplace("bwrap", bwrapArgs(nil, policy, false, command, args))
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
	// Emit read denies last: a later write bind must not re-expose them.
	argv = append(argv, bwrapReadDenyArgs(policy)...)

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

// bwrapDenyCandidates expands deny entries to the literal and resolved
// spellings the sandbox view may expose. Globs are skipped because Linux
// validation rejects them, and symlink spellings are skipped because bwrap
// cannot mount over a symlink destination.
func bwrapDenyCandidates(paths []string) []string {
	var literal []string
	for _, path := range paths {
		if strings.ContainsAny(path, "*?[") {
			continue
		}
		literal = append(literal, path)
	}
	var out []string
	for _, path := range bwrapBinds(literal) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		out = append(out, path)
	}
	return out
}

// bwrapReadDenyArgs masks explicitly denied read paths. The caller emits these
// after the write binds so a deny inside a writable root is not re-exposed.
func bwrapReadDenyArgs(policy Policy) []string {
	if policy.ReadMode != FSModeAllow {
		// read: off means no read confinement; no credential masks either.
		return nil
	}
	var argv []string
	for _, path := range bwrapDenyCandidates(policy.DeniedRead()) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			argv = append(argv, "--tmpfs", path)
			continue
		}
		argv = append(argv, "--ro-bind", "/dev/null", path)
	}
	return argv
}

// bwrapReadArgs renders the read-only part of the view: an allow-list root and
// the runtime mounts, or the whole filesystem when read confinement is off.
func bwrapReadArgs(dirs []string, policy Policy) []string {
	if policy.ReadMode != FSModeAllow {
		return []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp"}
	}
	// A fresh root: only the allow-listed paths exist beyond /dev /proc /tmp.
	argv := []string{"--tmpfs", "/"}
	for _, root := range bwrapBinds(bwrapReadRoots(dirs, policy)) {
		argv = append(argv, "--ro-bind", root, root)
	}

	argv = append(argv, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
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

// bwrapWriteArgs renders the writable part of the view: the write roots bound
// read-write, then explicit denies masked read-only (or with a private tmpfs
// when they do not exist yet).
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
	binds := bwrapBinds(paths, sockets)
	var argv []string
	for _, root := range binds {
		argv = append(argv, "--bind", root, root)
	}
	return append(argv, bwrapDenyWriteArgs(binds, policy)...)
}

// bwrapDenyWriteArgs masks denied paths after the writable roots are bound, so
// a deny inside an allowed root wins. An existing path is rebound read-only,
// preserving reads; a missing one gets a private tmpfs so writes stay off the
// host. bwrap must create that mount point itself, so the missing path may
// appear as an empty directory on the host.
func bwrapDenyWriteArgs(bound []string, policy Policy) []string {
	var argv []string
	for _, path := range bwrapDenyCandidates(policy.DeniedWrite()) {
		if !bwrapUnderAny(path, bound) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			argv = append(argv, "--ro-bind", path, path)
			continue
		}
		argv = append(argv, "--tmpfs", path)
	}
	return argv
}

// bwrapUnderAny reports whether path lies inside one of roots.
func bwrapUnderAny(path string, roots []string) bool {
	for _, root := range roots {
		if under(path, root) {
			return true
		}
	}
	return false
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
