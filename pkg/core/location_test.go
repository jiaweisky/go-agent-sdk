package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core/project"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLocation_ResolveGitProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	gitExec(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	gitExec(t, root, "add", ".")
	gitExec(t, root, "commit", "-m", "init")

	git := NewGit(NewAppProcess())
	proj := NewProject(ProjectConfig{Git: git})

	location, err := NewLocation(schema.LocationRef{Directory: schema.AbsolutePath(root)}, proj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if location.Project.ID == "" {
		t.Fatalf("expected non-empty project id")
	}
	if location.Project.ID == schema.ProjectIDGlobal {
		t.Fatalf("expected git-derived project id, got global")
	}
	if location.Vcs == nil || location.Vcs.Type != project.VcsTypeGit {
		t.Fatalf("expected git vcs, got %+v", location.Vcs)
	}
	if string(location.Directory) == "" {
		t.Fatalf("expected directory")
	}
}

func TestLocation_NonGitDirectory(t *testing.T) {
	root := t.TempDir()

	git := NewGit(NewAppProcess())
	proj := NewProject(ProjectConfig{Git: git})

	location, err := NewLocation(schema.LocationRef{Directory: schema.AbsolutePath(root)}, proj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if location.Project.ID != schema.ProjectIDGlobal {
		t.Fatalf("expected global project id, got %s", location.Project.ID)
	}
	if location.Vcs != nil {
		t.Fatalf("expected no vcs, got %+v", location.Vcs)
	}
}

func TestGit_Discover(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	gitExec(t, root, "init")
	sub := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	git := NewGit(NewAppProcess())
	repo, err := git.Repo.Discover(AbsolutePath(sub))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if repo == nil {
		t.Fatalf("expected repository")
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if string(repo.Worktree) != resolvedRoot {
		t.Fatalf("expected worktree %s, got %s", resolvedRoot, repo.Worktree)
	}
}
