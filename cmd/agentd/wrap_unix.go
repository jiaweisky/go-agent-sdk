//go:build unix

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// execReplace replaces the current process image with the sandboxed command, so
// signals and the exit code behave as if agentd had started inside the sandbox.
func execReplace(name string, args, env []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("whole-process sandbox %q not found: %w", name, err)
	}
	return syscall.Exec(path, append([]string{name}, args...), env)
}
