//go:build unix

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// execReplace replaces the current process image with command, so signals and
// the exit code behave as if agentd had started inside the sandbox. The
// environment (including the recursion sentinel) is inherited.
func execReplace(command string, args []string) error {
	path, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("whole-process sandbox %q not found: %w", command, err)
	}

	return syscall.Exec(path, append([]string{command}, args...), os.Environ())
}
