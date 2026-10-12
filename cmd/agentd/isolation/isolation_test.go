package isolation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNoneModeUsesWorkingDirectory(t *testing.T) {
	src := t.TempDir()
	project, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: src,
	}, IsolationSpec{Mode: ModeNone})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if project.View != src {
		t.Fatalf("view = %q, want %q", project.View, src)
	}
	if project.Teardown != nil {
		t.Fatal("none mode should not need teardown")
	}
}

func TestCopyModeIsolatesAndTearsDown(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "source")
	base := t.TempDir()

	project, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: src,
	}, IsolationSpec{Mode: ModeCopy, BaseDir: base})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	view := project.View
	if got := readFile(t, filepath.Join(view, "a.txt")); got != "source" {
		t.Fatalf("view content = %q", got)
	}

	// A write in the view must not reach the source.
	writeFile(t, filepath.Join(view, "b.txt"), "view-only")
	if _, err := os.Stat(filepath.Join(src, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("view write leaked into the source")
	}

	if err := project.Teardown(context.Background()); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatal("teardown did not remove the view")
	}
}

func TestCopyRequiresBaseDir(t *testing.T) {
	_, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: t.TempDir(),
	}, IsolationSpec{Mode: ModeCopy})
	if err == nil {
		t.Fatal("expected an error without a base directory")
	}
}

func TestOverlayRequiresWorkingDirectory(t *testing.T) {
	if _, err := Prepare(context.Background(), ProjectSpec{ProjectID: "x"}, IsolationSpec{Mode: ModeOverlay}); err == nil {
		t.Fatal("expected overlay without a working directory to be rejected")
	}
}

func TestContainerIsExternalMode(t *testing.T) {
	for _, mode := range []string{ModeContainer, ModeRemote} {
		_, err := Prepare(context.Background(), ProjectSpec{ProjectID: "x"}, IsolationSpec{Mode: mode})
		if !errors.Is(err, ErrExternalMode) {
			t.Fatalf("mode %s: got %v, want ErrExternalMode", mode, err)
		}
	}
}

func TestOverlayModeIsolatesAndTearsDown(t *testing.T) {
	if _, err := exec.LookPath("cp"); err != nil {
		t.Skip("cp is not available")
	}
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "source")
	base := t.TempDir()

	project, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: src,
	}, IsolationSpec{Mode: ModeOverlay, BaseDir: base, BaseReadOnly: true})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	view := project.View
	if got := readFile(t, filepath.Join(view, "a.txt")); got != "source" {
		t.Fatalf("view content = %q", got)
	}
	writeFile(t, filepath.Join(view, "b.txt"), "view-only")
	if _, err := os.Stat(filepath.Join(src, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("view write leaked into the source")
	}
	if err := project.Teardown(context.Background()); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatal("teardown did not remove the view")
	}
}

func TestOverlayRequiresReadOnlyBase(t *testing.T) {
	_, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: t.TempDir(),
	}, IsolationSpec{Mode: ModeOverlay, BaseDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected overlay without a read-only base to be rejected")
	}
}

func TestOverlayExplicitUnsupportedCowErrors(t *testing.T) {
	// Pick the mechanism that is NOT native to this platform.
	mode := CowReflink
	if runtime.GOOS == "linux" {
		mode = CowClonefile
	}
	_, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: t.TempDir(),
	}, IsolationSpec{Mode: ModeOverlay, BaseDir: t.TempDir(), BaseReadOnly: true, CowMode: mode})
	if err == nil {
		t.Fatalf("expected explicit unsupported cow mode %q to be rejected", mode)
	}
}

func TestWorktreeModeAtHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	src := t.TempDir()
	git(t, src, "init")
	git(t, src, "config", "user.email", "test@example.com")
	git(t, src, "config", "user.name", "Test")
	writeFile(t, filepath.Join(src, "a.txt"), "source")
	git(t, src, "add", "a.txt")
	git(t, src, "commit", "-m", "init")

	project, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: src,
	}, IsolationSpec{Mode: ModeWorktree, BaseDir: t.TempDir()})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := readFile(t, filepath.Join(project.View, "a.txt")); got != "source" {
		t.Fatalf("worktree content = %q", got)
	}
	if err := project.Teardown(context.Background()); err != nil {
		t.Fatalf("teardown: %v", err)
	}
}

func TestWorktreeBaseRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	src := t.TempDir()
	git(t, src, "init")
	git(t, src, "config", "user.email", "test@example.com")
	git(t, src, "config", "user.name", "Test")
	writeFile(t, filepath.Join(src, "a.txt"), "v1")
	git(t, src, "add", "a.txt")
	git(t, src, "commit", "-m", "first")
	first := gitOutput(t, src, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(src, "a.txt"), "v2")
	git(t, src, "commit", "-am", "second")

	project, err := Prepare(context.Background(), ProjectSpec{
		ProjectID: "grp", WorkingDirectory: src,
	}, IsolationSpec{Mode: ModeWorktree, BaseDir: t.TempDir(), BaseRef: first})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := readFile(t, filepath.Join(project.View, "a.txt")); got != "v1" {
		t.Fatalf("worktree base ref content = %q, want v1", got)
	}
	if err := project.Teardown(context.Background()); err != nil {
		t.Fatalf("teardown: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
