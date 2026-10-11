package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func testModel() config.ModelConfig {
	return config.ModelConfig{
		ID:          "test-model",
		Model:       "test-model",
		Protocol:    "openai-responses",
		Credentials: config.Credentials{APIKey: "test"},
	}
}

// TestRunRoutesByPort serves two servers with no host on distinct addresses and
// asserts both answer: without a host, differing listens degrade to port routing.
func TestRunRoutesByPort(t *testing.T) {
	addrA, addrB := freeAddr(t), freeAddr(t)
	cfg := config.Config{
		Models: []config.ModelConfig{testModel()},
		Servers: []config.Server{
			{Name: "a", Kind: "http", Listen: addrA, Root: t.TempDir(), Agents: []string{"build"}},
			{Name: "b", Kind: "http", Listen: addrB, Root: t.TempDir(), Agents: []string{"build"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}()

	for _, addr := range []string{addrA, addrB} {
		waitForStatus(t, "http://"+addr+"/api/health")
	}
}

func TestRunServesHTTP(t *testing.T) {
	addr := freeAddr(t)
	cfg := config.Config{
		Models: []config.ModelConfig{testModel()},
		Servers: []config.Server{{
			Name: "http", Kind: "http", Listen: addr, Root: t.TempDir(), Agents: []string{"build"},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	waitForStatus(t, "http://"+addr+"/api/health")

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func waitForStatus(t *testing.T, url string) {
	t.Helper()
	var lastErr error
	for i := 0; i < 200; i++ {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s not served: %v", url, lastErr)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}
