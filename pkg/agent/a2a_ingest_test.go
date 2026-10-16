package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// stubStore is an in-memory schema.MediaStore for the inbound-resolution test.
type stubStore struct{ data map[string][]byte }

func (s *stubStore) Put(context.Context, schema.MediaUpload) (schema.Reference, error) {
	return schema.Reference{}, nil
}

func (s *stubStore) Get(_ context.Context, ref schema.Reference) (io.ReadCloser, error) {
	b, ok := s.data[ref.Value]
	if !ok {
		return nil, errors.New("not this store's reference")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *stubStore) Target() schema.UploadTarget { return schema.UploadTarget{} }

func (s *stubStore) Reserve(context.Context, string, string, int64) (schema.UploadTarget, error) {
	return schema.UploadTarget{}, nil
}

// TestMaterializeStoredURI: a FileWithUri that names our own store is read back
// locally and exposed as a path (spool-equivalent).
func TestMaterializeStoredURI(t *testing.T) {
	store := &stubStore{data: map[string][]byte{"https://store/x": []byte("hi")}}
	dir := t.TempDir()
	att, note, err := materializeA2AFile(context.Background(), dir, false, store, server.File{URI: "https://store/x", Name: "a.txt", MIME: "text/plain"})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if att != nil || note == "" {
		t.Fatalf("att=%+v note=%q", att, note)
	}
	if b, err := os.ReadFile(note); err != nil || string(b) != "hi" {
		t.Fatalf("materialized file = %q err=%v", b, err)
	}
}

// TestMaterializeForeignURI: a uri our store does not own falls back to a url
// reference (a note), not a local read.
func TestMaterializeForeignURI(t *testing.T) {
	store := &stubStore{data: map[string][]byte{}}
	_, note, err := materializeA2AFile(context.Background(), t.TempDir(), false, store, server.File{URI: "https://other.example/x", Name: "a.txt"})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if note != "https://other.example/x" {
		t.Fatalf("note = %q, want the url", note)
	}
}
