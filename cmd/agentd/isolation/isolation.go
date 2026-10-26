// Package isolation prepares per-project working directories for agentd.
//
// A Client is bound to one working directory, so serving several projects from
// one agentd requires giving each server its own view of the source project.
// This package materializes that view (a no-op, a copy, a copy-on-write overlay,
// or a git worktree) and returns how to tear it down. Container/remote
// provisioning is out of scope: those run in a separate process and are driven by
// the host.
package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Isolation modes.
const (
	// ModeNone shares the working directory directly.
	ModeNone = "none"
	// ModeCopy copies the source working directory into a per-project view.
	ModeCopy = "copy"
	// ModeWorktree creates a git worktree as the per-project view.
	ModeWorktree = "worktree"
	// ModeOverlay creates a copy-on-write overlay over the read-only source.
	ModeOverlay = "overlay"
	// ModeContainer runs the instance in a container; provisioned by the host.
	ModeContainer = "container"
	// ModeRemote runs the instance on a remote host; provisioned by the host.
	ModeRemote = "remote"
)

// Copy-on-write mechanisms for overlay mode.
const (
	// CowAuto picks the platform default: reflink on Linux, clonefile on macOS.
	CowAuto = "auto"
	// CowReflink uses a filesystem reflink copy (Linux).
	CowReflink = "reflink"
	// CowClonefile uses APFS clonefile (macOS).
	CowClonefile = "clonefile"
)

// ErrExternalMode reports a mode that runs in a separate process (a container,
// VM or remote host). Such an instance is provisioned by the host supervisor,
// not by the local runner.
var ErrExternalMode = errors.New("isolation mode is provisioned by the host, not the local runner")

// ProjectSpec is the source project an instance should serve.
type ProjectSpec struct {
	// ProjectID is a stable, unique identifier (the server name).
	ProjectID string
	// Root is the SDK state directory; it is never shared between instances.
	Root string
	// WorkingDirectory is the source project to derive a view from.
	WorkingDirectory string
	// ConfigDirs are the agent/skill directories to load.
	ConfigDirs []string
}

// IsolationSpec describes how to materialize the view.
type IsolationSpec struct {
	// Mode is one of the Mode* constants; empty means ModeNone.
	Mode string
	// BaseDir is the parent directory for created views (required by every mode
	// but none).
	BaseDir string
	// BaseRef is the commit-ish a worktree is created at; empty means HEAD.
	BaseRef string
	// CowMode selects the overlay copy-on-write mechanism (auto | reflink |
	// clonefile); empty means auto.
	CowMode string
}

// IsolatedProject is a prepared view plus its teardown.
type IsolatedProject struct {
	Spec ProjectSpec
	// View is the working directory the serving Client must use.
	View string
	// Teardown releases the view; nil when there is nothing to release.
	Teardown func(context.Context) error
}

// Runner prepares isolated views. Kept as an interface so a host may substitute
// its own (for example, delegating to an orchestrator).
type Runner interface {
	Prepare(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error)
}

// DefaultRunner is the built-in runner for local modes.
func DefaultRunner() Runner { return localRunner{} }

// Prepare is a convenience wrapper around DefaultRunner.
func Prepare(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	return DefaultRunner().Prepare(ctx, p, iso)
}

// localRunner materializes views on the local filesystem.
type localRunner struct{}

func (localRunner) Prepare(
	ctx context.Context, p ProjectSpec, iso IsolationSpec,
) (IsolatedProject, error) {
	switch iso.Mode {
	case "", ModeNone:
		return IsolatedProject{Spec: p, View: p.WorkingDirectory}, nil
	case ModeCopy:
		return prepareCopy(ctx, p, iso)
	case ModeWorktree:
		return prepareWorktree(ctx, p, iso)
	case ModeOverlay:
		return prepareOverlay(ctx, p, iso)
	case ModeContainer, ModeRemote:
		return IsolatedProject{}, fmt.Errorf("%w: %s", ErrExternalMode, iso.Mode)
	default:
		return IsolatedProject{}, fmt.Errorf("unknown isolation mode %q", iso.Mode)
	}
}

