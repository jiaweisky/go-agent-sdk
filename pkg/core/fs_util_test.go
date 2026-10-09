package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMimeType(t *testing.T) {
	cases := map[string]string{
		"a.txt":     "text/plain",
		"a.md":      "text/markdown",
		"a.csv":     "text/csv",
		"a.js":      "text/javascript",
		"a.ts":      "video/mp2t",
		"a.go":      "application/octet-stream",
		"a.py":      "application/octet-stream",
		"a.yaml":    "text/yaml",
		"a.json":    "application/json",
		"a.png":     "image/png",
		"a.jpg":     "image/jpeg",
		"a.unknown": "application/octet-stream",
		"README":    "application/octet-stream",
	}
	for path, want := range cases {
		if got := MimeType(path); got != want {
			t.Errorf("MimeType(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFindUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	matches := FindUp([]string{".git"}, deep)
	if len(matches) != 1 || matches[0] != filepath.Join(root, "a", ".git") {
		t.Fatalf("FindUp = %v", matches)
	}
}

func TestStatAndReadDirKinds(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != FileKindFile {
		t.Fatalf("Stat file kind = %q", info.Type)
	}
	info, err = Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != FileKindDirectory {
		t.Fatalf("Stat dir kind = %q", info.Type)
	}

	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name == "f.txt" {
			found = true
			if entry.Type != FileKindFile {
				t.Fatalf("ReadDir file kind = %q", entry.Type)
			}
		}
	}
	if !found {
		t.Fatal("ReadDir did not list f.txt")
	}
}
