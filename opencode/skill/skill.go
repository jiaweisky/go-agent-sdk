// Package skill ports the filesystem skill discovery performed by the opencode
// CLI (packages/opencode/src/skill/index.ts). It is narrowed to the pure
// discovery logic: the caller supplies config directories, the global home and
// the runtime flags instead of the CLI's Effect services.
package skill

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

const (
	// ClaudeExternalDir is the shared Claude Code skills root.
	ClaudeExternalDir = ".claude"
	// AgentsExternalDir is the shared agents skills root.
	AgentsExternalDir = ".agents"
	// SkillPattern matches a SKILL.md anywhere under a container directory.
	SkillPattern = "**/SKILL.md"
)

// SkillDiscovery is the narrow URL discovery surface skill discovery needs. It
// mirrors the subset of core.SkillDiscovery used by discoverSkills.
type SkillDiscovery interface {
	Pull(url string) ([]schema.AbsolutePath, error)
}

// DiscoverConfig carries the inputs discoverSkills reads from its Effect
// services and runtime flags.
type DiscoverConfig struct {
	// Directory is the directory the session was opened in.
	Directory string
	// Worktree is the project root; upward external discovery stops here.
	Worktree string
	// Home is the global home directory (~/.claude, ~/.agents and `~/` paths).
	Home string
	// ConfigDirs are the config directories to scan for {skill,skills}.
	ConfigDirs []string
	// Skills are the flat config `skills` entries (paths or http(s) URLs).
	Skills []string
	// DisableExternalSkills skips ~/.claude, ~/.agents and project discovery.
	DisableExternalSkills bool
	// DisableClaudeCodeSkills additionally skips the .claude external root.
	DisableClaudeCodeSkills bool
	// Discovery pulls remote skill indexes for URL entries. May be nil.
	Discovery SkillDiscovery
}

// Discovered mirrors the { matches, dirs } result of discoverSkills, plus the
// container roots.
type Discovered struct {
	// Matches are absolute SKILL.md paths.
	Matches []string
	// Dirs are the parent directories of Matches, preserving insertion order.
	// opencode v1 uses these for the external_directory permission whitelist.
	Dirs []string
	// Roots are the container directories that were scanned. They are the
	// directory sources to hand to core.Skill so its {*.md,**/SKILL.md} glob
	// only sees skills, not sibling files inside a skill directory.
	Roots []string
}

// DiscoverSkills mirrors Skill.discoverSkills: it scans external, project,
// config-directory, configured-path and configured-URL skill roots and returns
// the matched SKILL.md files, their parent directories and the scanned roots.
func DiscoverSkills(cfg DiscoverConfig) Discovered {
	matches := newOrderedSet()
	dirs := newOrderedSet()
	roots := newOrderedSet()

	scan := func(root string, dot bool) {
		found, err := core.Glob(SkillPattern, util.GlobOptions{
			Cwd: root, Absolute: true, Include: "file", Symlink: true, Dot: dot,
		})
		if err != nil {
			return
		}
		for _, match := range found {
			matches.add(match)
			dirs.add(filepath.Dir(match))
		}
	}
	addRoot := func(root string, dot bool) {
		if !core.IsDir(root) {
			return
		}
		roots.add(root)
		scan(root, dot)
	}

	if !cfg.DisableExternalSkills {
		var externalDirs []string
		if !cfg.DisableClaudeCodeSkills {
			externalDirs = append(externalDirs, ClaudeExternalDir)
		}
		externalDirs = append(externalDirs, AgentsExternalDir)

		for _, dir := range externalDirs {
			addRoot(filepath.Join(cfg.Home, dir, "skills"), true)
		}
		for _, root := range core.Up(externalDirs, cfg.Directory, cfg.Worktree) {
			addRoot(filepath.Join(root, "skills"), true)
		}
	}

	for _, dir := range cfg.ConfigDirs {
		addRoot(filepath.Join(dir, "skill"), false)
		addRoot(filepath.Join(dir, "skills"), false)
	}

	for _, item := range cfg.Skills {
		if isHTTPURL(item) {
			if cfg.Discovery == nil {
				continue
			}
			pulled, err := cfg.Discovery.Pull(item)
			if err != nil {
				continue
			}
			for _, dir := range pulled {
				addRoot(string(dir), false)
			}
			continue
		}
		addRoot(expandSkillPath(item, cfg.Home, cfg.Directory), false)
	}

	return Discovered{Matches: matches.values(), Dirs: dirs.values(), Roots: roots.values()}
}

func expandSkillPath(item, home, directory string) string {
	expanded := item
	if strings.HasPrefix(item, "~/") {
		expanded = filepath.Join(home, item[2:])
	}
	if filepath.IsAbs(expanded) {
		return expanded
	}
	return filepath.Join(directory, expanded)
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// orderedSet preserves insertion order while deduplicating, matching the TS
// Set iteration used by discoverSkills.
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