// prepareCopy copies the source into a new per-project view.
func prepareCopy(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	if p.WorkingDirectory == "" {
		return IsolatedProject{}, errors.New("isolation mode copy requires a working directory")
	}

	view, err := viewPath(p, iso)
	if err != nil {
		return IsolatedProject{}, err
	}
	if pathWithin(p.WorkingDirectory, view) {
		return IsolatedProject{}, fmt.Errorf("copy view %s must be outside the working directory %s", view, p.WorkingDirectory)
	}
	if err := ensureViewAbsent(view); err != nil {
		return IsolatedProject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(view), 0o755); err != nil {
		return IsolatedProject{}, err
	}

	if err := ctx.Err(); err != nil {
		return IsolatedProject{}, err
	}
	if err := os.CopyFS(view, os.DirFS(p.WorkingDirectory)); err != nil {
		_ = os.RemoveAll(view)
		return IsolatedProject{}, fmt.Errorf("copy project: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = os.RemoveAll(view)
		return IsolatedProject{}, err
	}

	return IsolatedProject{
		Spec: p,
		View: view,
		Teardown: func(context.Context) error {
			return os.RemoveAll(view)
		},
	}, nil
}

// prepareWorktree creates a detached git worktree as the per-project view.
func prepareWorktree(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	if p.WorkingDirectory == "" {
		return IsolatedProject{}, errors.New("isolation mode worktree requires a working directory")
	}

	view, err := viewPath(p, iso)
	if err != nil {
		return IsolatedProject{}, err
	}
	if err := ensureViewAbsent(view); err != nil {
		return IsolatedProject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(view), 0o755); err != nil {
		return IsolatedProject{}, err
	}

	// Detached worktree at the requested base ref, or the current HEAD.
	args := []string{"-C", p.WorkingDirectory, "worktree", "add", "--detach", view}
	if ref := strings.TrimSpace(iso.BaseRef); ref != "" {
		args = append(args, ref)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(view)
		return IsolatedProject{}, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return IsolatedProject{
		Spec: p,
		View: view,
		Teardown: func(ctx context.Context) error {
			remove := exec.CommandContext(ctx, "git", "-C", p.WorkingDirectory, "worktree", "remove", "--force", view)
			if out, err := remove.CombinedOutput(); err != nil {
				// Fall back to a plain delete so a stale worktree cannot wedge
				// shutdown.
				_ = os.RemoveAll(view)
				return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}, nil
}

// prepareOverlay materializes a copy-on-write view of the source project. It
// shares on-disk blocks with the source until the view is written, costing far
// less than a full copy while keeping the two independent.
func prepareOverlay(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	if p.WorkingDirectory == "" {
		return IsolatedProject{}, errors.New("isolation mode overlay requires a working directory")
	}

	view, err := viewPath(p, iso)
	if err != nil {
		return IsolatedProject{}, err
	}
	if pathWithin(p.WorkingDirectory, view) {
		return IsolatedProject{}, fmt.Errorf("overlay view %s must be outside the working directory %s", view, p.WorkingDirectory)
	}
	if err := ensureViewAbsent(view); err != nil {
		return IsolatedProject{}, err
	}
	if err := os.MkdirAll(view, 0o755); err != nil {
		return IsolatedProject{}, err
	}

	if err := cowCopy(ctx, p.WorkingDirectory, view, iso.CowMode); err != nil {
		_ = os.RemoveAll(view)
		return IsolatedProject{}, fmt.Errorf("create copy-on-write view: %w", err)
	}

	return IsolatedProject{
		Spec: p,
		View: view,
		Teardown: func(context.Context) error {
			return os.RemoveAll(view)
		},
	}, nil
}

// cowCopy copies src into the existing directory dst using the best available
// copy-on-write mechanism for the platform. It falls back to a plain copy when
// no CoW tool exists and the mode was not requested explicitly.
func cowCopy(ctx context.Context, src, dst, mode string) error {
	explicit := mode != "" && mode != CowAuto
	if !explicit {
		mode = defaultCowMode()
	}

	flags, available := cowFlags(mode)
	if !available {
		if explicit {
			return fmt.Errorf("copy-on-write mode %q is not available on %s", mode, runtime.GOOS)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
			return err
		}
		return ctx.Err()
	}

	// `cp <flags> src/. dst` copies the contents of src into the existing dst.
	// Note: build the trailing "/." by hand; filepath.Join would clean it away.
	contents := src + string(filepath.Separator) + "."
	args := append(flags, contents, dst)
	cmd := exec.CommandContext(ctx, "cp", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp (%s): %w: %s", mode, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultCowMode is the platform's preferred copy-on-write mechanism, or "" when
// the platform has none.
func defaultCowMode() string {
	switch runtime.GOOS {
	case "darwin":
		return CowClonefile
	case "linux":
		return CowReflink
	default:
		return ""
	}
}

// cowFlags returns the `cp` flags for a mechanism and whether it is available on
// this platform.
func cowFlags(mode string) ([]string, bool) {
	switch mode {
	case CowClonefile:
		return []string{"-c", "-a"}, runtime.GOOS == "darwin"
	case CowReflink:
		return []string{"-a", "--reflink=auto"}, runtime.GOOS == "linux"
	default:
		return nil, false
	}
}

// ensureViewAbsent fails when the view path already exists, so a stale view is
// never silently reused.
func ensureViewAbsent(view string) error {
	if _, err := os.Stat(view); err == nil {
		return fmt.Errorf("view already exists: %s", view)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SanitizeComponent makes an identifier safe to use as a single path element.
func SanitizeComponent(value string) string {
	replaced := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_").Replace(value)
	return strings.Trim(replaced, " .")
}

// viewPath is the absolute view directory for a project and isolation spec.
func viewPath(p ProjectSpec, iso IsolationSpec) (string, error) {
	if iso.BaseDir == "" {
		return "", fmt.Errorf("isolation mode %s requires a base directory", iso.Mode)
	}

	id := strings.TrimSpace(p.ProjectID)
	if id == "" {
		return "", errors.New("isolation requires a project id")
	}
	component := SanitizeComponent(id)
	if component == "" {
		return "", fmt.Errorf("isolation project id %q is not usable as a path element", p.ProjectID)
	}

	base, err := filepath.Abs(iso.BaseDir)
	if err != nil {
		return "", fmt.Errorf("resolve isolation base directory: %w", err)
	}
	return filepath.Join(base, component), nil
}

// pathWithin reports whether path is root or lies inside it. Symlinks and
// case-insensitive filesystem aliases are resolved by comparing the existing
// ancestor chain, not just the lexical paths.
func pathWithin(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	canonRoot := canonicalPath(absRoot)
	canonPath := canonicalPath(absPath)
	if lexicalWithin(canonRoot, canonPath) {
		return true
	}

	rootInfo, err := os.Stat(canonRoot)
	if err != nil {
		return false
	}
	for p := canonPath; ; p = filepath.Dir(p) {
		if info, err := os.Stat(p); err == nil && os.SameFile(rootInfo, info) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
	}
}

// canonicalPath resolves the existing part of path, keeping any missing suffix.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

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

// lexicalWithin reports whether path is root or lies inside it without
// resolving filesystem aliases.
func lexicalWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
