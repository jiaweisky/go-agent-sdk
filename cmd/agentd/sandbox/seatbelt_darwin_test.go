//go:build darwin

package sandbox

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestSeatbeltLaunch(t *testing.T) {
	name, argv := seatbeltLaunch("/work", StandardPolicy("/work"), "bash", []string{"-c", "echo hi"})
	if name != "sandbox-exec" {
		t.Fatalf("name = %q, want sandbox-exec", name)
	}
	if len(argv) != 5 || argv[0] != "-p" || argv[2] != "bash" || argv[3] != "-c" || argv[4] != "echo hi" {
		t.Fatalf("argv = %v", argv)
	}
	if !strings.Contains(argv[1], `(subpath "/work")`) {
		t.Fatalf("profile missing the workdir subpath: %s", argv[1])
	}
}

func TestSeatbeltProfile(t *testing.T) {
	profile := seatbeltCommandProfile("/work", StandardPolicy("/work"))
	for _, want := range []string{
		"(version 1)", "(allow default)", "(deny network* (remote ip))",
		"(deny file-write*)", "(allow file-write*",
		"(deny file-read* (subpath", "(allow file-read*",
		"(deny process-info*)", `(subpath "/work")`,
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile missing %q: %s", want, profile)
		}
	}
	if strings.Contains(profile, `(subpath "")`) {
		t.Errorf("empty dir produced an empty subpath: %s", profile)
	}
}

// TestSeatbeltProfileReadOff checks that an explicit read: off drops the read
// allow-list (an explicit opt-out, not the default).
func TestSeatbeltProfileReadOff(t *testing.T) {
	policy := StandardPolicy("/work")
	policy.ReadMode = FSModeOff
	profile := seatbeltCommandProfile("/work", policy)
	if strings.Contains(profile, "(allow file-read*") || strings.Contains(profile, "(deny file-read*") {
		t.Fatalf("read: off must not emit read rules: %s", profile)
	}
}

// TestSeatbeltSocketAllowlist checks an explicitly allowed UNIX socket is added
// to both the read and write clauses.
func TestSeatbeltSocketAllowlist(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "test.sock")
	policy := StandardPolicy("/work")
	policy.SocketMode = SocketAllowlist
	policy.SocketAllow = []string{socket}
	profile := seatbeltCommandProfile("/work", policy)
	if strings.Count(profile, `(subpath "`+socket+`")`) != 2 {
		t.Fatalf("the socket must be readable and writable: %s", profile)
	}
}

// TestSeatbeltPrivateTempUnderHome checks a per-run temp inside a denied home
// region is re-allowed and usable.
func TestSeatbeltPrivateTempUnderHome(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := StandardPolicy(dir)
	policy.TempRoot = filepath.Join(home, "state", "sandbox-tmp")
	process, err := Resolve("seatbelt", policy)
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	result, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", `echo hi > "$TMPDIR/inside.txt" && cat "$TMPDIR/inside.txt"`},
		core.RunOptions{Dir: dir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("the private temp is unusable: %v (exit %d, %s)", err, result.ExitCode, result.Stderr)
	}
	if !strings.Contains(string(result.Stdout), "hi") {
		t.Fatalf("unexpected output: %q", result.Stdout)
	}
}

// TestSeatbeltUnixSocketAllowlist checks an allow-listed UNIX socket outside the
// workdir is reachable, while AF_UNIX stays allowed by the file policy (the
// network rule must deny IP only).
func TestSeatbeltUnixSocketAllowlist(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	python := "/usr/bin/python3"
	if _, err := os.Stat(python); err != nil {
		t.Skip("system python3 not available")
	}
	dir := t.TempDir()
	// Keep the socket path short: macOS sun_path is ~104 bytes and t.TempDir()
	// is long.
	sockDir, err := os.MkdirTemp("/private/tmp", "agentd-sock-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if unix, ok := listener.(*net.UnixListener); ok {
		_ = unix.SetDeadline(time.Now().Add(5 * time.Second))
	}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("ok"))
	}()

	policy := StandardPolicy(dir)
	// The system Python needs process introspection and a few home reads to
	// start; this test only exercises the socket rule.
	policy.DenyProcessInfo = false
	policy.ReadMode = FSModeOff
	policy.SocketMode = SocketAllowlist
	policy.SocketAllow = []string{sock}
	process, err := Resolve("seatbelt", policy)
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	script := `import socket,sys
s = socket.socket(socket.AF_UNIX)
s.connect(sys.argv[1])
print(s.recv(2).decode())`
	result, err := process.Run(context.Background(), python, []string{"-c", script, sock}, core.RunOptions{Dir: dir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("the allow-listed socket is unreachable: %v (exit %d, %s)", err, result.ExitCode, result.Stderr)
	}
	if !strings.Contains(string(result.Stdout)+string(result.Output), "ok") {
		t.Fatalf("unexpected output: %q", result.Stdout)
	}
}

