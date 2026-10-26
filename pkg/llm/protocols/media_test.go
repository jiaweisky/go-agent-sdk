package protocols

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestMediaAllowedNarrowsByProtocolThenModel(t *testing.T) {
	cases := []struct {
		name    string
		proto   schema.ProtocolID
		input   []string
		mime    string
		allowed bool
	}{
		{"protocol and model allow an image", OpenAIChatHTTPRoute.Protocol, nil, "image/png", true},
		{"openai-chat carries a pdf document", OpenAIChatHTTPRoute.Protocol, nil, "application/pdf", true},
		{"bedrock carries a document", BedrockConverseProtocol.ID, nil, "application/pdf", true},
		{"gemini carries a document", GeminiHTTPRoute.Protocol, nil, "application/pdf", true},
		{"anthropic carries a document", AnthropicMessagesHTTPRoute.Protocol, nil, "application/pdf", true},
		{"text-only model rejects an image", OpenAIChatHTTPRoute.Protocol, []string{"text"}, "image/png", false},
		{"vision model allows an image", OpenAIChatHTTPRoute.Protocol, []string{"text", "image"}, "image/png", true},
		{"gemini carries audio", GeminiHTTPRoute.Protocol, nil, "audio/wav", true},
		{"anthropic carries jpeg", AnthropicMessagesHTTPRoute.Protocol, nil, "image/jpeg", true},
		{"openai-responses docx under the pdf modality", OpenAIResponsesHTTPRoute.Protocol, []string{"text", "pdf"}, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"unknown protocol allows nothing", schema.ProtocolID("nope"), nil, "image/png", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MediaAllowed(tc.proto, tc.input, tc.mime); got != tc.allowed {
				t.Fatalf("MediaAllowed(%s, %v, %s) = %v, want %v", tc.proto, tc.input, tc.mime, got, tc.allowed)
			}
		})
	}
}

func TestMediaFeasibleDescriptor(t *testing.T) {
	support := ProtocolMediaSupport{
		MIMEs:   imageMIMESet,
		Sources: []MediaSource{MediaSourceInline, MediaSourceURL},
		Roles:   []string{"user"},
	}
	if !MediaFeasible(support, nil, "image/png") {
		t.Fatal("image should be feasible")
	}
	if MediaFeasible(support, nil, "audio/wav") {
		t.Fatal("audio is not in the MIME set")
	}
	if MediaFeasible(support, []string{"text"}, "image/png") {
		t.Fatal("a text-only model rejects images")
	}
	if !support.AllowsSource(MediaSourceInline) || support.AllowsSource(MediaSourceFile) {
		t.Fatalf("sources = %v", support.Sources)
	}
	if !support.AllowsRole("user") || support.AllowsRole("tool") {
		t.Fatalf("roles = %v", support.Roles)
	}
	// Empty Sources/Roles mean unconstrained.
	unconstrained := ProtocolMediaSupport{MIMEs: imageMIMESet}
	if !unconstrained.AllowsSource(MediaSourceFile) || !unconstrained.AllowsRole("tool") {
		t.Fatal("empty sources/roles should be unconstrained")
	}
}

func TestAnthropicDocumentLowering(t *testing.T) {
	doc, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "application/pdf", Source: schema.MediaSourceInline,
		Data: "data:application/pdf;base64,JVBERi0=",
	})
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if doc.Type != "document" || doc.Source.Type != "base64" || doc.Source.MediaType != "application/pdf" {
		t.Fatalf("document block = %+v", doc)
	}

	img, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "image/png", Source: schema.MediaSourceInline,
		Data: "data:image/png;base64,aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if img.Type != "image" {
		t.Fatalf("image block type = %q", img.Type)
	}

	urlDoc, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "application/pdf", Source: schema.MediaSourceURL, Data: "https://x/a.pdf",
	})
	if err != nil {
		t.Fatalf("url document: %v", err)
	}
	if urlDoc.Type != "document" || urlDoc.Source.Type != "url" {
		t.Fatalf("url document block = %+v", urlDoc)
	}
}

