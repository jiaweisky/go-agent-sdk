package schema

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrUnsupportedReference reports a reference a store does not own (a foreign
// scheme, host, or bucket). A MediaStore's Get wraps it, so a caller can tell a
// foreign reference from a missing object.
var ErrUnsupportedReference = errors.New("unsupported reference")

// Reference is a host-produced reference to media bytes: its form ("file" or
// "url"), its value (a provider file id/URI, or an http(s) url), and an optional
// expiry. The value is an id or a url, so the field is Value, not ID.
type Reference struct {
	Source    string     `json:"source"`
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// UploadTarget is where a peer may write bytes to a store's ingest (the A2A edge).
type UploadTarget struct {
	URL       string            `json:"url"`
	Method    string            `json:"method,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Reference string            `json:"reference,omitempty"`
	Expires   time.Time         `json:"expires,omitempty"`
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
// edge), and it advertises how a peer may write to it and reads those objects
// back (the A2A file-exchange edge). It is not bound to a provider, so one store
// serves every provider and every peer.
type MediaStore interface {
	// Put writes the media and returns a "url" reference to it.
	Put(ctx context.Context, media MediaUpload) (Reference, error)
	// Get reads a previously Put reference back. It returns an error wrapping
	// ErrUnsupportedReference when the reference is not this store's.
	Get(ctx context.Context, ref Reference) (io.ReadCloser, error)
	// Target returns the static upload target a peer may write to (zero when the
	// store advertises none).
	Target() UploadTarget
	// Reserve issues a per-file upload target (brokered mode) for a named file.
	Reserve(ctx context.Context, name, mime string, size int64) (UploadTarget, error)
}
