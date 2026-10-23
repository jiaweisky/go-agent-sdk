package config

import (
	"errors"
	"fmt"
	"time"
)

// StorageBackend selects the media-store implementation.
type StorageBackend string

const (
	// StorageHTTP writes objects with an HTTP PUT under URL.
	StorageHTTP StorageBackend = "http"
	// StorageLocal writes objects to a shared local directory.
	StorageLocal StorageBackend = "local"
	// StorageS3 writes to an S3-compatible bucket.
	StorageS3 StorageBackend = "s3"
	// StorageCOS writes to a Tencent COS bucket over its native API.
	StorageCOS StorageBackend = "cos"
)

// Valid reports whether b is a supported backend. An empty backend means
// StorageHTTP.
func (b StorageBackend) Valid() bool {
	switch b {
	case "", StorageHTTP, StorageLocal, StorageS3, StorageCOS:
		return true
	}
	return false
}

// StorageConfig configures the process's provider-independent media store: it
// hosts bytes and returns an http(s) url a provider can fetch. One store serves
// every provider.
type StorageConfig struct {
	// Backend selects the storage mechanism; empty means StorageHTTP.
	Backend StorageBackend `yaml:"backend"`
	// URL is the http backend's upload base; a content-addressed object is
	// written under it.
	URL string `yaml:"url"`
	// Auth is the http backend's credential scheme: "" / "none", "basic" or
	// "bearer". It is sent on the Put write.
	Auth string `yaml:"auth"`
	// Username / Password authenticate "basic".
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Token authenticates "bearer".
	Token string `yaml:"token"`
	// Header/Value, when both set, add a custom auth header instead (or on top)
	// of Auth — e.g. a non-standard "X-Api-Key".
	Header string `yaml:"header"`
	Value  string `yaml:"value"`
	// PresignSecret, when set, signs http urls with nginx's secure_link scheme
	// (md5 + expires), so a private origin can accept a provider's fetches
	// without a shared header credential.
	PresignSecret string `yaml:"presignSecret"`
	// PresignTTL bounds a presigned url's lifetime. Empty defaults to 15m.
	PresignTTL string `yaml:"presignTTL"`
	// Root is the local backend's shared directory.
	Root string `yaml:"root"`
	// ServeBaseURL (local) is where Root is served over HTTP, so the url is
	// fetchable by a provider; empty leaves the object reachable only on disk.
	ServeBaseURL string `yaml:"serveBaseURL"`
	// S3 configures an S3-compatible object store (backend StorageS3).
	S3 *S3Config `yaml:"s3"`
	// COS configures a Tencent COS bucket over its native REST API (backend
	// StorageCOS).
	COS *COSConfig `yaml:"cos"`
}

// Validate rejects a malformed store configuration. Nil is valid (storage is
// optional).
func (s *StorageConfig) Validate() error {
	if s == nil {
		return nil
	}
	switch s.Backend {
	case "", StorageHTTP:
		if s.URL == "" {
			return errors.New("storage.url is required")
		}
		if err := validateNonNegativeDuration("storage.presignTTL", s.PresignTTL); err != nil {
			return err
		}
		return validateHTTPAuth(s)
	case StorageLocal:
		if s.Root == "" {
			return errors.New("storage.root is required")
		}
	case StorageS3:
		if s.S3 == nil {
			return errors.New("storage.s3 is required when backend is s3")
		}
		return s.S3.Validate()
	case StorageCOS:
		if s.COS == nil {
			return errors.New("storage.cos is required when backend is cos")
		}
		return s.COS.Validate()
	default:
		return fmt.Errorf("storage.backend must be http, local, s3 or cos, got %q", s.Backend)
	}
	return nil
}

// validateNonNegativeDuration checks an optional duration string and rejects a
// negative value. An empty value means the default.
func validateNonNegativeDuration(field, raw string) error {
	if raw == "" {
		return nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if duration < 0 {
		return fmt.Errorf("%s must not be negative", field)
	}
	return nil
}

// validateHTTPAuth rejects an inconsistent http auth configuration.
func validateHTTPAuth(s *StorageConfig) error {
	switch s.Auth {
	case "", "none":
	case "basic":
		if s.Username == "" || s.Password == "" {
			return errors.New("storage.auth basic requires username and password")
		}
	case "bearer":
		if s.Token == "" {
			return errors.New("storage.auth bearer requires token")
		}
	default:
		return fmt.Errorf("storage.auth must be none, basic or bearer, got %q", s.Auth)
	}
	if (s.Header == "") != (s.Value == "") {
		return errors.New("storage.header and storage.value must be set together")
	}
	return nil
}
