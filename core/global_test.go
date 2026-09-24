package core

import (
	"path/filepath"
	"testing"
)

func TestWithDataRoot(t *testing.T) {
	root := t.TempDir()
	global := NewGlobal(WithDataRoot(root))
	if global.Data != root {
		t.Fatalf("Data = %q, want %q", global.Data, root)
	}
	if global.Log != filepath.Join(root, "log") {
		t.Fatalf("Log = %q", global.Log)
	}
	if global.Repos != filepath.Join(root, "repos") {
		t.Fatalf("Repos = %q", global.Repos)
	}
}
