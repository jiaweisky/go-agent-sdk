// Package util provides shared setup for the go-agent-sdk examples.
package util

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/core"
)

// Option customizes the client built by Client.
type Option func(*agent.ClientOptions)

// WithPermission sets the client-level permission handler.
func WithPermission(handler agent.PermissionHandlerFunc) Option {
	return func(opts *agent.ClientOptions) {
		if handler != nil {
			opts.Permission = handler
		}
	}
}

// WithQuestion sets the client-level question handler. Unlike a per-session
// SessionConfig.OnQuestion, it is inherited by subagent (task) sessions too.
func WithQuestion(handler agent.QuestionHandlerFunc) Option {
	return func(opts *agent.ClientOptions) {
		if handler != nil {
			opts.Question = handler
		}
	}
}

// ExampleDir is the directory of the file that calls it, so an example's state
// and sample config live next to the example instead of in the user's home (and
// stay put when -C changes the working directory). Call it from the example's
// own main or from a function defined there.
func ExampleDir() string {
	if _, file, _, ok := runtime.Caller(1); ok {
		return filepath.Dir(file)
	}
	return "."
}

// WithMCP adds MCP servers whose tools are exposed to every session.
func WithMCP(configs ...agent.MCPConfig) Option {
	return func(opts *agent.ClientOptions) {
		opts.MCP = append(opts.MCP, configs...)
	}
}

// WithCompaction overrides the client's compaction settings.
func WithCompaction(config agent.CompactionConfig) Option {
	return func(opts *agent.ClientOptions) {
		opts.Compaction = &config
	}
}

// WithTitle overrides the client's automatic session title settings.
func WithTitle(config agent.TitleConfig) Option {
	return func(opts *agent.ClientOptions) {
		opts.Title = &config
	}
}

// WithReference registers a named reference source injected into the model's
// system context.
func WithReference(name string, source core.ReferenceSource) Option {
	return func(opts *agent.ClientOptions) {
		if opts.References == nil {
			opts.References = map[string]core.ReferenceSource{}
		}
		opts.References[name] = source
	}
}

// WithSubagentDepth sets the maximum nesting depth for task subagents.
func WithSubagentDepth(depth int) Option {
	return func(opts *agent.ClientOptions) {
		opts.SubagentDepth = depth
	}
}

// WithPlugins registers plugins loaded at client startup.
func WithPlugins(configs []agent.PluginConfig) Option {
	return func(opts *agent.ClientOptions) {
		opts.Plugins = append(opts.Plugins, configs...)
	}
}

// Client builds a client from the caller's options. The model, its metadata and
// its credentials come from an option (for example a Catalog option), so there
// is nothing provider-specific to configure through the environment.
//
// Credentials are passed in explicitly; the SDK does not read them from a
// config file. Call LoadDotEnv from main to read a .env file in the working
// directory (without overriding variables already set in the environment).
//
// The default permission handler is agent.ApproveAll; override it with
// WithPermission.
func Client(dir string, options ...Option) (*agent.Client, error) {
	opts := &agent.ClientOptions{
		// The SDK never picks a state directory, so the example keeps its state
		// next to itself (git-ignored) instead of in the user's home.
		Root:       filepath.Join(dir, ".data"),
		Permission: agent.ApproveAll,
		// The example's sample agents/skills, then .config in the working
		// directory (-C pins it) so a project can override them.
		ConfigDirs: []string{filepath.Join(dir, ".config"), ".config"},
	}
	for _, option := range options {
		option(opts)
	}
	if opts.ModelInfo == nil {
		return nil, fmt.Errorf("no model info: pass a catalog option")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("no model configured: pass a model option")
	}
	client, err := agent.NewClient(opts)
	if err != nil {
		return nil, err
	}
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "You are a helpful assistant.",
	}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// LoadDotEnv loads KEY=VALUE pairs from the first .env found searching the
// working directory and its ancestors up to the project root (the nearest .git,
// or the filesystem root when not in a git repository), without overriding
// variables already present in the environment. Best-effort: a missing or
// malformed file is ignored.
//
// Call it explicitly from main before building options (arguments are evaluated
// before Client runs), after any os.Chdir, so the file is read from the intended
// directory.
func LoadDotEnv() {
	path, ok := findDotEnv()
	if !ok {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

// findDotEnv returns the nearest .env at or above the working directory, stopping
// at the project root so a stray .env outside the repository is never loaded.
func findDotEnv() (string, bool) {
	start, err := os.Getwd()
	if err != nil {
		return "", false
	}
	stop := projectRoot(start)
	for current := start; ; {
		candidate := filepath.Join(current, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		if current == stop {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// projectRoot is core's project boundary: the directory
// holding the nearest .git, else the filesystem root.
func projectRoot(start string) string {
	if matches := core.FindUp([]string{".git"}, start); len(matches) > 0 {
		return filepath.Dir(matches[0])
	}
	return filepath.VolumeName(start) + string(filepath.Separator)
}
