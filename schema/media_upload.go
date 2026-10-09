package schema

import (
	"context"
	"time"
)

// MediaRef is a resolved provider reference to uploaded media: the source kind
// ("file" or "url") plus the provider's id/URI (a file_id, a file URI, an s3 URI
// or an http URL). ExpiresAt, when set, is when the reference stops being usable.
type MediaRef struct {
	Source    string     `json:"source"`
	ID        string     `json:"id"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// MediaUpload is an inline payload handed to a MediaUploader.
type MediaUpload struct {
	Mime string
	Name *string
	Data []byte
}

// MediaUploader uploads an inline payload to a provider's Files API and returns
// a reference the SDK can send. The SDK calls it only during inline→file
// adaptation; it never owns credentials or storage, and implementations must be
// idempotent.
type MediaUploader interface {
	Upload(ctx context.Context, media MediaUpload) (MediaRef, error)
}
