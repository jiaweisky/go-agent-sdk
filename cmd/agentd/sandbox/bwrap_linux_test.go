//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestBwrapLaunch(t *testing.T) {
	dir := t.TempDir()
	name, argv := bwrapLaunch(dir, StandardPolicy(dir), "bash", []string{"-c", "echo hi"})
	if name != "bwrap" {
		t.Fatalf("name = %q, want bwrap", name)
	}
	for _, want := range [][]string{
		{"--tmpfs", "/"},
		{"--ro-bind", "/usr", "/usr"},
		{"--tmpfs", "/tmp"},
		{"--bind", dir, dir},
		{"--unshare-net"},
		{"--unshare-pid"},
	} {
		if indexOfArg(argv, want[0], want[1:]...) < 0 && indexOf(argv, want[0]) < 0 {
			t.Errorf("argv missing %v: %v", want, argv)
		}
	}
	if got := argv[len(argv)-4:]; !reflect.DeepEqual(got, []string{"--", "bash", "-c", "echo hi"}) {
		t.Fatalf("tail = %v", got)
	}
}

// TestBwrapLaunchReadOff keeps the old whole-filesystem read-only view when the
// policy explicitly disables read confinement.
func TestBwrapLaunchReadOff(t *testing.T) {
	policy := StandardPolicy("")
	policy.ReadMode = FSModeOff
	_, argv := bwrapLaunch("/work", policy, "ls", nil)
	if indexOfArg(argv, "--ro-bind", "/", "/") < 0 {
		t.Fatalf("read: off should keep --ro-bind / /: %v", argv)
	}
	if indexOfArg(argv, "--tmpfs", "/") >= 0 {
		t.Fatalf("read: off should not build a fresh root: %v", argv)
	}
}

func TestBwrapWriteOffWithReadOff(t *testing.T) {
	policy := StandardPolicy("")
	policy.ReadMode = FSModeOff
	policy.WriteMode = FSModeOff
	_, argv := bwrapLaunch("/work", policy, "ls", nil)
	if indexOfArg(argv, "--bind", "/", "/") < 0 {
		t.Fatalf("write: off + read: off must expose the host root rw: %v", argv)
	}
}

func TestBwrapLaunchNoDir(t *testing.T) {
	_, argv := bwrapLaunch("", StandardPolicy(""), "ls", nil)
	for i, arg := range argv {
		if arg == "--bind" && i+1 < len(argv) && argv[i+1] == "" {
			t.Fatalf("argv has an empty --bind: %v (index %d)", argv, i)
		}
	}
}

func TestBwrapSocketAllowlist(t *testing.T) {
	policy := StandardPolicy("")
	policy.SocketMode = SocketAllowlist
	policy.SocketAllow = []string{"/var/run/docker.sock"}
	_, argv := bwrapLaunch("/work", policy, "ls", nil)
	if indexOfArg(argv, "--bind", "/var/run/docker.sock", "/var/run/docker.sock") < 0 {
		t.Fatalf("the allow-listed socket is not bound: %v", argv)
	}
}

func TestBwrapPreciseTrace(t *testing.T) {
	policy := StandardPolicy("")
	policy.ViolationMode = ViolationPrecise
	_, argv := bwrapLaunch("/work", policy, "ls", nil)
	if indexOfArg(argv, "-e", "trace=openat,connect") < 0 {
		t.Fatalf("precise mode is missing the strace trace set: %v", argv)
	}
}

func TestBwrapDenyRead(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}

	policy := StandardPolicy(dir)
	policy.DenyRead = []string{secret, private}
	_, argv := bwrapLaunch(dir, policy, "ls", nil)

	bind := indexOfArg(argv, "--bind", dir, dir)
	if bind < 0 {
		t.Fatalf("write root is not bound: %v", argv)
	}
	if mask := indexOfArg(argv, "--ro-bind", "/dev/null", secret); mask < 0 || mask < bind {
		t.Fatalf("denyRead file mask must come after the write bind: %v", argv)
	}
	if mask := indexOfArg(argv, "--tmpfs", private); mask < 0 || mask < bind {
		t.Fatalf("denyRead directory mask must come after the write bind: %v", argv)
	}
}

