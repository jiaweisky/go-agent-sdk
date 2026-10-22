package schema

import (
	"context"
	"time"
)

// Reference is a host-produced reference to media bytes: its form ("file" or
// "url"), its value (a provider file id/URI, or an http(s) url), and an optional
// expiry. The value is an id or a url, so the field is Value, not ID.
type Reference struct {
	Source    string     `json:"source"`
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// MediaUpload is a payload handed to a host that hosts media (a MediaStore or a
// provider MediaUploader).
type MediaUpload struct {
	Mime string
	Name *string
	Data []byte
}

// MediaUploader uploads a payload to a provider's Files API and returns a "file"
// reference. A provider's own file API is bound to it, so it is resolved per
// provider id. The SDK calls it only during adaptation; it never owns credentials
// or storage, and implementations must be idempotent.
type MediaUploader interface {
	Upload(ctx context.Context, media MediaUpload) (Reference, error)
}

// MediaStore is the process's provider-independent storage. It hosts bytes and
// returns a "url" reference any provider that fetches urls can consume (the model
// edge). It is not bound to a provider, so one store serves every provider.
type MediaStore interface {
	// Put writes the media and returns a "url" reference to it.
	Put(ctx context.Context, media MediaUpload) (Reference, error)
}
