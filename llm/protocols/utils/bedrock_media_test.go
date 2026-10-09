package utils

import (
	"encoding/base64"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestBedrockLowerDocumentInline(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 fake"))
	part := schema.MediaPart{Type: "media", MediaType: "application/pdf", Data: "data:application/pdf;base64," + payload}
	block, err := BedrockLower(part)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	doc, ok := block.(DocumentBlock)
	if !ok {
		t.Fatalf("block = %T, want DocumentBlock", block)
	}
	if doc.Document.Format != "pdf" {
		t.Fatalf("format = %q, want pdf", doc.Document.Format)
	}
	if doc.Document.Name != "document.pdf" {
		t.Fatalf("name = %q, want document.pdf", doc.Document.Name)
	}
	if doc.Document.BedrockSource.Bytes != payload {
		t.Fatalf("bytes = %q, want %q", doc.Document.BedrockSource.Bytes, payload)
	}
	if doc.Document.BedrockSource.S3Location != nil {
		t.Fatal("inline document should not carry an s3Location")
	}
}

func TestBedrockLowerDocumentFileIsS3(t *testing.T) {
	part := schema.MediaPart{
		Type: "media", MediaType: "application/pdf",
		Source: schema.MediaSourceFile, Data: "s3://bucket/report.pdf",
		Filename: strPtr("report.pdf"),
	}
	block, err := BedrockLower(part)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	doc, ok := block.(DocumentBlock)
	if !ok {
		t.Fatalf("block = %T, want DocumentBlock", block)
	}
	if doc.Document.Name != "report.pdf" {
		t.Fatalf("name = %q, want report.pdf", doc.Document.Name)
	}
	if doc.Document.BedrockSource.S3Location == nil || doc.Document.BedrockSource.S3Location.URI != "s3://bucket/report.pdf" {
		t.Fatalf("s3 = %+v", doc.Document.BedrockSource.S3Location)
	}
	if doc.Document.BedrockSource.Bytes != "" {
		t.Fatal("a file-source document should not carry inline bytes")
	}
}

func TestBedrockLowerImage(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("pngbytes"))
	part := schema.MediaPart{Type: "media", MediaType: "image/png", Data: "data:image/png;base64," + payload}
	block, err := BedrockLower(part)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	img, ok := block.(ImageBlock)
	if !ok {
		t.Fatalf("block = %T, want ImageBlock", block)
	}
	if img.Image.Format != "png" || img.Image.BedrockSource.Bytes != payload {
		t.Fatalf("image = %+v", img.Image)
	}
}

func TestBedrockLowerRejectsUnsupported(t *testing.T) {
	part := schema.MediaPart{Type: "media", MediaType: "audio/wav", Data: base64.StdEncoding.EncodeToString([]byte("x"))}
	if _, err := BedrockLower(part); err == nil {
		t.Fatal("audio should not lower to a Bedrock block")
	}
}
