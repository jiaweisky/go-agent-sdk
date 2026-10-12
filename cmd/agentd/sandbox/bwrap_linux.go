//go:build linux

package sandbox

import "github.com/jiaweisky/go-agent-sdk/pkg/core"

func init() {
	Register("bwrap", func() core.AppProcess {
		return sandboxed{inner: core.NewAppProcess(), launch: bwrapLaunch}
	})
}

// bwrapLaunch wraps a command in bubblewrap (`bwrap`): the whole filesystem is
// mounted read-only, the working directory is writable, /tmp is a private tmpfs,
// the network is unshared, and the sandbox dies with its parent. It blocks writes
// outside the project and network egress but does NOT restrict reads (the whole
// filesystem stays readable). Best-effort: the SDK ships no guarantee for it; it
// requires `bwrap` and a kernel with user namespaces enabled.
func bwrapLaunch(dir, command string, args []string) (string, []string) {
	argv := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--unshare-net",
		"--die-with-parent",
	}
	if dir != "" {
		argv = append(argv, "--bind", dir, dir)
	}
	argv = append(argv, "--", command)
	return "bwrap", append(argv, args...)
}
