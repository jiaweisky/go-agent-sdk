package protocols

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestSystemUpdatePartsExtractsText(t *testing.T) {
	parts := SystemUpdateParts([]schema.ContentPart{
		schema.TextPart{Type: "text", Text: "hello"},
		schema.MediaPart{Type: "media", MediaType: "image/png"},
	})
	if len(parts) != 2 || parts[0].Text != "hello" || parts[1].Text != "" {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestGeminiBodyAllowsNilGeneration(t *testing.T) {
	model := GeminiHTTPRoute.Model(route.RouteModelInput{ID: "gemini-2.0-flash"})
	// A nil Generation must not panic.
	if _, err := GeminiHTTPRoute.Body.From(&schema.LLMRequest{Model: model}); err != nil {
		t.Fatalf("body: %v", err)
	}
}

func TestGeminiMediaMIMEsInitialized(t *testing.T) {
	if len(geminiMediaMIMEs) == 0 {
		t.Fatal("geminiMediaMIMEs is empty; media would always be rejected")
	}
}
