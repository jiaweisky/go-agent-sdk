package storage

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestLocalStorePut(t *testing.T) {
	dir := t.TempDir()
	store, err := New(&config.StorageConfig{Backend: config.StorageLocal, Root: dir, ServeBaseURL: "https://media.example/pub"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Mime: "image/png", Name: &name, Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.Value[:len("https://media.example/pub/")] != "https://media.example/pub/" {
		t.Fatalf("value = %q", ref.Value)
	}
}
