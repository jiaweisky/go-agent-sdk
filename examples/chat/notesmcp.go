package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/examples/internal/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
)

// startNotesMCP launches the notes MCP server as a child process over
// streamable HTTP and returns its URL and a stop function. It runs `go run`, so
// the whole process tree is killed as a group on stop. The chat owns the remote
// server's lifecycle; a local (stdio) server would be spawned by the SDK itself.
func startNotesMCP() (string, func(), error) {
	return startNotesMCPAt(filepath.Join(dataDir(), "notes.json"))
}

// startNotesMCPAt is startNotesMCP with an explicit notes file, so tests can
// keep their notes out of the example's state.
func startNotesMCPAt(data string) (string, func(), error) {
	root := filepath.Dir(filepath.Dir(util.ExampleDir())) // examples/chat -> examples -> repo root
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	cmd := exec.Command("go", "run", "./examples/notesmcp", "--http", addr, "--data", data)
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	stop := func() { killProcessGroup(cmd) }

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := waitForTCP(ctx, addr); err != nil {
		stop()
		return "", nil, fmt.Errorf("notes MCP did not start: %w", err)
	}
	return "http://" + addr, stop, nil
}

// notesMCP is the remote server config for a running notes server.
func notesMCP(url string) []agent.MCPConfig {
	if url == "" {
		return nil
	}
	return []agent.MCPConfig{{Name: "notes", Server: mcp.ServerConfig{Type: "remote", URL: url}}}
}

// freePort reserves a loopback port and returns its number.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// waitForTCP polls until addr accepts a connection or ctx ends.
func waitForTCP(ctx context.Context, addr string) error {
	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
