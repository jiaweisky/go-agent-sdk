//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

// configureProcessGroup is a no-op on Windows.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills the child process and its descendants. The child may
// have spawned grandchildren (go run launches the built binary), and an
// orphaned grandchild keeps the parent's stdio pipes open, so the whole tree
// must go.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
