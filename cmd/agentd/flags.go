package main

import "flag"

// flags are the whole agentd CLI: everything else lives in the config file.
type flags struct {
	fs      *flag.FlagSet
	config  string
	check   bool
	version bool
}

func newFlags() *flags {
	f := &flags{fs: flag.NewFlagSet("agentd", flag.ContinueOnError)}
	f.fs.StringVar(&f.config, "config", "agentd.yaml", "path to the YAML config file")
	f.fs.BoolVar(&f.check, "check", false, "validate the config and exit")
	f.fs.BoolVar(&f.version, "version", false, "print version and exit")
	return f
}

func (f *flags) parse(args []string) error { return f.fs.Parse(args) }