func TestBwrapDenySymlink(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.env")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	policy := StandardPolicy(dir)
	policy.DenyRead = []string{link}
	_, argv := bwrapLaunch(dir, policy, "ls", nil)

	if indexOfArg(argv, "--ro-bind", "/dev/null", target) < 0 {
		t.Fatalf("denyRead symlink target is not masked: %v", argv)
	}
	if indexOfArg(argv, "--ro-bind", "/dev/null", link) >= 0 {
		t.Fatalf("bwrap must not mount over the symlink destination: %v", argv)
	}
}

func TestBwrapDenyWrite(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "config.env")
	if err := os.WriteFile(blocked, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "future.env")
	privateTemp := filepath.Join(dir, "private-temp")
	if err := os.Mkdir(privateTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.env")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	policy := StandardPolicy(dir)
	policy.PrivateTemp = privateTemp
	policy.DenyWrite = []string{blocked, missing, outside}
	_, argv := bwrapLaunch(dir, policy, "ls", nil)

	bind := indexOfArg(argv, "--bind", dir, dir)
	if bind < 0 {
		t.Fatalf("write root is not bound: %v", argv)
	}
	mask := indexOfArg(argv, "--ro-bind", blocked, blocked)
	if mask < 0 || mask < bind {
		t.Fatalf("denyWrite file is not masked after the write root: %v", argv)
	}
	if indexOfArg(argv, "--tmpfs", missing) < 0 {
		t.Fatalf("missing denyWrite path is not masked: %v", argv)
	}
	if indexOfArg(argv, "--ro-bind", outside, outside) >= 0 {
		t.Fatalf("denyWrite outside the writable roots must not be bound: %v", argv)
	}
}

func TestBwrapRegistered(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not available")
	}
	if p, err := Resolve("bwrap", StandardPolicy("")); err != nil || p == nil {
		t.Fatalf("Resolve(bwrap) = %v, %v", p, err)
	}
}

func TestBwrapProcessArgs(t *testing.T) {
	policy := StandardPolicy("")
	argv := bwrapArgs([]string{"/work", "/state"}, policy, false, "/usr/bin/agentd", []string{"-config", "c.yaml"})
	if indexOfArg(argv, "--bind", "/work", "/work") < 0 || indexOfArg(argv, "--bind", "/state", "/state") < 0 {
		t.Fatalf("whole-process wrapper missing the write roots: %v", argv)
	}
	// The whole process serves listeners and calls the model, so it must not
	// unshare the network or its PID namespace.
	if indexOf(argv, "--unshare-net") >= 0 || indexOf(argv, "--unshare-pid") >= 0 {
		t.Fatalf("whole-process wrapper must keep the network and PID namespace: %v", argv)
	}
}

func TestBwrapProcessRegistered(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not available")
	}
	if l, err := ResolveProcess("bwrap"); err != nil || l == nil {
		t.Fatalf("ResolveProcess(bwrap) = %v, %v", l, err)
	}
}

// TestResolveMissingBinary exercises the availability probe: a registered
// sandbox whose wrapper binary is missing must fail Resolve (and
// ResolveProcess) with an "unavailable" error, so fail-closed refuses to start
// and fail-open warns.
func TestResolveMissingBinary(t *testing.T) {
	Register("probe-test", func(Policy) core.AppProcess { return nil })
	RegisterProcess("probe-test", func(Policy, string, []string) error { return nil })
	RegisterAvailable("probe-test", func() error { return exec.ErrNotFound })
	if _, err := Resolve("probe-test", StandardPolicy("")); err == nil {
		t.Fatal("Resolve(probe-test) succeeded despite the missing binary")
	}
	if _, err := ResolveProcess("probe-test"); err == nil {
		t.Fatal("ResolveProcess(probe-test) succeeded despite the missing binary")
	}
}

// indexOfArg returns the index of the flag whose following args equal values, or
// -1.
func indexOfArg(argv []string, flag string, values ...string) int {
	for i := 0; i+len(values) < len(argv); i++ {
		if argv[i] != flag {
			continue
		}
		ok := true
		for j, value := range values {
			if argv[i+1+j] != value {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// indexOf returns the index of an exact flag, or -1.
func indexOf(argv []string, flag string) int {
	for i, arg := range argv {
		if arg == flag {
			return i
		}
	}
	return -1
}
