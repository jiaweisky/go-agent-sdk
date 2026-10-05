package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/examples/internal/util"
	"github.com/user/go-agent-sdk/schema"
)

// echoAgentName is how the remote agent is registered in the chat. The name
// describes what the agent does; that it happens to be reached over A2A (and
// served by adk-go) is an implementation detail the definition below records,
// not part of its identity.
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

// registerRemoteAgent registers the remote agent so it is selectable as a
// primary agent (/agent echo) and delegatable via the task tool. The backend
// records how it is reached: A2A, at the given URL. A blank URL is a no-op
// (the server was unavailable).
func registerRemoteAgent(client *agent.Client, url string) error {
	if url == "" || client.Agents().Get(schema.AgentID(echoAgentName)) != nil {
		return nil
	}
	return client.RegisterAgent(echoAgentName, agent.AgentConfig{
		Description: "A deterministic agent that echoes the text it receives.",
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: url},
		},
	})
}
