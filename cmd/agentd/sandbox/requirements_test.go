package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// platformSandbox resolves the host's wrapper, shell and shell args, or skips
// when the platform has no usable wrapper. It is the portable part of the
// sandbox acceptance matrix: CI runs it on macOS, Linux and Windows.
func platformSandbox(t *testing.T) (wrapper, shell string, shellArgs func(string) []string) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Skip("sandbox-exec is not available")
		}
		return "seatbelt", "/bin/sh", func(command string) []string { return []string{"-c", command} }
	case "linux":
		if _, err := exec.LookPath("bwrap"); err != nil {
			t.Skip("bwrap is not available")
		}
		return "bwrap", "/bin/sh", func(command string) []string { return []string{"-c", command} }
	case "windows":
		return "appcontainer", "cmd", func(command string) []string { return []string{"/c", command} }
	default:
		t.Skip("no sandbox wrapper for " + runtime.GOOS)
		return "", "", nil
	}
}

// skipOnWindows gates the POSIX-shaped slices of the sandbox matrix. These
// tests assert literal /-rooted policy paths or wrapper behaviors the windows
// appcontainer wrapper does not provide: it denies out-of-sandbox writes only
// by exit status (no PolicyViolation reports), and its private-temp flow is
// not usable through cmd.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the windows appcontainer wrapper does not provide this behavior")
	}
}

// TestRequirementsReadWriteEnv is the portable slice of the acceptance matrix:
// a write/read inside works, a write outside is denied and reported, and a
// secret environment variable is scrubbed.
func TestRequirementsReadWriteEnv(t *testing.T) {
	skipOnWindows(t)
	wrapper, shell, shellArgs := platformSandbox(t)
	dir := t.TempDir()
	process, err := Resolve(wrapper, StandardPolicy(dir))
	if err != nil || process == nil {
		t.Fatalf("Resolve(%s) = %v, %v", wrapper, process, err)
	}
	ctx := context.Background()

	inside, err := process.Run(ctx, shell, shellArgs("echo ok > inside.txt"), core.RunOptions{Dir: dir})
	if err != nil || inside.ExitCode != 0 {
		t.Fatalf("write inside the workdir failed: %v (exit %d, %s)", err, inside.ExitCode, inside.Stderr)
	}
	readCommand := "cat inside.txt"
	if runtime.GOOS == "windows" {
		readCommand = "type inside.txt"
	}
	read, err := process.Run(ctx, shell, shellArgs(readCommand), core.RunOptions{Dir: dir})
	if err != nil || read.ExitCode != 0 {
		t.Fatalf("read inside the workdir failed: %v (exit %d, %s)", err, read.ExitCode, read.Stderr)
	}
	if !strings.Contains(string(read.Stdout)+string(read.Output), "ok") {
		t.Fatalf("unexpected read output: %q", read.Stdout)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	outside := filepath.Join(home, ".agentd-requirements-test")
	if runtime.GOOS == "linux" {
		// bwrap hides the home tree, so a write there returns ENOENT (not
		// reportable). /usr is bound read-only, which yields EROFS.
		outside = "/usr/agentd-requirements-test"
	}
	_ = os.Remove(outside)
	var violations []core.PolicyViolation
	denied, err := process.Run(ctx, shell, shellArgs(fmt.Sprintf("echo bad > %q", outside)), core.RunOptions{
		Dir:           dir,
		CombineOutput: true,
		OnViolation:   func(v core.PolicyViolation) { violations = append(violations, v) },
	})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if denied.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatal("a write outside the sandbox was allowed")
	}
	if len(violations) == 0 {
		t.Fatal("the denial was not reported as a violation")
	}

	t.Setenv("AGENTD_REQUIREMENTS_SECRET", "leak-me")
	envCommand := "printenv AGENTD_REQUIREMENTS_SECRET"
	if runtime.GOOS == "windows" {
		envCommand = "set AGENTD_REQUIREMENTS_SECRET"
	}
	env, err := process.Run(ctx, shell, shellArgs(envCommand), core.RunOptions{Dir: dir, CombineOutput: true})
	if err != nil {
		t.Fatalf("env run: %v", err)
	}
	if strings.Contains(string(env.Output)+string(env.Stdout)+string(env.Stderr), "leak-me") {
		t.Fatalf("the secret leaked into the child environment: %q", env.Output)
	}
}

// TestRequirementsPrivateTemp checks a sandboxed command can use the per-run
// temp under TempRoot and that the run directory is cleaned up.
func TestRequirementsPrivateTemp(t *testing.T) {
	skipOnWindows(t)
	wrapper, shell, shellArgs := platformSandbox(t)
	dir := t.TempDir()
	tempRoot := filepath.Join(t.TempDir(), "sandbox-tmp")
	policy := StandardPolicy(dir)
	policy.TempRoot = tempRoot
	process, err := Resolve(wrapper, policy)
	if err != nil || process == nil {
		t.Fatalf("Resolve(%s) = %v, %v", wrapper, process, err)
	}
	command := `echo hi > "$TMPDIR/inside.txt" && cat "$TMPDIR/inside.txt"`
	if runtime.GOOS == "windows" {
		command = `echo hi > "%TEMP%\inside.txt" && type "%TEMP%\inside.txt"`
	}
	result, err := process.Run(context.Background(), shell, shellArgs(command), core.RunOptions{Dir: dir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("the private temp is unusable: %v (exit %d, %s)", err, result.ExitCode, result.Stderr)
	}
	if !strings.Contains(string(result.Stdout)+string(result.Output), "hi") {
		t.Fatalf("unexpected output: %q", result.Stdout)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatalf("read temp root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the per-run temp was not cleaned up: %v", entries)
	}
}

// TestPreciseViolationPlatform guards the precise mode's platform support.
func TestPreciseViolationPlatform(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("precise is supported on linux (strace checked at startup)")
	}
	policy := StandardPolicy(t.TempDir())
	policy.ViolationMode = ViolationPrecise
	if _, err := Resolve("seatbelt", policy); err == nil {
		t.Fatalf("precise must be rejected on %s", runtime.GOOS)
	}
}
