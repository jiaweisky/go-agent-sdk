package utils

import (
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// ImageBlock is a Bedrock image content block.
type ImageBlock struct {
	Image ImageContent `json:"image"`
}

// ImageContent is a Bedrock image payload.
type ImageContent struct {
	Format        string        `json:"format"`
	BedrockSource BedrockSource `json:"source"`
}

// DocumentBlock is a Bedrock document content block.
type DocumentBlock struct {
	Document BedrockDocumentContent `json:"document"`
}

// BedrockDocumentContent is a Bedrock document payload.
type BedrockDocumentContent struct {
	Format        string        `json:"format"`
	Name          string        `json:"name"`
	BedrockSource BedrockSource `json:"source"`
}

// BedrockSource is a Bedrock media source: base64 bytes, or an S3 location.
type BedrockSource struct {
	Bytes      string             `json:"bytes,omitempty"`
	S3Location *BedrockS3Location `json:"s3Location,omitempty"`
}

// BedrockS3Location is an S3 object reference the provider reads.
type BedrockS3Location struct {
	URI string `json:"uri"`
}

var imageFormats = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpeg",
	"image/jpg":  "jpeg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

var documentFormats = map[string]string{
	"application/pdf":    "pdf",
	"text/csv":           "csv",
	"application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx",
	"text/html":     "html",
	"text/plain":    "txt",
	"text/markdown": "md",
}

func documentBlock(part schema.MediaPart, format string, source BedrockSource) DocumentBlock {
	name := part.Filename
	if name == nil || *name == "" {
		s := "document." + format
		name = &s
	}
	return DocumentBlock{
		Document: BedrockDocumentContent{
			Format:        format,
			Name:          *name,
			BedrockSource: source,
		},
	}
}

func imageMIMESet() map[string]struct{} {
	m := make(map[string]struct{}, len(imageFormats))
	for k := range imageFormats {
		m[k] = struct{}{}
	}
	return m
}

func documentMIMESet() map[string]struct{} {
	m := make(map[string]struct{}, len(documentFormats))
	for k := range documentFormats {
		m[k] = struct{}{}
	}
	return m
}

// BedrockMediaMIMEs returns the MIME types Bedrock Converse can carry: images
// and documents.
func BedrockMediaMIMEs() []string {
	values := make([]string, 0, len(imageFormats)+len(documentFormats))
	for mime := range imageFormats {
		values = append(values, mime)
	}
	for mime := range documentFormats {
		values = append(values, mime)
	}
	return values
}

// BedrockLower converts a media part into the Bedrock block that carries it.
func BedrockLower(part schema.MediaPart) (any, error) {
	mime := strings.ToLower(part.MediaType)
	source := part.Source
	if source == "" {
		source = schema.MediaSourceInline
	}
	imageFormat, isImage := imageFormats[mime]
	docFormat, isDoc := documentFormats[mime]
	if !isImage && !isDoc {
		return nil, fmt.Errorf("Bedrock Converse does not support media type %s", part.MediaType)
	}

	// A file source is an S3 reference the provider reads.
	if source == schema.MediaSourceFile {
		uri, _ := part.Data.(string)
		if strings.TrimSpace(uri) == "" {
			return nil, fmt.Errorf("Bedrock Converse media reference is empty")
		}
		s3 := &BedrockS3Location{URI: uri}
		if isImage {
			return ImageBlock{Image: ImageContent{Format: imageFormat, BedrockSource: BedrockSource{S3Location: s3}}}, nil
		}
		return documentBlock(part, docFormat, BedrockSource{S3Location: s3}), nil
	}

	if isImage {
		media, err := ValidateMedia("Bedrock Converse", part, imageMIMESet())
		if err != nil {
			return nil, err
		}
		return ImageBlock{
			Image: ImageContent{
				Format:        imageFormat,
				BedrockSource: BedrockSource{Bytes: media.Base64},
			},
		}, nil
	}
	media, err := ValidateMedia("Bedrock Converse", part, documentMIMESet())
	if err != nil {
		return nil, err
	}
	return documentBlock(part, docFormat, BedrockSource{Bytes: media.Base64}), nil
}