// TestSeatbeltReadMask verifies a credential directory outside the read
// allow-list cannot be read, while the working directory stays usable.
func TestSeatbeltReadMask(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	process, err := Resolve("seatbelt", StandardPolicy(dir))
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	blocked, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "cat " + filepath.Join(ssh, "id_rsa")}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blocked.ExitCode == 0 {
		t.Fatalf("reading the credential outside the allow-list succeeded: %q", blocked.Stdout)
	}

	// The working directory is still readable.
	ok, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo hi"}, core.RunOptions{Dir: dir})
	if err != nil || ok.ExitCode != 0 {
		t.Fatalf("the workdir became unusable: %v (exit %d, stderr %s)", err, ok.ExitCode, ok.Stderr)
	}
}

// TestSeatbeltEnvScrub checks the child environment does not inherit secrets.
func TestSeatbeltEnvScrub(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	t.Setenv("AGENTD_TEST_SECRET", "leak-me")
	dir := t.TempDir()
	process, err := Resolve("seatbelt", StandardPolicy(dir))
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	result, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "printenv AGENTD_TEST_SECRET"}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(string(result.Stdout), "leak-me") {
		t.Fatalf("the secret leaked into the child environment: %q", result.Stdout)
	}
}

func TestSeatbeltRegistered(t *testing.T) {
	if p, err := Resolve("seatbelt", StandardPolicy("")); err != nil || p == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", p, err)
	}
}

func TestSeatbeltProcessProfile(t *testing.T) {
	policy := StandardPolicy("")
	profile := seatbeltProcessProfile([]string{"/work", "/state"}, policy)
	for _, want := range []string{
		"(version 1)", "(allow default)", "(deny file-write*)",
		`(subpath "/work")`, `(subpath "/state")`,
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("whole-process profile missing %q: %s", want, profile)
		}
	}
	// The whole process serves listeners and calls the model, so the network
	// must stay up (unlike the per-command profile).
	if strings.Contains(profile, "(deny network* (remote ip))") {
		t.Errorf("whole-process profile must keep the network: %s", profile)
	}
	if strings.Contains(profile, "(deny process-info*)") {
		t.Errorf("whole-process profile must keep process introspection: %s", profile)
	}
}

func TestSeatbeltProcessRegistered(t *testing.T) {
	if l, err := ResolveProcess("seatbelt"); err != nil || l == nil {
		t.Fatalf("ResolveProcess(seatbelt) = %v, %v", l, err)
	}
}

// TestSeatbeltProcessEnforcesWrites runs the whole-process profile for real: a
// write under an allowed root succeeds, a write outside every root is denied.
func TestSeatbeltProcessEnforcesWrites(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	root := t.TempDir()
	policy := StandardPolicy(root)
	argv := seatbeltProcessArgs([]string{root}, policy, "/bin/sh",
		[]string{"-c", "echo hi > " + filepath.Join(root, "inside.txt")})
	if out, err := exec.Command("sandbox-exec", argv...).CombinedOutput(); err != nil {
		t.Fatalf("write inside a wrapped root was denied: %v (%s)", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "inside.txt")); err != nil {
		t.Fatalf("inside.txt was not written: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	outside := filepath.Join(home, ".agentd-wrap-deny-test")
	denyArgv := seatbeltProcessArgs([]string{root}, policy, "/bin/sh", []string{"-c", "echo bad > " + outside})
	if err := exec.Command("sandbox-exec", denyArgv...).Run(); err == nil {
		_ = os.Remove(outside)
		t.Fatalf("write outside the wrapped roots was allowed: %s", outside)
	}
}

// TestSeatbeltEnforcesWrites runs a real command through the wrapper: a write
// under the working directory succeeds, and a write outside the allowed roots
// (the user's home) is denied.
func TestSeatbeltEnforcesWrites(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	dir := t.TempDir()
	process, err := Resolve("seatbelt", StandardPolicy(dir))
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}

	inside, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo hi > inside.txt"}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("write inside the workdir errored: %v", err)
	}
	if inside.ExitCode != 0 {
		t.Fatalf("write inside the workdir was denied (exit %d, stderr %s)", inside.ExitCode, inside.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatalf("inside.txt was not written: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	outside := filepath.Join(home, ".agentd-seatbelt-deny-test")
	denied, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo bad > " + outside}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if denied.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatalf("write outside the sandbox was allowed (expected a denial): %s", outside)
	}
}

// TestSeatbeltStreams runs a real command through the wrapper's streaming path.
func TestSeatbeltStreams(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	dir := t.TempDir()
	process, err := Resolve("seatbelt", StandardPolicy(dir))
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	var lines []string
	for line, err := range process.RunStream(context.Background(), "/bin/sh",
		[]string{"-c", "echo one; echo two"}, core.RunStreamOptions{Dir: dir}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		lines = append(lines, line)
	}
	if !reflect.DeepEqual(lines, []string{"one", "two"}) {
		t.Fatalf("lines = %v, want [one two]", lines)
	}
}
