package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// localStore hosts objects in a shared directory (same host or NFS). It is
// provider-independent: the url is fetchable when ServeBaseURL serves Root over
// HTTP; otherwise the reference is a file:// url (reachable on the same host).
// It is the "shared store" for same-host agents (harbor's D2).
type localStore struct{ config *config.StorageConfig }

func (c localStore) base() string {
	if c.config.ServeBaseURL != "" {
		return strings.TrimRight(c.config.ServeBaseURL, "/") + "/"
	}
	return "file://" + strings.TrimRight(c.config.Root, "/") + "/"
}

// Put writes the media under Root and returns a url reference.
func (c localStore) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	if c.config.Root == "" {
		return schema.Reference{}, errors.New("storage: root is required")
	}
	if err := os.MkdirAll(c.config.Root, 0o755); err != nil {
		return schema.Reference{}, err
	}
	name := objectName(media)
	if err := os.WriteFile(filepath.Join(c.config.Root, name), media.Data, 0o644); err != nil {
		return schema.Reference{}, err
	}
	return schema.Reference{Source: "url", Value: c.base() + name}, nil
}
