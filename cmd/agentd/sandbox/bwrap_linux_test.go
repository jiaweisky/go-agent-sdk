//go:build linux

package sandbox

import (
	"reflect"
	"testing"
)

func TestBwrapLaunch(t *testing.T) {
	name, argv := bwrapLaunchWith("/work", nil, "bash", []string{"-c", "echo hi"})
	if name != "bwrap" {
		t.Fatalf("name = %q, want bwrap", name)
	}
	want := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--unshare-net",
		"--die-with-parent",
		"--bind", "/work", "/work",
		"--", "bash", "-c", "echo hi",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
}

func TestBwrapLaunchMasks(t *testing.T) {
	_, argv := bwrapLaunchWith("/work", []string{"/home/u/.ssh", "/home/u/.aws"}, "ls", nil)
	maskIndex := indexOfArg(argv, "--tmpfs", "/home/u/.ssh")
	bindIndex := indexOfArg(argv, "--bind", "/work")
	if maskIndex < 0 {
		t.Fatalf("mask tmpfs missing from %v", argv)
	}
	if bindIndex < 0 || maskIndex > bindIndex {
		t.Fatalf("mask must be mounted before the workdir bind: %v", argv)
	}
}

// indexOfArg returns the index of the flag whose following arg equals value, or -1.
func indexOfArg(argv []string, flag, value string) int {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return i
		}
	}
	return -1
}

func TestBwrapLaunchNoDir(t *testing.T) {
	_, argv := bwrapLaunchWith("", nil, "ls", nil)
	for i, arg := range argv {
		if arg == "--bind" {
			t.Fatalf("argv has --bind without a dir: %v (index %d)", argv, i)
		}
	}
}

func TestBwrapRegistered(t *testing.T) {
	if p, err := Resolve("bwrap"); err != nil || p == nil {
		t.Fatalf("Resolve(bwrap) = %v, %v", p, err)
	}
}

func TestBwrapProcessArgs(t *testing.T) {
	argv := bwrapProcessArgs([]string{"/work", "/state"}, "/usr/bin/agentd", []string{"-config", "c.yaml"})
	want := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--die-with-parent",
		"--bind", "/work", "/work",
		"--bind", "/state", "/state",
		"--", "/usr/bin/agentd", "-config", "c.yaml",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	// The whole process serves listeners and calls the model, so it must not
	// unshare the network (unlike the per-command wrapper).
	if indexOf(argv, "--unshare-net") >= 0 {
		t.Fatalf("whole-process wrapper must keep the network: %v", argv)
	}
}

func TestBwrapProcessRegistered(t *testing.T) {
	if l, err := ResolveProcess("bwrap"); err != nil || l == nil {
		t.Fatalf("ResolveProcess(bwrap) = %v, %v", l, err)
	}
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
