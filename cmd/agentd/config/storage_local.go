package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// localStore hosts objects in a shared directory (same host or NFS). It is
// provider-independent: the url is fetchable when ServeBaseURL serves Root over
// HTTP; otherwise the reference is a file:// url (reachable on the same host).
// It is the "shared store" for same-host agents (harbor's D2).
type localStore struct{ config StorageConfig }

func (c localStore) base() string {
	if c.config.ServeBaseURL != "" {
		return strings.TrimRight(c.config.ServeBaseURL, "/") + "/"
	}
	return "file://" + strings.TrimRight(c.config.Root, "/") + "/"
}

// name is a content-addressed object name, so a repeated Put is idempotent.
func (c localStore) name(media schema.MediaUpload) string {
	sum := sha256.Sum256(media.Data)
	name := hex.EncodeToString(sum[:8])
	if media.Name != nil && *media.Name != "" {
		name += "-" + filepath.Base(*media.Name)
	}
	return name
}

// Put writes the media under Root and returns a url reference.
func (c localStore) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	if c.config.Root == "" {
		return schema.Reference{}, errors.New("storage: root is required")
	}
	if err := os.MkdirAll(c.config.Root, 0o755); err != nil {
		return schema.Reference{}, err
	}
	name := c.name(media)
	if err := os.WriteFile(filepath.Join(c.config.Root, name), media.Data, 0o644); err != nil {
		return schema.Reference{}, err
	}
	return schema.Reference{Source: "url", Value: c.base() + name}, nil
}

// pathFor maps a reference back to a file under Root, rejecting anything that
// escapes it (a foreign uri, traversal, or a nested path).
func (c localStore) pathFor(value string) (string, bool) {
	name := strings.TrimPrefix(value, c.base())
	if name == value || name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return "", false
	}
	return filepath.Join(c.config.Root, name), true
}

// Get reads a Put object back. It rejects a reference that is not this store's.
func (c localStore) Get(ctx context.Context, ref schema.Reference) (io.ReadCloser, error) {
	path, ok := c.pathFor(ref.Value)
	if !ok {
		return nil, fmt.Errorf("%w: %q", schema.ErrUnsupportedReference, ref.Value)
	}
	return os.Open(path)
}

// Target is empty: a local directory has no network upload target (peers use the
// server's ingest endpoint or the shared filesystem directly).
func (c localStore) Target() schema.UploadTarget { return schema.UploadTarget{} }

// Reserve is empty for the same reason as Target.
func (c localStore) Reserve(ctx context.Context, name, mime string, size int64) (schema.UploadTarget, error) {
	return schema.UploadTarget{}, nil
}
