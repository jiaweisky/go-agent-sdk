package main

import (
	"os"
	"path/filepath"

	"github.com/jiaweisky/go-agent-sdk/examples/internal/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
)

// weatherMCP configures the built-in weather MCP server (stdio). The SDK
// spawns `go run ./examples/weathermcp` itself and owns the child process —
// the local-server counterpart of the notes server, which chat launches over
// streamable HTTP. AMAP_API_KEY is passed through; without a key the server
// still starts and its tools report the missing configuration.
//
// AMAP_API_KEY is a Web Service key from https://lbs.amap.com/ — register,
// then in the console create an application (Console -> Application
// Management -> Create Application) and add a Key of the Web Service type to
// it. Set it in the environment or in .env next to the other provider keys.
func weatherMCP() []agent.MCPConfig {
	root := filepath.Dir(filepath.Dir(util.ExampleDir())) // examples/chat -> examples -> repo root
	return []agent.MCPConfig{{
		Name: "weather",
		Server: mcp.ServerConfig{
			Type:        "local",
			Command:     []string{"go", "run", "./examples/weathermcp"},
			Cwd:         root,
			Environment: map[string]string{"AMAP_API_KEY": os.Getenv("AMAP_API_KEY")},
		},
	}}
}
