// Command agentd-supervisor is a reusable host supervisor for cmd/agentd. It
// speaks the supervisor_command protocol — one JSON request on stdin, one JSON
// response on stdout — and satisfies it through a running agentd's control
// API, so a host can point its supervisor_command at it without
// spawning Go processes itself.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agentd-supervisor: %v\n", err)
		os.Exit(1)
	}
}

// run reads one request from stdin and writes one response to stdout: the lease
// for acquire, or {"ok": true} for release.
func run(args []string) error {
	fs := flag.NewFlagSet("agentd-supervisor", flag.ContinueOnError)
	configPath := fs.String("config", "agentd-supervisor.yaml", "path to the supervisor config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var cfg supervisorConfig
	if err := config.Load(*configPath, &cfg); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	lease, err := newSupervisor(cfg).handle(req)
	if err != nil {
		return err
	}
	if lease != nil {
		return json.NewEncoder(os.Stdout).Encode(lease)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]bool{"ok": true})
}
