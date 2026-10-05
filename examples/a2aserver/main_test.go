package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMultiTurnAndInputRequired drives the example's server through the SDK's
// own A2A client and asserts multi-turn context and the input-required round trip.
func TestMultiTurnAndInputRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("spins an A2A server")
	}
	root, err := os.MkdirTemp("", "a2aserver-test-*")
	if err != nil {
		t.Fatalf("temp root: %v", err)
	}
	defer os.RemoveAll(root)

	client, err := newServerClient(root)
	if err != nil {
		t.Fatalf("server client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base, shutdown, err := serve("127.0.0.1:0", client)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer shutdown()

	replies, err := converse(ctx, filepath.Join(root, "caller"), base,
		[]string{"hello", "remember this turn", "please ask me a question"})
	if err != nil {
		t.Fatalf("converse: %v", err)
	}
	if len(replies) != 3 {
		t.Fatalf("replies = %d, want 3", len(replies))
	}
	if !strings.Contains(replies[0], "Turn 1") {
		t.Fatalf("turn 1 = %q", replies[0])
	}
	if !strings.Contains(replies[1], "Turn 2") {
		t.Fatalf("turn 2 (multi-turn) = %q", replies[1])
	}
	if !strings.Contains(replies[2], "answered") {
		t.Fatalf("turn 3 (input-required) = %q", replies[2])
	}
}
