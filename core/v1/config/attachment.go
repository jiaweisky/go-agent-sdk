package config

import "github.com/user/go-agent-sdk/schema"

// V1AttachmentImage mirrors ConfigAttachmentV1.Image.
type V1AttachmentImage struct {
	AutoResize     *bool               `json:"auto_resize,omitempty"`
	MaxWidth       *schema.PositiveInt `json:"max_width,omitempty"`
	MaxHeight      *schema.PositiveInt `json:"max_height,omitempty"`
	MaxBase64Bytes *schema.PositiveInt `json:"max_base64_bytes,omitempty"`
}

// V1AttachmentInfo mirrors ConfigAttachmentV1.Info.
type V1AttachmentInfo struct {
	Image *V1AttachmentImage `json:"image,omitempty"`
}
