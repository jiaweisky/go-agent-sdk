//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child in its own process group, so the whole
// `go run` tree (the go tool and the server it builds) can be killed together.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the child's process group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
