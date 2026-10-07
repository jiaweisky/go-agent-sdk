package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

// TestRunGracefulShutdown guards that cancelling the context shuts the server
// down cleanly (context-driven, no explicit Close): run returns nil and the
// listener is released.
func TestRunGracefulShutdown(t *testing.T) {
	addr := freeAddr(t)
	cfg := defaultConfig()
	cfg.Listen = addr
	cfg.Root = t.TempDir()
	cfg.Model = "test-model"
	cfg.Protocol = "openai-responses"
	cfg.Credentials = serveconfig.Credentials{APIKey: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	url := "http://" + addr + "/api/health"
	served := false
	for i := 0; i < 250; i++ {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				served = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !served {
		cancel()
		t.Fatal("health endpoint was not served")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}
