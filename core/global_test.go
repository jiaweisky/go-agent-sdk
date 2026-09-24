package core

import (
	"path/filepath"
	"testing"
)

func TestNewGlobalDerivesFromRoot(t *testing.T) {
	root := t.TempDir()
	global := NewGlobal(root, "/home/user")
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"Data", global.Data, filepath.Join(root, "data")},
		{"Cache", global.Cache, filepath.Join(root, "cache")},
		{"State", global.State, filepath.Join(root, "state")},
		{"Tmp", global.Tmp, filepath.Join(root, "tmp")},
		{"Bin", global.Bin, filepath.Join(root, "cache", "bin")},
		{"Log", global.Log, filepath.Join(root, "data", "log")},
		{"Repos", global.Repos, filepath.Join(root, "data", "repos")},
	} {
		if tc.got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if global.Home != "/home/user" {
		t.Fatalf("Home = %q", global.Home)
	}
}

func TestDefaultGlobalRoot(t *testing.T) {
	want := filepath.Join("/home/user", ".local", "share", "opencode")
	if got := DefaultGlobalRoot("/home/user"); got != want {
		t.Fatalf("DefaultGlobalRoot = %q, want %q", got, want)
	}
}
