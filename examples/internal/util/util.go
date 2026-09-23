// Package util provides shared setup for the go-agent-sdk examples.
package util

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/user/go-agent-sdk/agent"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
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

// WithMCP adds MCP servers whose tools are exposed to every session.
func WithMCP(configs ...agent.MCPConfig) Option {
	return func(opts *agent.ClientOptions) {
		opts.MCP = append(opts.MCP, configs...)
	}
}

// Client builds a client from environment variables:
//
//	OPENCODE_PROVIDER     openai (default) | anthropic | openai-compatible
//	OPENCODE_PROVIDER_ID  provider id for openai-compatible (optional)
//	OPENCODE_BASE_URL     provider base URL (optional)
//	OPENCODE_API_KEY      provider API key (falls back to OPENAI_API_KEY for
//	                      openai, ANTHROPIC_API_KEY for anthropic; none for
//	                      openai-compatible)
//	OPENCODE_MODEL        model id (required; provider-specific, so no default)
//
// Credentials are passed in explicitly; the SDK does not read them from
// ~/.config/opencode. A .env file in the working directory is loaded at package
// init (without overriding variables already set in the environment).
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
	apiKey := resolveAPIKey(providerType)
	if apiKey == "" {
		fmt.Fprintf(os.Stderr,
			"warning: no API key set for provider %q (%s); "+
				"requests will fail with 'Missing auth credential: value'\r\n",
			providerType, apiKeyHint(providerType))
	}
	opts := &agent.ClientOptions{
		Provider: agent.ProviderConfig{
			Type:     providerType,
			Provider: os.Getenv("OPENCODE_PROVIDER_ID"),
			BaseURL:  os.Getenv("OPENCODE_BASE_URL"),
			APIKey:   apiKey,
			Limits:   envLimits(),
		},
		Model:      model,
		Permission: agent.ApproveAll,
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

// init loads .env before main, so examples can read the environment while
// building options (arguments are evaluated before Client runs).
func init() { LoadDotEnv() }

// LoadDotEnv loads KEY=VALUE pairs from .env in the working directory, without
// overriding variables already present in the environment. Best-effort: a
// missing or malformed file is ignored.
//
// It runs once at package init, before main and before any environment variable
// is read (e.g. when building MCP configs).
func LoadDotEnv() {
	data, err := os.ReadFile(".env")
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
