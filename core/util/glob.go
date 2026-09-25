package util

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar"
)

// GlobOptions configures a glob scan.
type GlobOptions struct {
	Cwd      string
	Absolute bool
	Include  string // "file" | "all"
	Dot      bool
	Symlink  bool
}

// GlobScan expands brace alternatives and matches each pattern by walking the
// filesystem. Dot and Symlink follow the npm glob options: when Dot is false,
// wildcard segments do not match entries beginning with "." (literal dot segments
// still match); when Symlink is false, symlinked directories are not traversed.
func GlobScan(pattern string, options GlobOptions) ([]string, error) {
	cwd := options.Cwd
	if cwd == "" {
		cwd = "."
	}
	absoluteCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var results []string
	for _, expanded := range expandBraces(pattern) {
		matches, err := globPattern(expanded, absoluteCwd, options)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			results = append(results, match)
		}
	}
	sort.Strings(results)
	return results, nil
}

func globPattern(pattern, cwd string, options GlobOptions) ([]string, error) {
	slashPattern := filepath.ToSlash(pattern)
	absolute := filepath.IsAbs(pattern)
	format := func(full string) string {
		if absolute || options.Absolute {
			return full
		}
		rel, err := filepath.Rel(cwd, full)
		if err != nil {
			return full
		}
		return rel
	}

	if !strings.ContainsAny(slashPattern, "*?[{") {
		return globLiteral(pattern, cwd, absolute, options, format)
	}

	base := cwd
	if absolute {
		base = globLiteralBase(pattern)
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	walker := &globWalker{
		slashPattern: slashPattern,
		absolute:     absolute,
		options:      options,
		literals:     literalSegments(slashPattern),
		format:       format,
		visited:      map[string]struct{}{base: {}},
	}
	if err := walker.walk(base, ""); err != nil {
		return nil, err
	}
	return walker.results, nil
}

// globLiteral resolves a pattern that contains no wildcard characters.
func globLiteral(pattern, cwd string, absolute bool, options GlobOptions, format func(string) string) ([]string, error) {
	full := pattern
	if !absolute {
		full = filepath.Join(cwd, full)
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, nil
	}
	if info.IsDir() && options.Include != "all" {
		return nil, nil
	}
	return []string{format(full)}, nil
}

// globWalker walks a directory tree collecting pattern matches.
type globWalker struct {
	slashPattern string
	absolute     bool
	options      GlobOptions
	literals     map[string]struct{}
	format       func(string) string
	results      []string
	visited      map[string]struct{}
}

func (w *globWalker) walk(dir, rel string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if err := w.visit(dir, rel, entry); err != nil {
			return err
		}
	}
	return nil
}

func (w *globWalker) visit(dir, rel string, entry os.DirEntry) error {
	name := entry.Name()
	full := filepath.Join(dir, name)
	childRel := name
	if rel != "" {
		childRel = rel + "/" + name
	}

	isDir, isSymlink, ok := w.entryKind(entry, full)
	if !ok {
		return nil
	}
	allowed := w.options.Dot || dotAllowed(childRel, w.literals)
	if allowed {
		matched, err := w.matches(childRel, full, isDir)
		if err != nil {
			return err
		}
		if matched {
			w.results = append(w.results, w.format(full))
		}
	}

	if !isDir || !allowed {
		return nil
	}
	if isSymlink && !w.options.Symlink {
		return nil
	}
	return w.descend(full, childRel, isSymlink)
}

// entryKind reports whether an entry is a traversable directory and whether it
// is a symlink. ok is false when a symlink's target cannot be read.
func (w *globWalker) entryKind(entry os.DirEntry, full string) (isDir, isSymlink, ok bool) {
	isDir = entry.IsDir()
	isSymlink = entry.Type()&os.ModeSymlink != 0
	if !isSymlink {
		return isDir, false, true
	}
	target, err := os.Stat(full)
	if err != nil {
		return false, true, false
	}
	// npm glob only treats a symlink as a directory when it follows it: with
	// follow:false a symlink is a leaf, so a link to a directory still matches a
	// nodir (include:"file") pattern.
	return target.IsDir() && w.options.Symlink, true, true
}

// matches reports whether the entry matches the pattern and is includable.
func (w *globWalker) matches(childRel, full string, isDir bool) (bool, error) {
	candidate := childRel
	if w.absolute {
		candidate = filepath.ToSlash(full)
	}
	matched, err := doublestar.Match(w.slashPattern, candidate)
	if err != nil {
		return false, err
	}
	return matched && (!isDir || w.options.Include == "all"), nil
}

// descend recurses into a directory unless its real path was already visited.
func (w *globWalker) descend(full, childRel string, isSymlink bool) error {
	real := full
	if isSymlink {
		if resolved, err := filepath.EvalSymlinks(full); err == nil {
			real = resolved
		}
	}
	if _, ok := w.visited[real]; ok {
		return nil
	}
	w.visited[real] = struct{}{}
	return w.walk(full, childRel)
}

// globLiteralBase returns the longest directory prefix of an absolute pattern
// that contains no wildcard characters.
func globLiteralBase(pattern string) string {
	volume := filepath.VolumeName(pattern)
	rest := pattern[len(volume):]
	segments := strings.Split(filepath.ToSlash(rest), "/")
	var base []string
	for _, segment := range segments {
		if strings.ContainsAny(segment, "*?[{") {
			break
		}
		base = append(base, segment)
	}
	joined := strings.Join(base, "/")
	if joined == "" {
		joined = "/"
	}
	return filepath.FromSlash(volume + joined)
}

func literalSegments(pattern string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, segment := range strings.Split(pattern, "/") {
		if segment != "" && !strings.ContainsAny(segment, "*?[{") {
			set[segment] = struct{}{}
		}
	}
	return set
}

func dotAllowed(candidate string, literals map[string]struct{}) bool {
	for _, segment := range strings.Split(candidate, "/") {
		if !strings.HasPrefix(segment, ".") || segment == "." || segment == ".." {
			continue
		}
		if _, ok := literals[segment]; !ok {
			return false
		}
	}
	return true
}

// expandBraces expands comma-separated brace alternatives, including nesting,
// mirroring the brace expansion npm glob performs before matching.
func expandBraces(pattern string) []string {
	start := strings.IndexByte(pattern, '{')
	if start == -1 {
		return []string{pattern}
	}
	depth := 0
	end := -1
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end != -1 {
			break
		}
	}
	if end == -1 {
		return []string{pattern}
	}

	prefix := pattern[:start]
	suffix := pattern[end+1:]
	var result []string
	for _, option := range splitBraceOptions(pattern[start+1 : end]) {
		result = append(result, expandBraces(prefix+option+suffix)...)
	}
	return result
}

func splitBraceOptions(content string) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, content[last:i])
				last = i + 1
			}
		}
	}
	return append(parts, content[last:])
}

// GlobMatch reports whether the path matches the pattern, with dot matching
// enabled.
func GlobMatch(pattern, filepath string) bool {
	ok, err := doublestar.Match(pattern, filepath)
	return err == nil && ok
}
