package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jiaweisky/go-agent-sdk/examples/internal/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// echoAgentName is how the remote agent is named in the chat and in the
// generated markdown file. The name describes what the agent does; that it
// happens to be reached over A2A (and served by adk-go) is an implementation
// detail the backend in the definition records, not part of its identity.
const echoAgentName = "echo"

// startAdkA2A launches the examples/adka2a submodule (a Google ADK agent served
// over A2A) and returns its base URL plus a stop function. The submodule is a
// separate Go module — it depends on adk-go, which the SDK itself never does —
// so it is run from its own directory. This is the cross-implementation peer:
// the SDK talks to a different A2A implementation over the wire.
func startAdkA2A() (string, func(), error) {
	dir := filepath.Join(filepath.Dir(util.ExampleDir()), "adka2a") // examples/chat -> examples/adka2a
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	cmd := exec.Command("go", "run", ".", "--http", addr)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	stop := func() { killProcessGroup(cmd) }

	// The first run compiles adk-go, which is heavy; allow generous startup.
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := waitForTCP(ctx, addr); err != nil {
		stop()
		return "", nil, fmt.Errorf("adk A2A did not start: %w", err)
	}
	return "http://" + addr, stop, nil
}

// echoAgentMarkdown renders the markdown agent file that declares the remote A2A
// agent. The frontmatter names an A2A backend, so ConfigDirs alone is enough to
// reach a peer over the wire — no RegisterAgent call, exactly like any other
// configured subagent. The body is unused: the remote agent runs its own loop.
func echoAgentMarkdown(url string) string {
	return fmt.Sprintf(`---
description: A deterministic agent that echoes the text it receives (reached over A2A).
mode: all
backend:
  type: a2a
  a2a:
    endpoint: %q
---
`, url)
}

// writeRemoteAgentConfig writes the remote agent's markdown file under dir (a
// ConfigDirs entry) as agent/<echoAgentName>.md, so the chat loads it like any
// sample agent. The ADK server binds an ephemeral port, so the file is
// generated per run; a blank URL (the server was unavailable) removes a stale
// file left by a previous run.
func writeRemoteAgentConfig(dir, url string) error {
	path := filepath.Join(dir, "agent", echoAgentName+".md")
	if url == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(echoAgentMarkdown(url)), 0o644)
}

// withConfigDir appends a directory to the client's ConfigDirs, so agents
// declared in generated markdown load alongside the example's sample config.
func withConfigDir(dir string) util.Option {
	return func(opts *agent.ClientOptions) {
		opts.ConfigDirs = append(opts.ConfigDirs, dir)
	}
}
