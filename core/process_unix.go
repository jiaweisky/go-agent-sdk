//go:build unix

package core

import (
	"os/exec"
	"syscall"
)

// applyProcessAttrs mirrors ChildProcess's `detached` option on POSIX: the
// child becomes the leader of a new process group so the whole group can be
// signalled together.
func applyProcessAttrs(cmd *exec.Cmd, detached bool) {
	if !detached {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processTerminate mirrors ChildProcess's killSignal default of SIGTERM,
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
