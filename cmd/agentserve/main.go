// Command agentserve runs an HTTP/SSE server backed by go-agent-sdk so hosts
// written in other languages can drive the SDK. Configuration comes from a YAML
// file (--config) and CLI flags; environment variables are not read.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentserve:", err)
		os.Exit(1)
	}
}

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
		if err := loadConfig(flags.config, &cfg); err != nil {
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
