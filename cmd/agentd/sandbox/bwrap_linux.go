//go:build linux

package sandbox

import "github.com/jiaweisky/go-agent-sdk/pkg/core"

func init() {
	Register("bwrap", func() core.AppProcess {
		return sandboxed{inner: core.NewAppProcess(), launch: bwrapLaunch}
	})
	RegisterProcess("bwrap", bwrapProcess)
}

// bwrapProcess wraps the whole process in bubblewrap: the filesystem is
// read-only, every writable root is bound read-write, /tmp is a private tmpfs,
// and the sandbox dies with its parent. Unlike the per-command wrapper it does
// **not** unshare the network, because the process serves listeners and calls
// the model. Best-effort: requires `bwrap` and user namespaces.
func bwrapProcess(roots []string, command string, args []string) error {
	return execReplace("bwrap", bwrapProcessArgs(roots, command, args))
}

// bwrapProcessArgs is the bwrap argument vector (excluding the program name).
func bwrapProcessArgs(roots []string, command string, args []string) []string {
	argv := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--die-with-parent",
	}
	for _, root := range roots {
		argv = append(argv, "--bind", root, root)
	}
	argv = append(argv, "--", command)
	return append(argv, args...)
}

// bwrapLaunch wraps a command in bubblewrap (`bwrap`): the whole filesystem is
// mounted read-only, the working directory is writable, /tmp is a private tmpfs,
// the network is unshared, the sandbox dies with its parent, and the user's
// sensitive credential directories (see homeReadMasks) are masked with empty
// tmpfs mounts so they cannot be read. Best-effort: the SDK ships no guarantee
// for it; it requires `bwrap` and a kernel with user namespaces enabled.
func bwrapLaunch(dir, command string, args []string) (string, []string) {
	return bwrapLaunchWith(dir, homeReadMasks(), command, args)
}

// bwrapLaunchWith is bwrapLaunch with an explicit mask list, for testing.
func bwrapLaunchWith(dir string, masks []string, command string, args []string) (string, []string) {
	argv := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--unshare-net",
		"--die-with-parent",
	}
	for _, mask := range masks {
		argv = append(argv, "--tmpfs", mask)
	}
	if dir != "" {
		argv = append(argv, "--bind", dir, dir)
	}
	argv = append(argv, "--", command)
	return "bwrap", append(argv, args...)
}