func TestDeepSeekFileBlockLowering(t *testing.T) {
	// The `file` block belongs to the DeepSeek profile, not the vLLM one.
	if compat, _ := LookupProtocol(schema.ProtocolID(OpenAICompatibleChatHTTPRoute.ID)); compat.Media.AllowsSource(schema.MediaSourceFile) {
		t.Fatal("openai-compatible-chat (vLLM) must not advertise the file source")
	}
	entry, ok := LookupProtocol(schema.ProtocolID(DeepSeekChatHTTPRoute.ID))
	if !ok || !entry.Media.AllowsSource(schema.MediaSourceFile) {
		t.Fatal("deepseek-chat should allow the file source")
	}
	content, err := openaiChatLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "image/png", Source: schema.MediaSourceFile, Data: "file-api-123",
	}, deepSeekProfile)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if content.Type != "file" || content.FileID == nil || *content.FileID != "file-api-123" {
		t.Fatalf("content = %+v", content)
	}
}

func TestAnthropicFileReferenceLowering(t *testing.T) {
	entry, ok := LookupProtocol(schema.ProtocolID(AnthropicMessagesHTTPRoute.ID))
	if !ok || !entry.Media.AllowsSource(schema.MediaSourceFile) {
		t.Fatal("anthropic-messages should allow the file source")
	}
	img, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "image/png", Source: schema.MediaSourceFile, Data: "file-api-1",
	})
	if err != nil || img.Type != "image" || img.Source.Type != "file" || img.Source.FileID != "file-api-1" {
		t.Fatalf("image file ref = %+v err=%v", img, err)
	}
	doc, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "application/pdf", Source: schema.MediaSourceFile, Data: "file-api-2",
	})
	if err != nil || doc.Type != "document" || doc.Source.FileID != "file-api-2" {
		t.Fatalf("document file ref = %+v err=%v", doc, err)
	}
}

// TestResponsesDocumentURLSourceRejected: OpenAI Responses `input_image` takes a
// url, but `input_file` (documents) takes a file_id or inline data only, so a
// pdf url source must be rejected while a pdf file source is allowed.
func TestResponsesDocumentURLSourceRejected(t *testing.T) {
	entry, ok := LookupProtocol(schema.ProtocolID(OpenAIResponsesHTTPRoute.ID))
	if !ok {
		t.Fatal("openai-responses not registered")
	}
	if entry.Media.AllowsSourceFor("application/pdf", MediaSourceURL) {
		t.Fatal("a pdf url source must be rejected for OpenAI Responses")
	}
	if !entry.Media.AllowsSourceFor("image/png", MediaSourceURL) {
		t.Fatal("an image url source is allowed for OpenAI Responses")
	}
	if !entry.Media.AllowsSourceFor("application/pdf", MediaSourceFile) {
		t.Fatal("a pdf file source is allowed for OpenAI Responses")
	}
}

func TestDocumentMIMEExpansion(t *testing.T) {
	chat, _ := LookupProtocol(schema.ProtocolID(OpenAIChatHTTPRoute.ID))
	if !chat.Media.AllowsMIME("application/pdf") || chat.Media.AllowsMIME("text/plain") {
		t.Fatalf("openai-chat document MIMEs = %v", chat.Media.MIMEs)
	}
	responses, _ := LookupProtocol(schema.ProtocolID(OpenAIResponsesHTTPRoute.ID))
	for _, mime := range []string{"application/pdf", "text/plain", "text/markdown", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"} {
		if !responses.Media.AllowsMIME(mime) {
			t.Fatalf("openai-responses should carry %s", mime)
		}
		if responses.Media.AllowsSourceFor(mime, MediaSourceURL) {
			t.Fatalf("openai-responses %s must reject url", mime)
		}
	}
	anthropic, _ := LookupProtocol(schema.ProtocolID(AnthropicMessagesHTTPRoute.ID))
	if !anthropic.Media.AllowsMIME("text/plain") {
		t.Fatal("anthropic should carry text/plain")
	}
	deepseek, _ := LookupProtocol(schema.ProtocolID(DeepSeekChatHTTPRoute.ID))
	if deepseek.Media.AllowsMIME("application/pdf") {
		t.Fatal("deepseek file API must stay image-only")
	}
}

func TestAnthropicTextDocumentLowering(t *testing.T) {
	doc, err := anthropicMessagesLowerMedia(schema.MediaPart{
		Type: "media", MediaType: "text/plain", Source: schema.MediaSourceFile, Data: "file-id-1",
	})
	if err != nil {
		t.Fatalf("text document: %v", err)
	}
	if doc.Type != "document" || doc.Source.Type != "file" || doc.Source.FileID != "file-id-1" {
		t.Fatalf("text document block = %+v", doc)
	}
}
