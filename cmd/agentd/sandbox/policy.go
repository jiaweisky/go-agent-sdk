package sandbox

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandboxmode"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// Filesystem modes.
const (
	// FSModeAllow confines access to an allow-list (the secure default).
	FSModeAllow = sandboxmode.FSModeAllow
	// FSModeOff disables confinement for that dimension.
	FSModeOff = sandboxmode.FSModeOff
)

// Network modes.
const (
	// NetworkOff disables network access for sandboxed commands.
	NetworkOff = sandboxmode.NetworkOff
	// NetworkNoEgress allows local traffic but blocks outbound connections.
	NetworkNoEgress = sandboxmode.NetworkNoEgress
)

// UNIX-socket modes.
const (
	// SocketInherit follows the filesystem policy.
	SocketInherit = sandboxmode.SocketInherit
	// SocketAllowAll disables the socket restriction.
	SocketAllowAll = sandboxmode.SocketAllowAll
	// SocketAllowlist explicitly allows the listed socket paths.
	SocketAllowlist = sandboxmode.SocketAllowlist
	// SocketDeny is the pre-rename name for SocketInherit.
	SocketDeny = sandboxmode.SocketDeny
)

// Environment modes.
const (
	// EnvScrub removes the configured environment-name globs before a command
	// starts.
	EnvScrub = sandboxmode.EnvScrub
	// EnvInherit passes the environment through unchanged.
	EnvInherit = sandboxmode.EnvInherit
)

// Violation-reporting modes.
const (
	// ViolationHeuristic scans a command's stderr for OS denial messages.
	ViolationHeuristic = sandboxmode.ViolationHeuristic
	// ViolationPrecise parses syscall-level denials (Linux, via strace).
	ViolationPrecise = sandboxmode.ViolationPrecise
	// ViolationOff disables command-side reporting.
	ViolationOff = sandboxmode.ViolationOff
)

// Policy is a resolved sandbox policy shared by the per-command wrapper, the
// whole-process wrapper and the in-process file guard.
type Policy struct {
	// WorkingDirectory is the writable project root (the prepared view).
	WorkingDirectory string
	ReadMode         string
	WriteMode        string
	DenyRead         []string
	AllowRead        []string
	DenyWrite        []string
	AllowWrite       []string
	NetworkMode      string
	SocketMode       string
	SocketAllow      []string
	// TempRoot is the base directory for a per-run private temp. agentd sets it
	// under the server's state root; the adapter creates a run subdirectory.
	TempRoot string
	// PrivateTemp is the per-run temp directory. The adapter sets it and adds it
	// to the read/write roots, so the shared host temp is not exposed.
	PrivateTemp     string
	DenyProcessInfo bool
	// ScrubEnv lists environment-name globs removed before a command starts.
	// Empty disables scrubbing.
	ScrubEnv []string
	// ViolationMode is off, heuristic or precise.
	ViolationMode string
	// ReportToModel appends a short denial note to a command's output.
	ReportToModel bool
}

// StandardPolicy is the secure default when a sandbox is enabled: allow-list
// reads and writes, no process introspection, a scrubbed environment, no egress
// for sandboxed commands, and denied UNIX sockets.
func StandardPolicy(workingDir string) Policy {
	return Policy{
		WorkingDirectory: workingDir,
		ReadMode:         FSModeAllow,
		WriteMode:        FSModeAllow,
		NetworkMode:      NetworkNoEgress,
		SocketMode:       SocketInherit,
		DenyProcessInfo:  true,
		ScrubEnv:         DefaultScrubEnv(),
		ViolationMode:    ViolationHeuristic,
		ReportToModel:    true,
	}
}

// DefaultScrubEnv returns the environment-name globs scrubbed by default.
func DefaultScrubEnv() []string {
	return []string{"*_KEY", "*_SECRET", "*_TOKEN", "*_PASSWORD"}
}

// WriteRoots is the set of paths writable when WriteMode is allow.
func (p Policy) WriteRoots() []string {
	roots := append([]string{p.WorkingDirectory, p.tempPath()}, p.AllowWrite...)
	return dedupe(roots)
}

// tempPath is the run's temp directory: the per-run private one when set,
// otherwise the host temp.
func (p Policy) tempPath() string {
	if p.PrivateTemp != "" {
		return p.PrivateTemp
	}

	return os.TempDir()
}

// legacyTempReadRoots keeps /tmp readable when no private temp is configured.
func legacyTempReadRoots() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/private/tmp"}
	}

	return nil
}

