//go:build windows

package core

import "os/exec"

// applyProcessAttrs is a no-op: Windows has no process-group equivalent, and
// detachment is only requested on non-Windows platforms.
func applyProcessAttrs(cmd *exec.Cmd, detached bool) {}

// processTerminate kills the child: Windows has no SIGTERM,
// where there is no SIGTERM and processes are terminated directly.
func processTerminate(cmd *exec.Cmd, detached bool) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
