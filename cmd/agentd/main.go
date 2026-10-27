package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// version is the agentd build version, overridable at link time with
// -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agentd: %v\n", err)
		os.Exit(1)
	}
}

// runMain parses flags, loads and validates the configuration, then serves until
// interrupted.
func runMain(args []string) error {
	fl := newFlags()
	if err := fl.parse(args); err != nil {
		return err
	}

	if fl.version {
		fmt.Println(version)
		return nil
	}

	cfg := config.Config{}
	if err := config.Load(fl.config, &cfg); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	for _, line := range config.MediaSummary(cfg) {
		fmt.Fprintln(os.Stderr, line)
	}

	for _, warning := range config.MediaWarnings(cfg) {
		fmt.Fprintf(os.Stderr, "agentd: warning: %s\n", warning)
	}

	// -check validates the configuration and exits without serving.
	if fl.check {
		fmt.Fprintf(os.Stderr, "agentd: configuration %s is valid\n", fl.config)
		return nil
	}

	// Wrap, when configured, re-executes agentd inside a whole-process sandbox
	// (does not return on success).
	if err := wrapProcess(cfg, fl.config); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return run(ctx, cfg, &introspectDoc{config: cfg, path: fl.config, version: version})
}
