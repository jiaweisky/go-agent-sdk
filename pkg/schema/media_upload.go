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

// MediaCapabilities describes a provider-bound uploader's accepted media.
// Empty MIMEs means unconstrained; MaxBytes 0 means no provider-side limit.
type MediaCapabilities struct {
	MIMEs    []string
	MaxBytes int64
}

// MediaCapabilityReporter is implemented by uploaders that know their
// provider-side media limits.
type MediaCapabilityReporter interface {
	MediaCapabilities() MediaCapabilities
}

// MediaUploader uploads a payload to a provider's Files API and returns a "file"
// reference. A provider's own file API is bound to it, so it is resolved per
// provider id. The SDK calls it only during adaptation; it never owns credentials
// or storage, and implementations must be idempotent.
type MediaUploader interface {
	Upload(ctx context.Context, media MediaUpload) (Reference, error)
}

// StoreCapabilities describes a host's URL store.
type StoreCapabilities struct {
	// Fetchable is true when URLs produced by the store are fetchable by a
	// provider (http/https), false for a local file:// reference.
	Fetchable bool
	// MIMEs are optional store limits; empty means unrestricted.
	MIMEs    []string
	MaxBytes int64
	// TTL is the URL lifetime (0 = no expiry).
	TTL time.Duration
}

// MediaStoreCapabilityReporter is implemented by stores that expose their
// capabilities to the planner.
type MediaStoreCapabilityReporter interface {
	StoreCapabilities() StoreCapabilities
}

// MediaStore is the process's provider-independent storage. It hosts bytes and
// returns a "url" reference any provider that fetches urls can consume (the model
// edge). It is not bound to a provider, so one store serves every provider.
type MediaStore interface {
	// Put writes the media and returns a "url" reference to it.
	Put(ctx context.Context, media MediaUpload) (Reference, error)
}
