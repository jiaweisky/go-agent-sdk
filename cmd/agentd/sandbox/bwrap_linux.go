//go:build linux

package sandbox

import (
	"os"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("bwrap", func(policy Policy) core.AppProcess {
		return newSandboxed(bwrapLaunch, policy)
	})
	RegisterProcess("bwrap", bwrapProcess)
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
	for _, root := range dedupe(append(append([]string{}, dirs...), policy.ReadRoots()...)) {
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
	var argv []string
	for _, root := range dedupe(append(append([]string{}, dirs...), policy.WriteRoots()...)) {
		argv = append(argv, "--bind", root, root)
	}
	for _, socket := range policy.SocketPaths() {
		argv = append(argv, "--bind", socket, socket)
	}
	return argv
}
