package util

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeGlobFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"SKILL.md",
		"a.md",
		"sub/b.md",
		"sub/SKILL.md",
		".hidden.md",
		"sub/.secret",
		".dotdir/c.md",
	}
	for _, file := range files {
		full := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(file), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func relativeTo(t *testing.T, dir string, paths []string) []string {
	t.Helper()
	result := make([]string, len(paths))
	for i, path := range paths {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		result[i] = filepath.ToSlash(rel)
	}
	return result
}

func TestGlobScanBraceAndDoubleStar(t *testing.T) {
	dir := writeGlobFixture(t)

	got, err := GlobScan("{*.md,**/SKILL.md}", GlobOptions{
		Cwd: dir, Absolute: true, Include: "file", Symlink: true, Dot: true,
	})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	want := []string{".hidden.md", "SKILL.md", "a.md", "sub/SKILL.md"}
	if rel := relativeTo(t, dir, got); !reflect.DeepEqual(rel, want) {
		t.Fatalf("brace glob = %v, want %v", rel, want)
	}
}

func TestGlobScanNodirAndDot(t *testing.T) {
	dir := writeGlobFixture(t)

	got, err := GlobScan("**/*", GlobOptions{Cwd: dir, Absolute: true, Include: "file", Dot: true})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	want := []string{".dotdir/c.md", ".hidden.md", "SKILL.md", "a.md", "sub/.secret", "sub/SKILL.md", "sub/b.md"}
	if rel := relativeTo(t, dir, got); !reflect.DeepEqual(rel, want) {
		t.Fatalf("dot glob = %v, want %v", rel, want)
	}

	got, err = GlobScan("**/*", GlobOptions{Cwd: dir, Absolute: true, Include: "file", Dot: false})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	want = []string{"SKILL.md", "a.md", "sub/SKILL.md", "sub/b.md"}
	if rel := relativeTo(t, dir, got); !reflect.DeepEqual(rel, want) {
		t.Fatalf("no-dot glob = %v, want %v", rel, want)
	}
}

func TestGlobScanRelativeOutput(t *testing.T) {
	dir := writeGlobFixture(t)
	got, err := GlobScan("**/*.md", GlobOptions{Cwd: dir, Include: "file", Dot: true})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	want := []string{".dotdir/c.md", ".hidden.md", "SKILL.md", "a.md", "sub/SKILL.md", "sub/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relative glob = %v, want %v", got, want)
	}
}

func TestGlobScanSymlink(t *testing.T) {
	dir := writeGlobFixture(t)
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(filepath.Join(dir, "sub"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	followed, err := GlobScan("**/*.md", GlobOptions{Cwd: dir, Absolute: true, Include: "file", Dot: true, Symlink: true})
	if err != nil {
		t.Fatalf("glob follow: %v", err)
	}
	if !containsPath(followed, filepath.Join(link, "b.md")) {
		t.Fatalf("expected followed symlink to include linked/b.md: %v", followed)
	}

	unfollowed, err := GlobScan("**/*.md", GlobOptions{Cwd: dir, Absolute: true, Include: "file", Dot: true, Symlink: false})
	if err != nil {
		t.Fatalf("glob no-follow: %v", err)
	}
	if containsPath(unfollowed, filepath.Join(link, "b.md")) {
		t.Fatalf("expected no-follow to exclude linked/b.md: %v", unfollowed)
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func TestGlobScanAbsolutePattern(t *testing.T) {
	dir := writeGlobFixture(t)
	got, err := GlobScan(filepath.Join(dir, "*.md"), GlobOptions{Include: "file", Dot: true})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	want := []string{filepath.Join(dir, ".hidden.md"), filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "a.md")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("absolute pattern = %v, want %v", got, want)
	}
}

func TestGlobMatch(t *testing.T) {
	if !GlobMatch("**/*.md", "a/b/c.md") {
		t.Fatalf("expected match")
	}
	if GlobMatch("**/*.md", "a/b/c.txt") {
		t.Fatalf("expected no match")
	}
}
