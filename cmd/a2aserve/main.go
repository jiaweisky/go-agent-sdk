// Command a2aserve runs an A2A (Agent2Agent) server backed by go-agent-sdk. By
// default it exposes the built-in "build" agent. Configuration comes from a
// YAML file (--config) and CLI flags; environment variables are not read.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "a2aserve:", err)
		os.Exit(1)
	}
}

// runMain resolves the configuration (defaults < file < flags) and serves.
func runMain(args []string) error {
	flags := newFlags()
	if err := flags.parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg := defaultConfig()
	if flags.config != "" {
		if err := serveconfig.Load(flags.config, &cfg); err != nil {
			return err
		}
	}
	flags.apply(&cfg)
	if err := cfg.validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, cfg)
}
