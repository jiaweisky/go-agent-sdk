package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupA2ASpool(t *testing.T) {
	data := t.TempDir()
	spool := A2ASpoolDir(data)
	oldDir := filepath.Join(spool, "turn-old")
	newDir := filepath.Join(spool, "turn-new")
	for _, dir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// Age the old turn's directory beyond the retention window.
	old := time.Now().Add(-A2ASpoolRetention - time.Hour)
	if err := os.Chtimes(oldDir, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := CleanupA2ASpool(data); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("the expired spool directory was not removed")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("a fresh spool directory must be kept: %v", err)
	}
}

func TestCleanupA2ASpoolMissingDir(t *testing.T) {
	if err := CleanupA2ASpool(t.TempDir()); err != nil {
		t.Fatalf("a missing spool directory must not error: %v", err)
	}
}
