package a2ago

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
)

func TestFilePart(t *testing.T) {
	gc := &goClient{}
	raw := gc.filePart(client.FileAttachment{MIME: "image/png", Name: "photo.png", Data: []byte("bytes")})
	if raw.MediaType != "image/png" || raw.Filename != "photo.png" || string(raw.Raw()) != "bytes" {
		t.Fatalf("raw part = %+v", raw)
	}
	ref := gc.filePart(client.FileAttachment{MIME: "text/plain", Name: "note.txt", URI: "https://example.com/note.txt"})
	if ref.MediaType != "text/plain" || ref.Filename != "note.txt" || string(ref.URL()) != "https://example.com/note.txt" {
		t.Fatalf("url part = %+v", ref)
	}
}
