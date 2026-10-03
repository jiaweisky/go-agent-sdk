//go:build windows

package main

import "os/exec"

// configureProcessGroup is a no-op on Windows.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills the child process.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
