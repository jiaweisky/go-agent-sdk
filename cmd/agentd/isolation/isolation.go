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

// Transport is the wire protocol an instance serves. It is recorded here so a
// prepared project can be associated with the adapter that will serve it.
type Transport string

const (
	// TransportHTTP serves the HTTP/SSE API.
	TransportHTTP Transport = "http"
	// TransportA2A serves the A2A JSON-RPC API.
	TransportA2A Transport = "a2a"
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

// ErrExternalMode reports a mode that runs in a separate process (a container,
// VM or remote host). Such an instance is provisioned by the host supervisor,
// not by the local runner.
var ErrExternalMode = errors.New("isolation mode is provisioned by the host, not the local runner")

// Copy-on-write mechanisms for overlay mode.
const (
	// CowAuto picks the platform default: reflink on Linux, clonefile on macOS.
	CowAuto = "auto"
	// CowReflink uses a filesystem reflink copy (Linux).
	CowReflink = "reflink"
	// CowClonefile uses APFS clonefile (macOS).
	CowClonefile = "clonefile"
)

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
	// BaseReadOnly requests that the source be treated read-only. Required by
	// overlay and ignored by the other modes.
	BaseReadOnly bool
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

type localRunner struct{}

func (localRunner) Prepare(
	ctx context.Context, p ProjectSpec, iso IsolationSpec,
) (IsolatedProject, error) {
	switch iso.Mode {
	case "", ModeNone:
		return IsolatedProject{Spec: p, View: p.WorkingDirectory}, nil
	case ModeCopy:
		return prepareCopy(p, iso)
	case ModeWorktree:
		return prepareWorktree(ctx, p, iso)
	case ModeOverlay:
		return prepareOverlay(p, iso)
	case ModeContainer, ModeRemote:
		return IsolatedProject{}, fmt.Errorf("%w: %s", ErrExternalMode, iso.Mode)
	default:
		return IsolatedProject{}, fmt.Errorf("unknown isolation mode %q", iso.Mode)
	}
}

func prepareCopy(p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	if p.WorkingDirectory == "" {
		return IsolatedProject{}, errors.New("isolation mode copy requires a working directory")
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
	if err := os.CopyFS(view, os.DirFS(p.WorkingDirectory)); err != nil {
		_ = os.RemoveAll(view)
		return IsolatedProject{}, fmt.Errorf("copy project: %w", err)
	}
	return IsolatedProject{
		Spec: p,
		View: view,
		Teardown: func(context.Context) error {
			return os.RemoveAll(view)
		},
	}, nil
}

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
		Teardown: func(context.Context) error {
			remove := exec.Command("git", "-C", p.WorkingDirectory, "worktree", "remove", "--force", view)
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
func prepareOverlay(p ProjectSpec, iso IsolationSpec) (IsolatedProject, error) {
	if p.WorkingDirectory == "" {
		return IsolatedProject{}, errors.New("isolation mode overlay requires a working directory")
	}
	if !iso.BaseReadOnly {
		return IsolatedProject{}, errors.New("isolation mode overlay requires a read-only base")
	}
	view, err := viewPath(p, iso)
	if err != nil {
		return IsolatedProject{}, err
	}
	if err := ensureViewAbsent(view); err != nil {
		return IsolatedProject{}, err
	}
	if err := os.MkdirAll(view, 0o755); err != nil {
		return IsolatedProject{}, err
	}
	if err := cowCopy(p.WorkingDirectory, view, iso.CowMode); err != nil {
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
func cowCopy(src, dst, mode string) error {
	explicit := mode != "" && mode != CowAuto
	if !explicit {
		mode = defaultCowMode()
	}
	flags, available := cowFlags(mode)
	if !available {
		if explicit {
			return fmt.Errorf("copy-on-write mode %q is not available on %s", mode, runtime.GOOS)
		}
		return os.CopyFS(dst, os.DirFS(src))
	}
	// `cp <flags> src/. dst` copies the contents of src into the existing dst.
	// Note: build the trailing "/." by hand; filepath.Join would clean it away.
	contents := src + string(filepath.Separator) + "."
	args := append(flags, contents, dst)
	cmd := exec.Command("cp", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp (%s): %w: %s", mode, err, strings.TrimSpace(string(out)))
	}
	return nil
}

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
	return filepath.Join(iso.BaseDir, component), nil
}

// SanitizeComponent makes an identifier safe to use as a single path element.
func SanitizeComponent(value string) string {
	replaced := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(value)
	return strings.Trim(replaced, " .")
}
