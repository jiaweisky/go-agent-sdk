# Examples

Runnable examples for the go-agent-sdk high-level `agent` package.

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `AGENT_MODEL` | Model id (required; provider-specific, so there is no default) | — |
| `AGENT_PROVIDER` | Provider type: `openai`, `anthropic`, `openai-compatible`, or `gemini` | `openai` |
| `AGENT_PROVIDER_ID` | Provider id for `openai-compatible` (e.g. `deepseek`) | `openai-compatible` |
| `AGENT_BASE_URL` | Provider base URL | provider default |
| `AGENT_API_KEY` | Provider API key (falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`) | — |

Example:

```sh
export AGENT_PROVIDER=openai
export AGENT_API_KEY=sk-...
export AGENT_MODEL=gpt-4o
```

For an OpenAI-compatible endpoint:

```sh
export AGENT_PROVIDER=openai-compatible
export AGENT_PROVIDER_ID=deepseek
export AGENT_BASE_URL=https://api.deepseek.com/v1
export AGENT_API_KEY=...
export AGENT_MODEL=deepseek-chat
```

## chat

Interactive multi-turn chat. Streams reasoning/tool notifications and prints the
assistant reply. State goes to `examples/chat/.data` (git-ignored) and the
sample agents and skills come from the committed `examples/chat/.config`; a
`.config` in the working directory is scanned after it and overrides them.

```sh
go run ./examples/chat
```

Flags: `-C <dir>` runs as if started there and `-yolo` skips permission
prompts. Model token limits and pricing come from the demo's model catalog
(`examples/chat/model.go`), keyed by `AGENT_MODEL`; the limits enable automatic
compaction.

Commands: `/skills`, `/agents`, `/mcps`, `/task [<#n>]`, `/compact` (summarizes
the session history on demand, demonstrating `Session.Compact`), `/tool` (adds
or removes a demo tool at runtime via `Client.RegisterTools`), `/reference`
(lists or adds named reference sources injected into the model's system
context), `/persona <text>` (starts a new session with an appended system prompt
via `SessionConfig.SystemPrompt`), and `/history` (prints the projected session
history via `Session.Messages`).

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
