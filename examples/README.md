# Examples

Runnable examples for the go-agent-sdk high-level `agent` package.

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `OPENCODE_PROVIDER` | Provider type: `openai`, `anthropic`, or `openai-compatible` | `openai` |
| `OPENCODE_PROVIDER_ID` | Provider id for `openai-compatible` (e.g. `deepseek`) | `openai-compatible` |
| `OPENCODE_BASE_URL` | Provider base URL | provider default |
| `OPENCODE_API_KEY` | Provider API key | — |
| `OPENCODE_MODEL` | Model id | `gpt-4o` |

Example:

```sh
export OPENCODE_PROVIDER=openai
export OPENCODE_API_KEY=sk-...
export OPENCODE_MODEL=gpt-4o
```

For an OpenAI-compatible endpoint:

```sh
export OPENCODE_PROVIDER=openai-compatible
export OPENCODE_PROVIDER_ID=deepseek
export OPENCODE_BASE_URL=https://api.deepseek.com/v1
export OPENCODE_API_KEY=...
export OPENCODE_MODEL=deepseek-chat
```

## chat

Interactive multi-turn chat. Streams reasoning/tool notifications and prints the
assistant reply.

```sh
go run ./examples/chat
```

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
