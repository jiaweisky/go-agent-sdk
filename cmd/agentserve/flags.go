package main

import (
	"flag"
	"strings"
)

// flags holds the CLI options. Only flags the user set are applied over the
// config file (see apply).
type flags struct {
	fs                  *flag.FlagSet
	config              string
	listen              string
	root                string
	workingDir          string
	model               string
	protocol            string
	modelBase           string
	subagentDepth       int
	titleAuto           bool
	configDirs          stringList
	agents              stringList
	interactionTimeout  string
	interactionFallback string
	publicRoutes        stringList
	cors                stringList
	subscriberCapacity  int
	keepAlive           string
	allowInsecure       bool
}

func newFlags() *flags {
	f := &flags{fs: flag.NewFlagSet("agentserve", flag.ContinueOnError)}
	f.fs.StringVar(&f.config, "config", "", "path to a YAML config file")
	f.fs.StringVar(&f.listen, "listen", "", "listen address host:port (default 127.0.0.1:8787)")
	f.fs.StringVar(&f.root, "root", "", "SDK state directory (required)")
	f.fs.StringVar(&f.workingDir, "working-dir", "", "project/worktree root (default: process cwd)")
	f.fs.StringVar(&f.model, "model", "", "default model id (required)")
	f.fs.StringVar(&f.protocol, "protocol", "", "wire protocol id, e.g. openai-responses (required)")
	f.fs.StringVar(&f.modelBase, "model-base-url", "", "model API base URL (protocol default when empty)")
	f.fs.IntVar(&f.subagentDepth, "subagent-depth", 0, "max nested task subagent recursion")
	f.fs.BoolVar(&f.titleAuto, "title-auto", false, "enable automatic session titles")
	f.fs.Var(&f.configDirs, "config-dir", "agent/skill config directory (repeatable)")
	f.fs.Var(&f.agents, "agent", "usable agent name (repeatable)")
	f.fs.StringVar(&f.interactionTimeout, "interaction-timeout", "", "HITL fallback timeout, e.g. 5m")
	f.fs.StringVar(&f.interactionFallback, "interaction-fallback", "", "reject | allow")
	f.fs.Var(&f.publicRoutes, "public-route", "unauthenticated route pattern (repeatable)")
	f.fs.Var(&f.cors, "cors", "extra browser origin (repeatable)")
	f.fs.IntVar(&f.subscriberCapacity, "subscriber-capacity", 0, "SSE subscriber queue bound")
	f.fs.StringVar(&f.keepAlive, "keep-alive", "", "SSE heartbeat interval, e.g. 15s")
	f.fs.BoolVar(&f.allowInsecure, "allow-insecure", false, "allow a non-loopback bind without auth")
	return f
}

func (f *flags) parse(args []string) error { return f.fs.Parse(args) }

// apply overlays the flags the user set onto cfg.
func (f *flags) apply(cfg *config) {
	set := map[string]bool{}
	f.fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["listen"] {
		cfg.Listen = f.listen
	}
	if set["root"] {
		cfg.Root = f.root
	}
	if set["working-dir"] {
		cfg.WorkingDirectory = f.workingDir
	}
	if set["model"] {
		cfg.Model = f.model
	}
	if set["protocol"] {
		cfg.Protocol = f.protocol
	}
	if set["model-base-url"] {
		cfg.ModelBaseURL = f.modelBase
	}
	if set["subagent-depth"] {
		cfg.SubagentDepth = f.subagentDepth
	}
	if set["title-auto"] {
		cfg.TitleAuto = f.titleAuto
	}
	if set["config-dir"] {
		cfg.ConfigDirs = f.configDirs
	}
	if set["agent"] {
		cfg.Agents = f.agents
	}
	if set["interaction-timeout"] {
		cfg.InteractionTimeout = f.interactionTimeout
	}
	if set["interaction-fallback"] {
		cfg.InteractionFallback = f.interactionFallback
	}
	if set["public-route"] {
		cfg.PublicRoutes = f.publicRoutes
	}
	if set["cors"] {
		cfg.CORS = f.cors
	}
	if set["subscriber-capacity"] {
		cfg.SubscriberCapacity = f.subscriberCapacity
	}
	if set["keep-alive"] {
		cfg.KeepAlive = f.keepAlive
	}
	if set["allow-insecure"] {
		cfg.AllowInsecure = f.allowInsecure
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}
