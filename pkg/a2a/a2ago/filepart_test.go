package a2ago

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
)

func TestFilePart(t *testing.T) {
	gc := clientWithIngest(nil)
	raw := gc.filePart(context.Background(), client.FileAttachment{MIME: "image/png", Name: "photo.png", Data: []byte("bytes")})
	if raw.MediaType != "image/png" || raw.Filename != "photo.png" || string(raw.Raw()) != "bytes" {
		t.Fatalf("raw part = %+v", raw)
	}
	ref := gc.filePart(context.Background(), client.FileAttachment{MIME: "text/plain", Name: "note.txt", URI: "https://example.com/note.txt"})
	if ref.MediaType != "text/plain" || ref.Filename != "note.txt" || string(ref.URL()) != "https://example.com/note.txt" {
		t.Fatalf("url part = %+v", ref)
	}
}
