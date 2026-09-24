// Package skill scans explicit directories for SKILL.md files. Unlike the
// opencode CLI it performs no environment, global or project discovery: the
// caller supplies every directory to scan.
package skill

import (
	"path/filepath"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/util"
)

// SkillPattern matches a SKILL.md anywhere under a container directory.
const SkillPattern = "**/SKILL.md"

// Discovered is the scan result: the matched SKILL.md paths, their parent
// directories, and the container roots that were scanned.
type Discovered struct {
	// Matches are absolute SKILL.md paths.
	Matches []string
	// Dirs are the parent directories of Matches, in insertion order.
	Dirs []string
	// Roots are the container directories that were scanned. They are the
	// sources handed to core.Skill so its {*.md,**/SKILL.md} glob only sees
	// skills, not sibling files inside a skill directory.
	Roots []string
}

// DiscoverSkills scans each directory's `skill` and `skills` subdirectories for
// SKILL.md files.
func DiscoverSkills(dirs []string) Discovered {
	matches := newOrderedSet()
	parentDirs := newOrderedSet()
	roots := newOrderedSet()

	addRoot := func(root string) {
		if !core.IsDir(root) {
			return
		}
		roots.add(root)
		found, err := core.Glob(SkillPattern, util.GlobOptions{
			Cwd: root, Absolute: true, Include: "file", Symlink: true, Dot: true,
		})
		if err != nil {
			return
		}
		for _, match := range found {
			matches.add(match)
			parentDirs.add(filepath.Dir(match))
		}
	}

	for _, dir := range dirs {
		addRoot(filepath.Join(dir, "skill"))
		addRoot(filepath.Join(dir, "skills"))
	}

	return Discovered{Matches: matches.values(), Dirs: parentDirs.values(), Roots: roots.values()}
}

// orderedSet preserves insertion order while deduplicating.
type orderedSet struct {
	seen  map[string]struct{}
	items []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]struct{}{}}
}

func (s *orderedSet) add(value string) {
	if _, ok := s.seen[value]; ok {
		return
	}
	s.seen[value] = struct{}{}
	s.items = append(s.items, value)
}

func (s *orderedSet) values() []string {
	return s.items
}
