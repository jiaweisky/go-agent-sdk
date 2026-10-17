//go:build e2e

package main

import (
	"strings"
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// TestRunPeersReachesRemoteA2A is the peers end-to-end test: a local agentd
// reaches a remote agentd's A2A agent through a `peers` entry. The remote agent
// runs the live model, so a turn proves the whole chain — config `peers` ->
// registered agent with an A2A backend -> A2A client -> remote execution ->
// mirrored local session -> the HTTP API.
//
// The peer runs as the session's primary agent, so the local model is never
// asked to pick the task tool (vLLM may have tool-calling disabled); the test is
// deterministic and only requires the live model on the remote side.
//
//	export VLLM_BASE_URL=http://host:8000/v1 VLLM_MODEL=...
func TestRunPeersReachesRemoteA2A(t *testing.T) {
	baseURL, model, modalities := envModel(t)

	remoteBase := startRemoteAgentd(t, baseURL, model, modalities)
	localBase := startPeeredAgentd(t, remoteBase, baseURL, model, modalities)

	reply := runHTTPSession(t, localBase, "remote", "Reply with exactly one word: pong", nil)
	if !strings.Contains(strings.ToLower(reply), "pong") {
		t.Fatalf("reply = %q, want it to contain \"pong\" (remote model reached over A2A)", reply)
	}
}

// startRemoteAgentd serves an A2A agent over the live model and returns its
// base URL. Every tool is denied so the request carries no tool definitions: the
// test model may not have tool calling enabled.
func startRemoteAgentd(t *testing.T, baseURL, model string, modalities []string) string {
	t.Helper()
	addr := freeAddr(t)
	stop := startAgentd(t, config.Config{
		Models: []config.ModelConfig{mediaModel(baseURL, model, modalities)},
		Servers: []config.Server{{
			Name: "remote", Kind: "a2a", Listen: addr, Root: t.TempDir(),
			Agents:             []string{"build"},
			Cards:              map[string]config.CardConfig{"build": {Name: "Build", Description: "remote build agent"}},
			InboundPermissions: mediaDenyAll(),
		}},
	})
	t.Cleanup(stop)
	base := "http://" + addr
	waitForCard(t, base+"/.well-known/agent-card.json")
	return base
}

// startPeeredAgentd serves an HTTP API whose `remote` peer is the remote agent,
// and returns its base URL.
func startPeeredAgentd(t *testing.T, remoteBase, baseURL, model string, modalities []string) string {
	t.Helper()
	addr := freeAddr(t)
	stop := startAgentd(t, config.Config{
		Models: []config.ModelConfig{mediaModel(baseURL, model, modalities)},
		Servers: []config.Server{{
			Name: "local", Kind: "http", Listen: addr, Root: t.TempDir(),
			Agents: []string{"build", "remote"},
			Peers: []config.Peer{{
				Name: "remote", Endpoint: remoteBase, Mode: "primary",
				Description: "A remote agent reached over A2A.",
			}},
		}},
	})
	t.Cleanup(stop)
	base := "http://" + addr
	waitForHealth(t, base)
	return base
}
