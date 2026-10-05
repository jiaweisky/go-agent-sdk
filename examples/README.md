# Examples

Runnable examples for the go-agent-sdk high-level `agent` package.

## Environment variables

The examples read provider API keys from the environment (or a `.env`, loaded by
`util.LoadDotEnv`). A model is offered only when its provider key is set; the
shared model catalog (`examples/internal/util/catalog.go`) lists the selectable
models and their provider, protocol and endpoint.

| Variable | Description | Default |
|---|---|---|
| `OPENAI_API_KEY` | OpenAI API key | — |
| `ANTHROPIC_API_KEY` | Anthropic API key | — |
| `DEEPSEEK_API_KEY` | DeepSeek API key | — |
| `WEATHER_MCP` | chat: path to the weather MCP server entry script | — |
| `AMAP_API_KEY` | chat: AMap key used by the weather MCP server | — |

Example:

```sh
export DEEPSEEK_API_KEY=sk-...
go run ./examples/chat
```

## chat

Interactive multi-turn chat. Streams reasoning/tool notifications and prints the
assistant reply. State goes to `examples/chat/.data` (git-ignored) and the
sample agents and skills come from the committed `examples/chat/.config`
(`agent/*.md` are subagents, `mode/*.md` are primary agents); a `.config` in the
working directory is scanned after it and overrides them. Locale-aware ordering
(nil `Locale` uses `LC_ALL`/`LC_COLLATE`/`LANG`) affects lists like `/agents`.

```sh
go run ./examples/chat
```

Flags: `-C <dir>` runs as if started there and `-yolo` skips permission
prompts. Model token limits and pricing come from the shared model catalog
(`examples/internal/util/catalog.go`); the limits enable automatic compaction.

The chat starts a small notes MCP server (`examples/notesmcp`) over streamable
HTTP (spawned with `go run` and killed on exit); `/mcp` lists and fetches its
prompts and resources, and `/mcps` shows each server's instructions.

It also starts a Google ADK (adk-go) agent served over A2A (`examples/adka2a`,
a **separate Go module** so the SDK never depends on adk-go) and registers it as
the `echo` agent. Select it with `/agent echo` (or delegate to it with the `task`
tool): the SDK reaches it through the same A2A wire protocol, exercising
cross-implementation interoperability. `go test ./examples/chat -run
TestAdkA2AInterop` is the interop test (skipped under `-short`).

Commands: `/skills`, `/agents`, `/mcps`, `/mcp`, `/task [<#n>]`, `/compact` (summarizes
the session history on demand, via `Session.Compact`), `/tool` (adds
or removes a runtime tool via `Client.RegisterTools`, including the typed
`convert_length`, the large `emit_lines` and the progress-reporting `count_to`),
`/tool-output` (reports or prunes
the SDK's spilled tool-output files), `/attach` (attaches an image to the next
message via `MessageOptions.Files`), `/todos` (shows the model's task list from
`todo.updated` events), `/diff` (shows the last turn's file diffs from snapshot
comparison), `/undo` and `/redo` (revert and restore the last turn's file
changes via `Session.RevertStage`/`RevertClear`), `/async` (holds
permission/question requests for an out-of-band reply via `Client.Permission`/
`Client.Question`, resolved with `/permissions`/`/allow`/`/deny` and
`/questions`/`/answer`/`/reject`), `/plugin` (unloads
a plugin, running its cleanup), `/reference` (lists or adds local and git
reference sources injected into the model's system context), `/persona <text>`
(starts a new session with an appended system prompt via
`SessionConfig.SystemPrompt`), `/rename <title>` (renames the current session
via `Session.SetTitle`), `/fork [#n]` (forks the session into a new one via
`Client.ForkSession`, optionally before user message `#n`), and `/history`
(prints the projected session history via `Session.Messages`).

Subagents may nest: the example sets `SubagentDepth: 2`, so a task can start
another task and both appear in the `/task` list.

Keys: Ctrl+C (or Ctrl+D on an empty line) exits; Esc interrupts a running turn
(press it twice, like opencode) without leaving the chat.

## tool

Registers a custom tool with `agent.DefineTool` and a permission handler, sends
one prompt, and prints the streamed reply.

```sh
go run ./examples/tool
```

## adka2a

A Google ADK (`google.golang.org/adk`) agent served over A2A. It is its own Go
module so the SDK's dependency graph never includes adk-go; the chat example
spawns it as a child process and connects to it through the SDK's A2A client.
Its model is a deterministic stub, so it needs no provider credentials.

```sh
cd examples/adka2a && go run . --http 127.0.0.1:9999
```

The agent card is served at `/.well-known/agent-card.json` and the JSON-RPC
endpoint at `/invoke`. This example is the cross-implementation peer used to
verify the wire conventions in `docs/DESIGN-A2A.md`.

## a2aserver

A standalone example of the SDK as an A2A **server**. `examples/a2aserver` exposes
a local agent (via `a2a/server` + `NewA2AFactory` + `Server.Handler`) and then
calls it with the SDK's own A2A client, all in one process, showing the half the
chat example only shows from the client side:

- **multi-turn context** — successive prompts share one A2A context, so the
  exposed agent sees the earlier turns;
- **human-in-the-loop** — the agent asks a question, the caller answers, and the
  turn resumes (A2A `input-required`).

The agent uses a deterministic, credential-free stub model.

```sh
go run ./examples/a2aserver    # or: make a2aserver
```

It is interactive: it starts the A2A server, then reads prompts from stdin and
prints the replies. Successive prompts share one context, so context persists
across turns; type a prompt containing "ask" to make the agent ask you a
question and answer it. Ctrl+D exits (the server also keeps listening for
external A2A clients). `go test ./examples/a2aserver` drives the same flow.

## Not ported

`copilot-sdk/go/samples/manual_tool_resume` is not ported: it relies on
copilot-runtime features the SDK does not have (declaration-only tools executed
externally, `ForceStop`, `ResumeSession(ContinuePendingWork)`, and RPC
`HandlePendingPermissionRequest`/`HandlePendingToolCall`). The `tool` example
covers the closest equivalent (custom tool + permission callback).
