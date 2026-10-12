//go:build linux

package sandbox

import (
	"reflect"
	"testing"
)

func TestBwrapLaunch(t *testing.T) {
	name, argv := bwrapLaunch("/work", "bash", []string{"-c", "echo hi"})
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

func TestBwrapLaunchNoDir(t *testing.T) {
	_, argv := bwrapLaunch("", "ls", nil)
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
