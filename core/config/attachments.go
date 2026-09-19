package config

import "github.com/user/go-agent-sdk/schema"

// AttachmentsImage mirrors ConfigAttachments.Image.
type AttachmentsImage struct {
	AutoResize     *bool               `json:"auto_resize,omitempty"`
	MaxWidth       *schema.PositiveInt `json:"max_width,omitempty"`
	MaxHeight      *schema.PositiveInt `json:"max_height,omitempty"`
	MaxBase64Bytes *schema.PositiveInt `json:"max_base64_bytes,omitempty"`
}

// AttachmentsInfo mirrors ConfigAttachments.Info.
type AttachmentsInfo struct {
	Image *AttachmentsImage `json:"image,omitempty"`
}
