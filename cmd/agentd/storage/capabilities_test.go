package storage

import (
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func capabilities(t *testing.T, cfg *config.StorageConfig) schema.StoreCapabilities {
	t.Helper()
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	reporter, ok := store.(schema.MediaStoreCapabilityReporter)
	if !ok {
		t.Fatalf("%T does not report capabilities", store)
	}
	return reporter.StoreCapabilities()
}

func TestStoreCapabilities(t *testing.T) {
	httpCaps := capabilities(t, &config.StorageConfig{Backend: config.StorageHTTP, URL: "https://store.example/", PresignSecret: "s", PresignTTL: "5m"})
	if !httpCaps.Fetchable || httpCaps.TTL != 5*time.Minute {
		t.Fatalf("http caps = %+v", httpCaps)
	}

	local := capabilities(t, &config.StorageConfig{Backend: config.StorageLocal, Root: t.TempDir()})
	if local.Fetchable {
		t.Fatalf("local caps without serveBaseURL = %+v", local)
	}
	local = capabilities(t, &config.StorageConfig{Backend: config.StorageLocal, Root: t.TempDir(), ServeBaseURL: "https://media.example"})
	if !local.Fetchable {
		t.Fatalf("local caps with serveBaseURL = %+v", local)
	}
}
