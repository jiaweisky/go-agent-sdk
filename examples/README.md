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

Commands: `/skills`, `/agents`, `/mcps`, `/mcp`, `/task [<#n>]`, `/compact` (summarizes
the session history on demand, via `Session.Compact`), `/tool` (adds
or removes a runtime tool via `Client.RegisterTools`, including the typed
`convert_length` and the large `emit_lines`), `/tool-output` (reports or prunes
the SDK's spilled tool-output files), `/attach` (attaches an image to the next
message via `MessageOptions.Files`), `/todos` (shows the model's task list from
`todo.updated` events), `/diff` (shows the last turn's file diffs from snapshot
comparison), `/undo` and `/redo` (revert and restore the last turn's file
changes via `Session.RevertStage`/`RevertClear`), `/plugin` (unloads
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

## Not ported

`copilot-sdk/go/samples/manual_tool_resume` is not ported: it relies on
copilot-runtime features the SDK does not have (declaration-only tools executed
externally, `ForceStop`, `ResumeSession(ContinuePendingWork)`, and RPC
`HandlePendingPermissionRequest`/`HandlePendingToolCall`). The `tool` example
covers the closest equivalent (custom tool + permission callback).
