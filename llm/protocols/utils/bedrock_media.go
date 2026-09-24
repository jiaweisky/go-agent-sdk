package utils

import (
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk/llm/schema"
)

// ImageBlock is a Bedrock image content block.
type ImageBlock struct {
	Image ImageContent `json:"image"`
}

// ImageContent is a Bedrock image payload.
type ImageContent struct {
	Format string `json:"format"`
	Source Source `json:"source"`
}

// DocumentBlock is a Bedrock document content block.
type DocumentBlock struct {
	Document DocumentContent `json:"document"`
}

// DocumentContent is a Bedrock document payload.
type DocumentContent struct {
	Format string `json:"format"`
	Name   string `json:"name"`
	Source Source `json:"source"`
}

// Source is a Bedrock media source: bytes or a URI.
type Source struct {
	Bytes string `json:"bytes"`
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

func documentBlock(part schema.MediaPart, format, bytes string) DocumentBlock {
	name := part.Filename
	if name == nil || *name == "" {
		s := "document." + format
		name = &s
	}
	return DocumentBlock{
		Document: DocumentContent{
			Format: format,
			Name:   *name,
			Source: Source{Bytes: bytes},
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

// Lower converts a media part into the Bedrock block that carries it.
func Lower(part schema.MediaPart) (any, error) {
	mime := strings.ToLower(part.MediaType)
	if imageFormat, ok := imageFormats[mime]; ok {
		media, err := ValidateMedia("Bedrock Converse", part, imageMIMESet())
		if err != nil {
			return nil, err
		}
		return ImageBlock{
			Image: ImageContent{
				Format: imageFormat,
				Source: Source{Bytes: media.Base64},
			},
		}, nil
	}
	if strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("Bedrock Converse does not support image media type %s", part.MediaType)
	}
	if docFormat, ok := documentFormats[mime]; ok {
		media, err := ValidateMedia("Bedrock Converse", part, documentMIMESet())
		if err != nil {
			return nil, err
		}
		return documentBlock(part, docFormat, media.Base64), nil
	}
	return nil, fmt.Errorf("Bedrock Converse does not support media type %s", part.MediaType)
}
