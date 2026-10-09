package agent

import (
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestLastUserFilesDecodesAttachments(t *testing.T) {
	name := "photo.png"
	req := &llmschema.LLMRequest{
		Messages: []llmschema.Message{{
			Role: llmschema.RoleUser,
			Content: []llmschema.ContentPart{
				llmschema.TextPart{Type: "text", Text: "look"},
				llmschema.MediaPart{Type: "media", MediaType: "image/png", Data: "data:image/png;base64,aGVsbG8=", Filename: &name},
			},
		}},
	}
	files, err := lastUserFiles(req)
	if err != nil {
		t.Fatalf("lastUserFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	got := files[0]
	if got.MIME != "image/png" || got.Name != "photo.png" || string(got.Data) != "hello" {
		t.Fatalf("attachment = %+v (data %q)", got, got.Data)
	}
}

func TestLastUserFilesRejectsUndecodableData(t *testing.T) {
	req := &llmschema.LLMRequest{
		Messages: []llmschema.Message{{
			Role: llmschema.RoleUser,
			Content: []llmschema.ContentPart{
				llmschema.MediaPart{Type: "media", MediaType: "image/png", Data: "not base64!!"},
			},
		}},
	}
	if _, err := lastUserFiles(req); err == nil {
		t.Fatal("expected an error for undecodable attachment data")
	}
}
