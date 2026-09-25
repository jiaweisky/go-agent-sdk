//go:build unix

package core

import (
	"os/exec"
	"syscall"
)

// applyProcessAttrs puts the child in its own process group when detached, so
// child becomes the leader of a new process group so the whole group can be
// signalled together.
func applyProcessAttrs(cmd *exec.Cmd, detached bool) {
	if !detached {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processTerminate signals the child, defaulting to SIGTERM,
// signalling the whole process group when the command is detached.
func processTerminate(cmd *exec.Cmd, detached bool) error {
	if cmd.Process == nil {
		return nil
	}
	if detached {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err == nil {
			return nil
		}
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