// ReallowReadRoots is the set of paths re-allowed inside a denied read region,
// such as a project that lives under the user's home directory.
func (p Policy) ReallowReadRoots() []string {
	roots := append([]string{p.WorkingDirectory}, p.AllowRead...)
	if p.PrivateTemp != "" {
		// The per-run temp may live under a denied region (e.g. the state root
		// under the user's home), so re-allow it explicitly.
		roots = append(roots, p.PrivateTemp)
	}

	return dedupe(roots)
}

// DenyReadRegions are the broad regions reads are denied in by default: the
// user's home (and its parent), /etc and the root account. A path under one of
// them is readable only when it is re-allowed by ReallowReadRoots.
func (p Policy) DenyReadRegions() []string {
	home, _ := os.UserHomeDir()
	return denyReadRegions(home)
}

// denyReadRegions is DenyReadRegions with an explicit home directory, so the
// parent logic is testable. A home directly under / (e.g. /root) must not turn
// the parent into a deny-everything rule.
func denyReadRegions(home string) []string {
	regions := []string{"/etc", "/private/etc", "/var/root", "/root", "/home"}

	if home != "" {
		regions = append(regions, home)
		if parent := filepath.Dir(home); parent != home && parent != "." && parent != string(filepath.Separator) {
			regions = append(regions, parent)
		}
	}

	return dedupe(regions)
}

// SocketPaths are the explicitly allowed UNIX-socket paths.
func (p Policy) SocketPaths() []string {
	if p.SocketMode != SocketAllowlist {
		return nil
	}

	return dedupe(p.SocketAllow)
}

// DeniedRead / DeniedWrite return the deny entries expanded for matching.
func (p Policy) DeniedRead() []string  { return expandEntries(p.DenyRead, p.WorkingDirectory) }
func (p Policy) DeniedWrite() []string { return expandEntries(p.DenyWrite, p.WorkingDirectory) }

// platformReadRoots are the system paths a sandboxed command needs to run.
func platformReadRoots() []string {
	switch runtime.GOOS {
	case "windows":
		// AppContainer grants read access to the run directory and explicitly
		// granted paths; other system locations are not writable.
		return nil
	case "darwin":
		return []string{"/usr", "/System", "/Library", "/dev", "/bin", "/sbin"}
	default:
		return []string{"/usr", "/lib", "/lib64", "/bin", "/sbin", "/dev", "/proc", "/sys"}
	}
}

func dedupe(paths []string) []string {
	var out []string

	for _, path := range paths {
		if path == "" {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		path = filepath.Clean(path)
		if !contains(out, path) {
			out = append(out, path)
		}
	}

	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

// expandEntries expands `~` and resolves relative paths against the working
// directory. A relative glob is prefixed with `**/` so it matches at any depth.
func expandEntries(entries []string, workingDir string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, expandEntry(entry, workingDir))
	}

	return out
}

func expandEntry(entry, workingDir string) string {
	if entry == "" {
		return ""
	}

	if strings.HasPrefix(entry, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			entry = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(entry, "~"), string(filepath.Separator)))
		}
	}

	if filepath.IsAbs(entry) {
		return filepath.Clean(entry)
	}

	if strings.ContainsAny(entry, "*?[") {
		if !strings.HasPrefix(entry, "**") {
			return "**/" + filepath.ToSlash(entry)
		}

		return filepath.ToSlash(entry)
	}

	return filepath.Join(workingDir, entry)
}

// PathGuard enforces the policy for the in-process file tools (read, write,
// edit, apply_patch). It implements both core.FileReadGuard and
// core.FileWriteGuard.
type PathGuard struct {
	policy Policy
}

// NewPathGuard builds an in-process guard from a policy.
func NewPathGuard(policy Policy) PathGuard {
	return PathGuard{policy: policy}
}

var (
	_ core.FileReadGuard  = PathGuard{}
	_ core.FileWriteGuard = PathGuard{}
)

// AllowRead reports an error when the path falls in a denied read region and is
// not re-allowed. Reads outside those regions (the platform's run-time paths)
// are allowed, matching the OS profile.
func (g PathGuard) AllowRead(_ context.Context, path string) error {
	if g.policy.ReadMode != FSModeAllow {
		return nil
	}

	canonical := canonicalPath(path)
	if rule, ok := matchingDeny(canonical, g.policy.DeniedRead()); ok {
		return denied("read", path, rule)
	}

	if rule, ok := matchingDeny(canonical, g.policy.DenyReadRegions()); ok {
		roots := append(g.policy.ReallowReadRoots(), g.policy.SocketPaths()...)
		for _, root := range roots {
			if under(canonical, canonicalPath(root)) {
				return nil
			}
		}

		return denied("read", path, rule)
	}

	return nil
}

