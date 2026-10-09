// Command external is a boundary probe: it lives in its own module and imports
// ONLY the public pkg/... packages, so it fails to compile if any public API
// leaks an internal/ type. Run: cd testdata/external && go build ./...
package main

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/server"
	"github.com/jiaweisky/go-agent-sdk/pkg/session"

	a2aserver "github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// Compile-time references to the public surface that used to expose internals.
var (
	_ route.LLMClient
	_ schema.ModelInfo
	_ mcp.ServerConfig
	_ jsonschema.Schema
	_ session.TurnStore
)

func main() {
	opts := &agent.ClientOptions{
		Credentials: agent.APIKey("key"),
		ModelInfo:   func(string) (schema.ModelInfo, error) { return schema.ModelInfo{}, nil },
		MediaUploader: func(schema.ProviderID) schema.MediaUploader {
			return nil
		},
	}
	_ = opts

	var turns session.TurnStore
	_ = a2aserver.Options{Turns: turns}
	_ = server.Options{}

	tool := agent.DefineTool("noop", "no-op", func(struct{}, agent.ToolContext) (string, error) {
		return "", nil
	})
	_ = tool

	fmt.Println("external probe ok")
}
