// Package sampleutil provides shared setup for the go-agent-sdk examples.
package sampleutil

import (
	"os"

	"github.com/user/go-agent-sdk/agent"
)

// Client builds a client from environment variables:
//
//	OPENCODE_PROVIDER     openai (default) | anthropic | openai-compatible
//	OPENCODE_PROVIDER_ID  provider id for openai-compatible (optional)
//	OPENCODE_BASE_URL     provider base URL (optional)
//	OPENCODE_API_KEY      provider API key
//	OPENCODE_MODEL        model id (default: gpt-4o)
func Client() (*agent.Client, error) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Provider: agent.ProviderConfig{
			Type:     envOr("OPENCODE_PROVIDER", "openai"),
			Provider: os.Getenv("OPENCODE_PROVIDER_ID"),
			BaseURL:  os.Getenv("OPENCODE_BASE_URL"),
			APIKey:   os.Getenv("OPENCODE_API_KEY"),
		},
		Model:      envOr("OPENCODE_MODEL", "gpt-4o"),
		Permission: agent.ApproveAll,
	})
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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
