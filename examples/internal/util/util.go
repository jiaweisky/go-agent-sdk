// Package util provides shared setup for the go-agent-sdk examples.
package util

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/user/go-agent-sdk/agent"
	"github.com/user/go-agent-sdk/core"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
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

// WithRoot overrides the state root. Defaults to ExampleDir()/.data.
func WithRoot(root string) Option {
	return func(opts *agent.ClientOptions) {
		if root != "" {
			opts.Root = root
		}
	}
}

// WithExampleDir makes an example self-contained: state goes to <dir>/.data and
// <dir>/.config is scanned for the sample agents and skills. Pass ExampleDir(),
// which also prepends it to any explicit ConfigDirs.
func WithExampleDir(dir string) Option {
	return func(opts *agent.ClientOptions) {
		if dir == "" {
			return
		}
		opts.Root = filepath.Join(dir, ".data")
		opts.ConfigDirs = append([]string{filepath.Join(dir, ".config")}, opts.ConfigDirs...)
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

// Client builds a client from environment variables:
//
//	OPENCODE_PROVIDER     openai (default) | anthropic | openai-compatible | gemini
//	OPENCODE_PROVIDER_ID  provider id for openai-compatible (optional)
//	OPENCODE_BASE_URL     provider base URL (optional)
//	OPENCODE_API_KEY      provider API key (falls back to OPENAI_API_KEY for
//	                      openai, ANTHROPIC_API_KEY for anthropic; none for
//	                      openai-compatible)
//	OPENCODE_MODEL        model id (required; provider-specific, so no default)
//
// Credentials are passed in explicitly; the SDK does not read them from
// ~/.config/opencode. Call LoadDotEnv from main to read a .env file in the
// working directory (without overriding variables already set in the
// environment).
//
// Note on openai-compatible: unlike openai/anthropic it has no env fallback, so
// OPENCODE_API_KEY is required — without it the request fails with "Missing auth
// credential: value". (A keyless local endpoint would instead need the
// ClientOptions.ModelInfo path, where a missing key maps to route.None() and no
// auth header is sent.)
//
// The default permission handler is agent.ApproveAll; override it with
// WithPermission.
func Client(options ...Option) (*agent.Client, error) {
	providerType := envOr("OPENCODE_PROVIDER", "openai")
	// The model id is provider-specific, so require it explicitly rather than
	// silently defaulting to another provider's model.
	model := os.Getenv("OPENCODE_MODEL")
	if model == "" {
		return nil, fmt.Errorf("OPENCODE_MODEL is required (the model id is provider-specific)")
	}
	protocol, err := protocolFor(providerType)
	if err != nil {
		return nil, err
	}
	apiKey := resolveAPIKey(providerType)
	if apiKey == "" {
		fmt.Fprintf(os.Stderr,
			"warning: no API key set for provider %q (%s); "+
				"requests will fail with 'Missing auth credential: value'\r\n",
			providerType, apiKeyHint(providerType))
	}
	baseURL := os.Getenv("OPENCODE_BASE_URL")
	var limit schema.ModelLimit
	if value := envLimits(); value != nil {
		limit = schema.ModelLimit{Context: value.Context, Output: value.Output}
	}
	opts := &agent.ClientOptions{
		Model:    model,
		Protocol: protocol,
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := agent.NewModelInfo(protocol, id, baseURL, limit)
			if err != nil {
				return info, err
			}
			if providerID := os.Getenv("OPENCODE_PROVIDER_ID"); providerID != "" {
				info.ProviderID = schema.ProviderID(providerID)
			}
			return info, nil
		},
		Credentials: agent.APIKey(apiKey),
		Permission:  agent.ApproveAll,
		// The example's sample agents/skills, then .config in the working
		// directory (-C pins it) so a project can override them.
		ConfigDirs: []string{".config"},
	}
	for _, option := range options {
		option(opts)
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

// protocolFor maps the OPENCODE_PROVIDER value to a native protocol id.
func protocolFor(providerType string) (schema.ProtocolID, error) {
	switch providerType {
	case "", "openai":
		return "openai-responses", nil
	case "anthropic":
		return "anthropic-messages", nil
	case "openai-compatible":
		return "openai-compatible-chat", nil
	case "gemini":
		return "gemini", nil
	default:
		return "", fmt.Errorf("unsupported OPENCODE_PROVIDER %q", providerType)
	}
}

// resolveAPIKey mirrors the credential the provider's route auth uses:
// OPENCODE_API_KEY first, then the provider's conventional env var. The generic
// openai-compatible provider has no fallback.
func resolveAPIKey(providerType string) string {
	if key := os.Getenv("OPENCODE_API_KEY"); key != "" {
		return key
	}
	switch providerType {
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "openai-compatible":
		// No env fallback: its route auth is BearerAuth(..., nil), so only the
		// explicit OPENCODE_API_KEY (above) can satisfy it.
		return ""
	default: // "openai", "" and the unknown-provider fallback route
		return os.Getenv("OPENAI_API_KEY")
	}
}

func apiKeyHint(providerType string) string {
	switch providerType {
	case "anthropic":
		return "OPENCODE_API_KEY / ANTHROPIC_API_KEY"
	case "openai-compatible":
		return "OPENCODE_API_KEY"
	default:
		return "OPENCODE_API_KEY / OPENAI_API_KEY"
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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

// projectRoot mirrors core's project boundary (core/project.go): the directory
// holding the nearest .git, else the filesystem root.
func projectRoot(start string) string {
	if matches := core.Up([]string{".git"}, start); len(matches) > 0 {
		return filepath.Dir(matches[0])
	}
	return filepath.VolumeName(start) + string(filepath.Separator)
}

// envLimits reads OPENCODE_CONTEXT_LIMIT / OPENCODE_OUTPUT_LIMIT so the runner
// can compact history before the provider rejects an oversized request. Without
// limits, automatic compaction is disabled.
func envLimits() *llmschema.ModelLimits {
	context := envInt("OPENCODE_CONTEXT_LIMIT")
	output := envInt("OPENCODE_OUTPUT_LIMIT")
	if context == 0 && output == 0 {
		return nil
	}
	return llmschema.NewModelLimits(context, output)
}

func envInt(key string) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return value
	}
	return 0
}
