// Command agentd-client is a minimal Go client for cmd/agentd. It drives the same
// agent over **both** of agentd's transports, so it is the worked reference for
// a host written in another language (e.g. a Python/Node bot):
//
//   - HTTP/SSE — create a session, send a prompt, stream the turn's events and
//     auto-approve permission requests;
//   - A2A — discover the agent card, then stream one message over JSON-RPC.
//
// Either transport can be skipped by leaving its flag empty.
//
//	go run ./examples/agentd-client -url http://127.0.0.1:8787 -a2a http://127.0.0.1:9099
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	httpURL := flag.String("url", "http://127.0.0.1:8787", "agentd HTTP base URL (empty to skip)")
	a2aURL := flag.String("a2a", "", "agentd A2A base URL or card URL (empty to skip)")
	agentID := flag.String("agent", "build", "agent to run over HTTP")
	prompt := flag.String("prompt", "Say hello in one sentence.", "prompt to send")
	token := flag.String("token", "", "HTTP bearer token (when the server requires auth)")
	timeout := flag.Duration("timeout", 2*time.Minute, "per-turn timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	failed := false
	if base := strings.TrimRight(*httpURL, "/"); base != "" {
		fmt.Printf("== HTTP/SSE  %s ==\n", base)
		if err := (&httpClient{base: base, token: *token}).run(ctx, *agentID, *prompt); err != nil {
			fmt.Fprintln(os.Stderr, "http:", err)
			failed = true
		}
		fmt.Println()
	}
	if base := strings.TrimSpace(*a2aURL); base != "" {
		fmt.Printf("== A2A  %s ==\n", base)
		if err := runA2A(ctx, base, *prompt); err != nil {
			fmt.Fprintln(os.Stderr, "a2a:", err)
			failed = true
		}
		fmt.Println()
	}
	if failed {
		os.Exit(1)
	}
}
