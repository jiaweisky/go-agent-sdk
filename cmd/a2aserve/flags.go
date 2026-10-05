package main

import (
	"flag"
	"strings"
)

// flags holds the CLI options. Values are zero by default; only flags the user
// actually set are applied over the config file (see apply).
type flags struct {
	fs            *flag.FlagSet
	config        string
	listen        string
	baseURL       string
	root          string
	workingDir    string
	model         string
	protocol      string
	modelBase     string
	keepAlive     string
	configDirs    stringList
	agents        stringList
	allowInsecure bool
}

func newFlags() *flags {
	f := &flags{fs: flag.NewFlagSet("a2aserve", flag.ContinueOnError)}
	f.fs.StringVar(&f.config, "config", "", "path to a YAML config file")
	f.fs.StringVar(&f.listen, "listen", "", "listen address host:port (default 127.0.0.1:9099)")
	f.fs.StringVar(&f.baseURL, "base-url", "", "public base URL advertised in cards")
	f.fs.StringVar(&f.root, "root", "", "SDK state directory (required)")
	f.fs.StringVar(&f.workingDir, "working-dir", "", "project/worktree root (default: process cwd)")
	f.fs.StringVar(&f.model, "model", "", "default model id (required)")
	f.fs.StringVar(&f.protocol, "protocol", "", "wire protocol id, e.g. openai-responses (required)")
	f.fs.StringVar(&f.modelBase, "model-base-url", "", "model API base URL (protocol default when empty)")
	f.fs.StringVar(&f.keepAlive, "keep-alive", "", "SSE keep-alive interval, e.g. 15s")
	f.fs.Var(&f.configDirs, "config-dir", "agent/skill config directory (repeatable)")
	f.fs.Var(&f.agents, "agent", "agent name to expose (repeatable)")
	f.fs.BoolVar(&f.allowInsecure, "allow-insecure", false, "allow binding a non-loopback address without auth")
	return f
}

func (f *flags) parse(args []string) error {
	return f.fs.Parse(args)
}

// apply overlays the flags the user set onto cfg.
func (f *flags) apply(cfg *config) {
	set := map[string]bool{}
	f.fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["listen"] {
		cfg.Listen = f.listen
	}
	if set["base-url"] {
		cfg.BaseURL = f.baseURL
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
	if set["keep-alive"] {
		cfg.KeepAlive = f.keepAlive
	}
	if set["config-dir"] {
		cfg.ConfigDirs = f.configDirs
	}
	if set["agent"] {
		cfg.Agents = f.agents
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
