package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakeDiscovery struct {
	pulled map[string][]schema.AbsolutePath
}

func (f fakeDiscovery) Pull(url string) ([]schema.AbsolutePath, error) {
	return f.pulled[url], nil
}

func TestDiscoverExternalGlobal(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\nA\n")
	writeFile(t, filepath.Join(home, ".agents", "skills", "beta", "SKILL.md"), "---\nname: beta\n---\nB\n")

	discovered := DiscoverSkills(DiscoverConfig{Directory: dir, Worktree: dir, Home: home})
	if len(discovered.Matches) != 2 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	want := filepath.Join(home, ".claude", "skills", "alpha")
	if !contains(discovered.Dirs, want) {
		t.Fatalf("dirs = %v, missing %s", discovered.Dirs, want)
	}
}

func TestDiscoverDisableClaudeSkills(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\nA\n")
	writeFile(t, filepath.Join(home, ".agents", "skills", "beta", "SKILL.md"), "---\nname: beta\n---\nB\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home, DisableClaudeCodeSkills: true,
	})
	if len(discovered.Matches) != 1 || filepath.Base(filepath.Dir(discovered.Matches[0])) != "beta" {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverDisableExternalSkills(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	writeFile(t, filepath.Join(home, ".agents", "skills", "beta", "SKILL.md"), "---\nname: beta\n---\nB\n")
	writeFile(t, filepath.Join(dir, ".opencode", "skills", "gamma", "SKILL.md"), "---\nname: gamma\n---\nG\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home,
		ConfigDirs:            []string{filepath.Join(dir, ".opencode")},
		DisableExternalSkills: true,
	})
	if len(discovered.Matches) != 1 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	if filepath.Base(filepath.Dir(discovered.Matches[0])) != "gamma" {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverProjectUpWalk(t *testing.T) {
	home := t.TempDir()
	worktree := t.TempDir()
	directory := filepath.Join(worktree, "packages", "app")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(worktree, ".agents", "skills", "root", "SKILL.md"), "---\nname: root\n---\nR\n")

	discovered := DiscoverSkills(DiscoverConfig{Directory: directory, Worktree: worktree, Home: home})
	if len(discovered.Matches) != 1 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	if filepath.Base(filepath.Dir(discovered.Matches[0])) != "root" {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverConfigSkillsPaths(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	absolute := t.TempDir()
	writeFile(t, filepath.Join(home, "shared", "one", "SKILL.md"), "---\nname: one\n---\n1\n")
	writeFile(t, filepath.Join(dir, "relative", "two", "SKILL.md"), "---\nname: two\n---\n2\n")
	writeFile(t, filepath.Join(absolute, "three", "SKILL.md"), "---\nname: three\n---\n3\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home,
		Skills: []string{"~/shared", "relative", absolute, filepath.Join(dir, "missing")},
	})
	if len(discovered.Matches) != 3 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverConfigSkillsURLs(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	pulled := t.TempDir()
	writeFile(t, filepath.Join(pulled, "remote", "SKILL.md"), "---\nname: remote\n---\nR\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home,
		Skills:    []string{"https://example.com/skills/"},
		Discovery: fakeDiscovery{pulled: map[string][]schema.AbsolutePath{"https://example.com/skills/": {schema.AbsolutePath(pulled)}}},
	})
	if len(discovered.Matches) != 1 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	if filepath.Base(filepath.Dir(discovered.Matches[0])) != "remote" {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverConfigDirPatterns(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".opencode")
	writeFile(t, filepath.Join(configDir, "skill", "one", "SKILL.md"), "---\nname: one\n---\n1\n")
	writeFile(t, filepath.Join(configDir, "skills", "nested", "two", "SKILL.md"), "---\nname: two\n---\n2\n")
	writeFile(t, filepath.Join(configDir, "skills", "ignored.md"), "not a skill\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home, ConfigDirs: []string{configDir},
	})
	if len(discovered.Matches) != 2 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}

func TestDiscoverRootsAreContainers(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".opencode", "skills", "foo", "SKILL.md"), "---\nname: foo\n---\nF\n")
	writeFile(t, filepath.Join(dir, ".opencode", "skills", "foo", "reference.md"), "notes\n")

	discovered := DiscoverSkills(DiscoverConfig{
		Directory: dir, Worktree: dir, Home: home,
		ConfigDirs: []string{filepath.Join(dir, ".opencode")},
	})
	wantRoot := filepath.Join(dir, ".opencode", "skills")
	if len(discovered.Roots) != 1 || discovered.Roots[0] != wantRoot {
		t.Fatalf("roots = %v, want [%s]", discovered.Roots, wantRoot)
	}
	if len(discovered.Matches) != 1 || filepath.Base(discovered.Matches[0]) != "SKILL.md" {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	if len(discovered.Dirs) != 1 || discovered.Dirs[0] != filepath.Join(wantRoot, "foo") {
		t.Fatalf("dirs = %v", discovered.Dirs)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
