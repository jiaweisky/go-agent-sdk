package utils

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestBedrockVideoLowering(t *testing.T) {
	inline, err := BedrockLower(schema.MediaPart{
		Type: "media", MediaType: "video/mp4", Source: schema.MediaSourceInline,
		Data: "data:video/mp4;base64,aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("inline video: %v", err)
	}
	block, ok := inline.(VideoBlock)
	if !ok || block.Video.Format != "mp4" || block.Video.BedrockSource.Bytes == "" {
		t.Fatalf("inline video block = %#v", inline)
	}

	s3, err := BedrockLower(schema.MediaPart{
		Type: "media", MediaType: "video/webm", Source: schema.MediaSourceFile, Data: "s3://bucket/a.webm",
	})
	if err != nil {
		t.Fatalf("s3 video: %v", err)
	}
	s3Block, ok := s3.(VideoBlock)
	if !ok || s3Block.Video.Format != "webm" || s3Block.Video.BedrockSource.S3Location == nil {
		t.Fatalf("s3 video block = %#v", s3)
	}
}
