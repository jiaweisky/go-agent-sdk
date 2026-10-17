//go:build linux

package sandbox

import (
	"reflect"
	"testing"
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

func TestBwrapRegistered(t *testing.T) {
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
	if l, err := ResolveProcess("bwrap"); err != nil || l == nil {
		t.Fatalf("ResolveProcess(bwrap) = %v, %v", l, err)
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
