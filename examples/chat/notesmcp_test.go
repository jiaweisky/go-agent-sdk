package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
)

// TestStartNotesMCP guards the whole path the chat relies on: spawning the
// notes server over streamable HTTP and connecting the MCP client to it.
func TestStartNotesMCP(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the notes server with go run")
	}
	url, stop, err := startNotesMCPAt(filepath.Join(t.TempDir(), "notes.json"))
	if err != nil {
		t.Fatalf("start notes MCP: %v", err)
	}
	defer stop()

	client := mcp.NewClient(t.TempDir(), "test", "0")
	defer client.Close()
	if status := client.Connect(context.Background(), "notes", mcp.ServerConfig{Type: "remote", URL: url}); status.Status != mcp.StatusConnected {
		t.Fatalf("connect = %+v", status)
	}
	if len(client.Prompts(context.Background())) == 0 {
		t.Fatal("no prompts")
	}
	if len(client.Resources(context.Background())) == 0 {
		t.Fatal("no resources")
	}
}
