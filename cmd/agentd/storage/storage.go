// Package storage builds the provider-independent media store selected by a
// config.StorageConfig.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// objectName is the content-addressed object name every backend writes under,
// with the caller's file name as a readable suffix. Repeating a Put is
// idempotent.
func objectName(media schema.MediaUpload) string {
	sum := sha256.Sum256(media.Data)
	name := hex.EncodeToString(sum[:8])
	if media.Name != nil && *media.Name != "" {
		name += "-" + path.Base(filepath.ToSlash(*media.Name))
	}
	return name
}

// New validates cfg and builds its media store. A nil config yields a nil store
// (the SDK keeps the caller-declared media behavior).
func New(cfg *config.StorageConfig) (schema.MediaStore, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Backend {
	case "", config.StorageHTTP:
		return httpStore{config: cfg}, nil
	case config.StorageLocal:
		return localStore{config: cfg}, nil
	case config.StorageS3:
		return s3Store{config: cfg}, nil
	case config.StorageCOS:
		return cosStore{config: cfg}, nil
	default:
		return nil, fmt.Errorf("storage: unknown backend %q", cfg.Backend)
	}
}
