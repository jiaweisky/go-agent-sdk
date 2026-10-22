package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaPolicyValidate(t *testing.T) {
	valid := []MediaPolicy{
		{},
		{Prefer: []MediaPolicySource{MediaPolicySourceFile, MediaPolicySourceInline}, InlineLimitBytes: MediaSizeLimits{Default: 1024}, MaxBytes: MediaSizeLimits{Default: 2048}},
		{Prefer: []MediaPolicySource{MediaPolicySourceURL, MediaPolicySourceFile, MediaPolicySourceInline}},
		{Spool: &MediaSpoolPolicy{Dir: "/tmp/spool"}, Upload: true, Downsample: true},
	}
	for _, policy := range valid {
		if err := policy.Validate(); err != nil {
			t.Fatalf("valid policy rejected: %v (%+v)", err, policy)
		}
	}

	invalid := []MediaPolicy{
		{Prefer: []MediaPolicySource{"bogus"}},
		{InlineLimitBytes: MediaSizeLimits{Default: -1}},
		{MaxBytes: MediaSizeLimits{Default: -1}},
		{Spool: &MediaSpoolPolicy{}},
	}
	for _, policy := range invalid {
		if err := policy.Validate(); err == nil {
			t.Fatalf("invalid policy accepted: %+v", policy)
		}
	}
}

func TestMediaPolicyPreferredSources(t *testing.T) {
	got := MediaPolicy{}.PreferredSources()
	want := []MediaPolicySource{MediaPolicySourceFile, MediaPolicySourceInline}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("default prefer = %v, want %v", got, want)
	}
	explicit := MediaPolicy{Prefer: []MediaPolicySource{MediaPolicySourceInline}}
	if got := explicit.PreferredSources(); len(got) != 1 || got[0] != MediaPolicySourceInline {
		t.Fatalf("explicit prefer = %v", got)
	}
}

func TestCleanupSpoolDirRemovesExpired(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	fresh := filepath.Join(dir, "fresh")
	if err := os.WriteFile(old, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-A2ASpoolRetention - time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSpoolDir(dir); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired spool file should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh spool file should remain: %v", err)
	}
	// A missing directory is not an error.
	if err := CleanupSpoolDir(filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	if err := CleanupSpoolDir(""); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
}
