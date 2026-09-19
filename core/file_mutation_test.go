package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileMutationCreateReturnsTargetExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("sentinel"), 0644); err != nil {
		t.Fatal(err)
	}

	files := NewFileMutation()
	_, err := files.Create(FileMutationWriteInput{
		Target:  FileMutationTarget{Canonical: path, Resource: "existing.txt"},
		Content: []byte("replacement"),
	})
	var exists *FileMutationTargetExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("expected FileMutationTargetExistsError, got %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "sentinel" {
		t.Fatalf("target should be unchanged, got %q (%v)", got, err)
	}
}

func TestFileMutationCreateMakesParentDirectories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "new.txt")

	files := NewFileMutation()
	if _, err := files.Create(FileMutationWriteInput{
		Target:  FileMutationTarget{Canonical: path, Resource: "nested/new.txt"},
		Content: []byte("created"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "created" {
		t.Fatalf("unexpected content %q (%v)", got, err)
	}
}
