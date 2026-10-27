package storage

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

// StoreCapabilities reports that an HTTP-backed store produces a fetchable URL.
// A presigned store also reports its URL lifetime.
func (c httpStore) StoreCapabilities() schema.StoreCapabilities {
	caps := schema.StoreCapabilities{Fetchable: true}
	if c.config.PresignSecret != "" {
		caps.TTL = c.ttl()
	}

	return caps
}

// StoreCapabilities reports a local store as fetchable only when a serve base
// URL exposes it; otherwise it is a file:// reference for the same host.
func (c localStore) StoreCapabilities() schema.StoreCapabilities {
	return schema.StoreCapabilities{Fetchable: c.config.ServeBaseURL != ""}
}

// StoreCapabilities reports an S3 store as a fetchable URL source.
func (c s3Store) StoreCapabilities() schema.StoreCapabilities {
	return schema.StoreCapabilities{Fetchable: true}
}

// StoreCapabilities reports a COS store as a fetchable URL source.
func (c cosStore) StoreCapabilities() schema.StoreCapabilities {
	return schema.StoreCapabilities{Fetchable: true}
}
