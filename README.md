# go-agent-sdk

An embeddable Go SDK for building LLM agents. It originated as a Go port of
[opencode](https://github.com/anomalyco/opencode); the TypeScript/Effect → Go
porting phase is complete, and it is now maintained as a Go-native SDK.
Historical porting notes live in
[`docs/PORTING-REFERENCE.md`](docs/PORTING-REFERENCE.md).

`go-agent-sdk` packages a complete agent runtime behind a small public API: a
multi-protocol LLM layer, the tool-calling agent loop, a permission system,
subagents, MCP, and A2A. State lives in SQLite and everything is driven from Go
iterators, so it embeds cleanly in a CLI, a service, or a test.

## Features

- **Multi-protocol LLM layer** — OpenAI Chat, OpenAI Responses, OpenAI-compatible
  Chat, Anthropic Messages, Gemini, and Bedrock Converse, selected by protocol id.
- **Agent loop** — streaming tool-calling loop with step management, context
  overflow recovery, and steer/queue input.
- **Built-in tools** — `read`, `write`, `edit`, `apply_patch`, `bash`, `glob`,
  `grep`, `webfetch`, `question`, `skill`, `todowrite`.
- **Permissions** — rule-based allow/deny/ask with per-session and per-agent rules.
- **Subagents** — nested `task` delegation into independent child sessions.
- **Agents & skills from markdown** — loaded from explicit config directories.
- **MCP integration** — native Model Context Protocol client (local and remote).
- **A2A protocol** — expose agents over A2A (`a2a/server`) and call remote agents
  (`a2a/client`), including human-in-the-loop resume.
- **Durable sessions** — snapshots, revert/redo, automatic compaction, optional
  automatic titles.
- **Execution seam & sandboxing** — `ClientOptions.Process` is the single injection
  point for every command the tools spawn (`bash`, `ripgrep`, `git`), so a host can
  run them under an OS sandbox; `FileWriteGuard` extends that boundary to file
  mutations. `cmd/agentd` ships optional `bwrap`/`seatbelt`/`appcontainer` wrappers
  and per-project isolation.

## Requirements

Go 1.27 or newer.

## Install

```sh
go get github.com/jiaweisky/go-agent-sdk/pkg/agent
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

func main() {
	ctx := context.Background()

	// Root is required: the SDK never picks a state location for you. Keep it
	// stable — not os.TempDir(), which is shared and subject to pruning.
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:        filepath.Join(home, ".local", "share", "go-agent-sdk"),
		Model:       "gpt-4o-mini",
		Protocol:    "openai-responses",
		Credentials: agent.APIKey(os.Getenv("OPENAI_API_KEY")),
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	session, err := client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	for event, err := range session.Send(ctx, agent.MessageOptions{
		Prompt: "List the Go files in this directory and summarize them.",
	}) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		switch e := event.(type) {
		case agent.TextDelta:
			fmt.Print(e.Delta)
		case agent.ToolCalled:
			fmt.Printf("\n[tool: %s]\n", e.Name)
		}
	}
}
```

`Root` is mandatory by design: the SDK does not read `XDG_*` or other host
environment to locate state, so every instance is explicit and isolated.

## Public API

The public API lives in `pkg/agent` (`import "github.com/jiaweisky/go-agent-sdk/pkg/agent"`); internal engine code is under `internal/`.

| Type / function | Purpose |
|---|---|
| `agent.NewClient(*ClientOptions)` | Build a client and its backing runtime |
| `Client.CreateSession` / `ResumeSession` | Start or resume a durable session |
| `Client.RegisterAgent` | Register a named agent |
| `Client.RegisterTools` | Add application tools at runtime |
| `Session.Send(ctx, MessageOptions)` | Run a turn, streaming `agent.Event`s |
| `Session.On(func(Event))` | Observe events out of band |
| `Session.Steer` / `Abort` / `Compact` | Interrupt, steer, or compact a turn |
| `agent.DefineTool(name, desc, handler)` | Define a typed tool from a Go function |
| `SessionConfig.Hooks` | Intercept tool and session lifecycle |
| `SessionConfig.Permissions` / `OnPermissionRequest` | Permission rules and prompts |

`ClientOptions` is where you set the state `Root`, working directory, config
directories, the default `Model`/`Protocol`/`Credentials`, MCP servers, plugins,
references, compaction/title behavior, and the execution seams (`Process`,
`FileWriteGuard`, `SearchExcludes`).

## Protocols and providers

Select a wire protocol with `ClientOptions.Protocol`:

| Protocol id | Auth |
|---|---|
| `openai-responses` | bearer |
| `openai-chat` | bearer |
| `openai-compatible-chat` | bearer |
| `anthropic-messages` | `x-api-key` |
| `gemini` | `x-goog-api-key` |
| `bedrock-converse` | SigV4 |

To supply catalog metadata (endpoint, context/output limits, headers, variants),
set `ClientOptions.ModelInfo` — the SDK's narrow replacement for a models.dev
catalog — built with `agent.NewModelInfo`. Context/output limits enable automatic
compaction. `llm/provider` also offers convenience facades (openai, anthropic,
openrouter, xai, azure, cloudflare, github_copilot, google, amazon_bedrock) for
callers that use the `llm` package directly.

## Custom tools

```go
type statusInput struct {
	ID string `json:"id" jsonschema:"Identifier to look up"`
}

status := agent.DefineTool("status_lookup", "Look up a status value",
	func(in statusInput, _ agent.ToolContext) (string, error) {
		return fmt.Sprintf("status for %s: READY", in.ID), nil
	})

session, _ := client.CreateSession(ctx, &agent.SessionConfig{
	Agent: "build",
	Tools: []agent.Tool{status},
})
```

The input struct's JSON Schema is derived from its fields and `jsonschema` tags,
and is validated before the handler runs.

## MCP and A2A

- **MCP** — declare servers in `ClientOptions.MCP`; their tools are exposed to
  every session (`mcp` package).
- **A2A (server)** — register an agent with `Expose`, build a handler with
  `agent.NewA2AFactory`, and serve it with `a2a/server`.
- **A2A (client)** — set an agent's `Backend` to the A2A backend to delegate to a
  remote agent; the same path is used by the `task` tool.
- **Server command** — `cmd/agentd` serves one agent client per configured
  server over HTTP/SSE and/or A2A, from a single YAML config (expanded first:
  `$VAR` / `${VAR}` / `${VAR:-default}`; an unset variable without a fallback is
  an error; no implicit environment reads), with an optional control API
  that creates isolated project instances at runtime and per-server command
  sandboxing / isolation. `cmd/agentd-supervisor` wraps that control API for a
  host's `supervisor_command`. See [`docs/DESIGN-AGENTD.md`](docs/DESIGN-AGENTD.md)
  and [`docs/DESIGN-ISOLATION.md`](docs/DESIGN-ISOLATION.md).

## Package layout

```
go-agent-sdk/
├── pkg/          public API (importable by external code)
│   ├── agent/      high-level Client/Session API
│   ├── schema/     shared types
│   ├── llm/        LLM protocol layer
│   │   ├── schema/   request/event/error types
│   │   ├── route/    Route, Protocol, transport, auth
│   │   ├── protocols/ openai-chat, openai-responses, anthropic-messages, gemini, bedrock-converse
│   │   └── provider/ provider facades
│   ├── core/       core engine types (permission, agent, project, event) and
│   │               the public tool / skill / systemcontext packages
│   ├── mcp/        MCP client and tool adapter
│   ├── a2a/        A2A protocol: client, a2ago (a2a-go adapter), server
│   ├── server/     language-agnostic HTTP/SSE API
│   ├── session/    public session-store interfaces (e.g. TurnStore)
│   └── jsonschema/ typed JSON Schema AST for tool schemas
├── internal/     engine internals (not importable outside this module)
│   ├── core/       session, session/runner, database, effect, util, project,
│   │               reference, ripgrep, installation, instructions, wire
│   ├── defs/       config and skill definitions loaded from markdown
│   └── llm/        LLM internals (utils, protocols/utils)
└── cmd/          commands (cmd/agentd: HTTP/SSE + A2A + control API;
                  cmd/agentd-supervisor: host supervisor_command)
```

> The public API is `pkg/...`; everything under `internal/` is implementation
> detail and cannot be imported by other modules. An external probe module in
> `testdata/external` asserts the boundary.


## Examples

Runnable examples live under `examples/` — see [examples/README.md](examples/README.md).

| Example | What it shows |
|---|---|
| `examples/tool` | A custom tool plus a permission handler |
| `examples/chat` | Interactive multi-turn chat, MCP, A2A, skills, subagents |
| `examples/a2a-embedded` | The SDK's A2A library API in-process: multi-turn + HITL with a stub model |
| `examples/agentd-client` | A client for `cmd/agentd` over both HTTP/SSE and A2A |
| `examples/adka2a` | A Google ADK agent over A2A for cross-implementation interop |

```sh
export OPENAI_API_KEY=sk-...
go run ./examples/tool
```

## License

MIT — see [LICENSE](LICENSE). This software is a source port of opencode, whose
MIT notice is retained in [LICENSE-opencode](LICENSE-opencode). Third-party
attributions are in [NOTICE](NOTICE).
