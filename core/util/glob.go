package util

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar"
)

// GlobOptions mirrors Glob.Options from util/glob.ts.
type GlobOptions struct {
	Cwd      string
	Absolute bool
	Include  string // "file" | "all"
	Dot      bool
	Symlink  bool
}

// GlobScan mirrors Glob.scan/scanSync: brace alternatives are expanded, then
// each pattern is matched by walking the filesystem. Dot and Symlink follow the
// npm glob options: when Dot is false, wildcard segments do not match entries
// beginning with "." (literal dot segments still match); when Symlink is false,
// symlinked directories are not traversed.
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

	base := cwd
	if absolute {
		base = globLiteralBase(pattern)
	}
	literals := literalSegments(slashPattern)

	var results []string
	visited := map[string]struct{}{}
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			full := filepath.Join(dir, name)
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}

			isDir := entry.IsDir()
			isSymlink := entry.Type()&os.ModeSymlink != 0
			if isSymlink {
				target, statErr := os.Stat(full)
				if statErr != nil {
					continue
				}
				// npm glob only treats a symlink as a directory when it follows
				// it: with follow:false a symlink is a leaf, so a link to a
				// directory still matches a nodir (include:"file") pattern.
				isDir = target.IsDir() && options.Symlink
			}

			candidate := childRel
			if absolute {
				candidate = filepath.ToSlash(full)
			}
			allowed := options.Dot || dotAllowed(childRel, literals)

			if allowed {
				matched, matchErr := doublestar.Match(slashPattern, candidate)
				if matchErr != nil {
					return matchErr
				}
				if matched && (!isDir || options.Include == "all") {
					results = append(results, format(full))
				}
			}

			if !isDir || !allowed {
				continue
			}
			if isSymlink && !options.Symlink {
				continue
			}
			real := full
			if isSymlink {
				if resolved, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
					real = resolved
				}
			}
			if _, ok := visited[real]; ok {
				continue
			}
			visited[real] = struct{}{}
			if err := walk(full, childRel); err != nil {
				return err
			}
		}
		return nil
	}

	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	visited[base] = struct{}{}
	if err := walk(base, ""); err != nil {
		return nil, err
	}
	return results, nil
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

// GlobMatch mirrors Glob.match (minimatch with dot:true).
func GlobMatch(pattern, filepath string) bool {
	ok, err := doublestar.Match(pattern, filepath)
	return err == nil && ok
}