// Allow reports an error when a file mutation is outside the write allow-list.
func (g PathGuard) Allow(_ context.Context, path string, _ core.FileOp) error {
	if g.policy.WriteMode != FSModeAllow {
		return nil
	}

	canonical := canonicalPath(path)
	if rule, ok := matchingDeny(canonical, g.policy.DeniedWrite()); ok {
		return denied("write", path, rule)
	}

	roots := append(g.policy.WriteRoots(), g.policy.SocketPaths()...)
	for _, root := range roots {
		if under(canonical, canonicalPath(root)) {
			return nil
		}
	}

	return denied("write", path, "allowList")
}

// denied builds the typed error a guard returns. It carries the structured
// violation the SDK publishes as a session event.
func denied(action, path, rule string) error {
	return &core.PolicyDeniedError{Violation: core.PolicyViolation{
		Action: action, Path: path, Rule: rule, Surface: "tool", Certainty: "enforced",
	}}
}

// canonicalPath resolves a path as far as it exists, so symlinks do not escape
// the policy. A not-yet-existing path keeps its cleaned absolute form.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

	// Resolve the deepest existing ancestor, then append the remainder.
	dir, rest := abs, ""
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}

		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
	}
}

// under reports whether path is inside root (or equals it).
func under(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// matchingDeny returns the deny entry that matches path. A literal entry is a
// subpath; an entry with glob metacharacters is a slash-separated glob.
func matchingDeny(path string, deny []string) (string, bool) {
	for _, pattern := range deny {
		if pattern == "" {
			continue
		}

		if strings.ContainsAny(pattern, "*?[") {
			if matchGlob(pattern, path) {
				return pattern, true
			}
			continue
		}

		if under(path, canonicalPath(pattern)) {
			return pattern, true
		}
	}

	return "", false
}

var globCache sync.Map // map[string]*regexp.Regexp

// matchGlob matches a slash-separated path against a glob supporting `**`.
func matchGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	if cached, ok := globCache.Load(pattern); ok {
		return cached.(*regexp.Regexp).MatchString(path)
	}

	expression := regexp.MustCompile(globRegex(pattern))
	globCache.Store(pattern, expression)

	return expression.MatchString(path)
}

// globRegex converts a glob to an anchored regular expression. `**` matches any
// characters (including separators), `*` and `?` do not cross a separator.
func globRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}

	b.WriteString("$")

	return b.String()
}

// scrubbedProcess removes secret environment variables before delegating to the
// wrapped process. It applies uniformly to every sandbox wrapper.
type scrubbedProcess struct {
	inner    core.AppProcess
	patterns []string
}

func (s scrubbedProcess) Run(
	ctx context.Context, command string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	if len(options) > 0 {
		options[0].Env, options[0].ExtendEnv = scrubEnv(options[0].Env, options[0].ExtendEnv, s.patterns)
	}

	return s.inner.Run(ctx, command, args, options...)
}

func (s scrubbedProcess) RunStream(
	ctx context.Context, command string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	if len(options) > 0 {
		options[0].Env, options[0].ExtendEnv = scrubEnv(options[0].Env, options[0].ExtendEnv, s.patterns)
	}

	return s.inner.RunStream(ctx, command, args, options...)
}

// scrubEnv returns the effective child environment with matching names removed.
// The returned map replaces inheritance (extend=false) so nothing leaks back in.
func scrubEnv(env map[string]string, extend bool, patterns []string) (map[string]string, bool) {
	filtered := map[string]string{}
	if extend || env == nil {
		for _, pair := range os.Environ() {
			key, value, ok := strings.Cut(pair, "=")
			if ok && !scrubName(key, patterns) {
				filtered[key] = value
			}
		}
	}

	for key, value := range env {
		if !scrubName(key, patterns) {
			filtered[key] = value
		}
	}

	return filtered, false
}

func scrubName(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchName(pattern, name) {
			return true
		}
	}

	return false
}

func matchName(pattern, name string) bool {
	if runtime.GOOS == "windows" {
		pattern, name = strings.ToUpper(pattern), strings.ToUpper(name)
	}

	matched, err := filepath.Match(pattern, name)

	return err == nil && matched
}
