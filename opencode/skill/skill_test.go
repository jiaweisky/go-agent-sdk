package skill

import (
	"os"
	"path/filepath"
	"testing"
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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDiscoverSkillsScansGivenDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "skill", "alpha", "SKILL.md"), "---\nname: alpha\n---\nA\n")
	writeFile(t, filepath.Join(dir, "skills", "beta", "SKILL.md"), "---\nname: beta\n---\nB\n")

	discovered := DiscoverSkills([]string{dir})
	if len(discovered.Matches) != 2 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
	if !contains(discovered.Dirs, filepath.Join(dir, "skill", "alpha")) {
		t.Fatalf("dirs = %v", discovered.Dirs)
	}
	if !contains(discovered.Roots, filepath.Join(dir, "skill")) {
		t.Fatalf("roots = %v", discovered.Roots)
	}
}

func TestDiscoverSkillsIgnoresMissingDirs(t *testing.T) {
	if discovered := DiscoverSkills([]string{filepath.Join(t.TempDir(), "nope")}); len(discovered.Matches) != 0 {
		t.Fatalf("matches = %v", discovered.Matches)
	}
}
