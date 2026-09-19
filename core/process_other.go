//go:build !unix && !windows

package core

import "os/exec"

// applyProcessAttrs has no platform-specific process attributes to apply.
func applyProcessAttrs(cmd *exec.Cmd, detached bool) {}

// processTerminate falls back to terminating the process directly.
func processTerminate(cmd *exec.Cmd, detached bool) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
