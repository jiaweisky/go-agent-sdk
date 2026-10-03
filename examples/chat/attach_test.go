package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAttachCommand guards that /attach queues an image as a base64 data URL and
// that takeAttachments clears it (sent once with the next message).
func TestAttachCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCommand(t, nil, func(c *chat) {
		handleCommand(c, "/attach "+path)
		handleCommand(c, "/attach")
		files := c.takeAttachments()
		if len(files) != 1 {
			t.Fatalf("files = %d, want 1", len(files))
		}
		if files[0].Mime != "image/png" || !strings.HasPrefix(files[0].URI, "data:image/png;base64,") {
			t.Fatalf("attachment = %+v", files[0])
		}
		if got := c.takeAttachments(); len(got) != 0 {
			t.Fatalf("attachments not cleared: %d", len(got))
		}
	})
	for _, want := range []string{"attached pic.png (image/png)", "Attachments (1):", "pic.png"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestAttachRejectsUnsupportedType guards the MIME check.
func TestAttachRejectsUnsupportedType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCommand(t, nil, func(c *chat) {
		handleCommand(c, "/attach "+path)
	})
	if !strings.Contains(out, "unsupported image type") {
		t.Fatalf("out = %q, want the unsupported-type error", out)
	}
}
