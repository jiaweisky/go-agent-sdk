//go:build windows

package core

import "os/exec"

// applyProcessAttrs mirrors ChildProcess's `detached` option. The bash tool
// only requests detachment on non-Windows platforms, matching TS, so there is
// nothing to apply here yet.
func applyProcessAttrs(cmd *exec.Cmd, detached bool) {}

// processTerminate mirrors ChildProcess's killSignal behavior on Windows,
// where there is no SIGTERM and processes are terminated directly.
func processTerminate(cmd *exec.Cmd, detached bool) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
