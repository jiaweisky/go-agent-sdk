package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

func newSnapshotTest(t *testing.T) (*Snapshot, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = resolved
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
		t.Fatalf("location: %v", err)
	}
	global := NewGlobal(t.TempDir(), "/home")
	snapshot, err := NewSnapshot(git, global, location)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot, root
}

func TestSnapshotCaptureDiffRestore(t *testing.T) {
	snapshot, root := newSnapshotTest(t)

	from, err := snapshot.Capture()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if from == nil {
		t.Fatalf("expected snapshot id")
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	to, err := snapshot.Capture()
	if err != nil || to == nil {
		t.Fatalf("second capture: %v %v", to, err)
	}

	files, err := snapshot.Files(SnapshotCompareInput{From: *from, To: *to})
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	if len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("files = %v", files)
	}

	diff, err := snapshot.Diff(SnapshotDiffInput{SnapshotCompareInput: SnapshotCompareInput{From: *from, To: *to}})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(diff) != 1 || diff[0].Path != "a.txt" {
		t.Fatalf("diff = %+v", diff)
	}

	preview, err := snapshot.Preview(SnapshotPreviewInput{SnapshotRestoreInput: SnapshotRestoreInput{
		Files: map[schema.RelativePath]SnapshotID{"a.txt": *from},
	}})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview) != 1 || preview[0].Path != "a.txt" {
		t.Fatalf("preview = %+v", preview)
	}

	if err := snapshot.Restore(SnapshotRestoreInput{
		Files: map[schema.RelativePath]SnapshotID{"a.txt": *from},
	}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello" {
		t.Fatalf("restored content = %q", content)
	}
}

func TestSnapshotPlanRejectsEscapingPath(t *testing.T) {
	snapshot, _ := newSnapshotTest(t)
	err := snapshot.Restore(SnapshotRestoreInput{
		Files: map[schema.RelativePath]SnapshotID{"../outside.txt": "tree"},
	})
	if err == nil {
		t.Fatalf("expected path escape error")
	}
}
