# go-agent-sdk design document

> ⚠️ **The Go code in this document is illustrative only**, expressing the architecture and patterns; it does not represent the final implementation. Every function signature, type definition and error-handling detail is governed by opencode's TypeScript source. When porting, compare against the full logic of the corresponding opencode file.

## 1. Project overview

go-agent-sdk is a Go port of [opencode](https://opencode.ai), aiming to provide a complete, embeddable Agent SDK. Core capabilities include:

- **LLM protocol layer** — supports OpenAI Chat / OpenAI Responses / Anthropic Messages / Gemini / Bedrock Converse and more (plus openai-compatible chat); callers choose by **protocol id** (`ClientOptions.Protocol`), and all six protocols are reachable through the `llm/protocols` registry; the `llm/provider/` facades are only a convenience layer with preset defaults (see §5).
- **Agent loop** — an automated tool-calling agent loop supporting step management, context-overflow recovery, and steer/queue input.
- **Agent definitions** — configurable agent metadata, system prompt, permission rules, and model override.
- **Skill** — load specialized knowledge from markdown files and inject it into the system context.
- **MCP integration** — a native Model Context Protocol client supporting local and remote MCP servers.
- **Built-in tools** — read, write, edit, apply_patch, bash, glob, grep, webfetch, question, skill, todowrite (11, see §7.4); plus `task` (subagent delegation, see §6.3).
- **Permission system** — a rule-based allow/deny/ask permission model.
- **Subagent** — recursively create independent child sessions via the task tool within an agent.

---

## 2. MVP scope

Based on the above design, the first version (MVP) implements the following core loop, supporting end-to-end agent conversations:

### Included

| Module | Content | Note |
|---|---|---|
| `llm/schema/` | LLMRequest, LLMEvent, the LLMClient interface, Model, options, ids, errors | Canonical type definitions |
| `llm/route/` | Route + Protocol + Transport + Framing + Auth + Endpoint | LLM protocol-layer core |
| `llm/protocols/` | OpenAI Chat / Responses, Anthropic Messages, Bedrock Converse, Gemini | Per-provider wire protocols |
| `llm/provider/` | Provider Facades (openai, anthropic, openrouter, ...) | Preset defaults |
| root package `agent` (implemented) | high-level public API (Client/Session/Send/On/DefineTool/Hooks) | Assembles core/session + llm + tool |
| `core/tool/` | ToolRegistry + `registry.go` / `tool.go` | Registration, lookup, permission filtering |
| Built-in tools | 11 (`core/tool/`, see §7.4) | read/write/edit/apply_patch/bash/glob/grep/webfetch/question/skill/todowrite |
| `core/permission.go` | Rule engine (allow / deny / ask) | Permission evaluation |
| `core/` | file-mutation / location-mutation / git / database / process ... | Supporting modules |
| root package `agent` | `NewClient` / `CreateSession` / `Send` / `On` / `RegisterAgent` | Public API (implemented, see §11) |
| `schema/` | Base types | JSON Schema, ID generation |

### Not included (deferred to later versions)

| Module | Reason for deferral |
|---|---|
| `lsp` tool | Not on the core path; to be added later |
| Provider facade (preset base URLs for OpenRouter / xAI / Azure / Cloudflare / GitHub Copilot / Google / Bedrock) | Protocols and facades are implemented; the `agent` layer uses `Protocol` + `ModelInfo`, and facades are only for callers using the `llm` package directly (see §5.4) |
| `bash` advanced features (tree-sitter, arity, background tasks, progress stream) | See the TODO in `core/tool/bash.go` |
| `edit` advanced features (fuzzy match, formatter, watcher, snapshot, LSP) | See the TODO in `core/tool/edit.go` |
| credential / catalog / integration services | CLI infrastructure; the SDK uses narrow input instead (see AGENTS.md) |
| `weak` package, `os.Root` and other security features | To be hardened later |

> `apply_patch` supports neither moves nor atomic rollback because v2 core does not either — `packages/core/src/tool/apply-patch.ts` rejects moves and says "Moves and atomic rollback are not supported yet". The Go port matches it, so this is parity, not a deferred feature.

> Implemented (previously listed as "deferred" in early documents, now landed): MCP integration (§8), Skill system (§9), Config frontmatter loading (`defs/config`), context compaction (§6), Steer/Queue input, Plugin (narrowed, §7.3), Reference service, `task` subagents (§6.3).

### MVP user code example

```go
client, err := agent.NewClient(&agent.ClientOptions{
    Model:       "gpt-4o",
    Protocol:    "openai-responses",           // protocol id
    Credentials: agent.APIKey(os.Getenv("OPENAI_API_KEY")),
    // Optional: catalog metadata (endpoint/limits/headers). If unset, the protocol route defaults are used.
    ModelInfo: func(id string) (schema.ModelInfo, error) {
        return agent.NewModelInfo("openai-responses", id, "https://api.openai.com/v1",
            schema.ModelLimit{Context: 128_000, Output: 8_192}) // enables automatic compaction
    },
})

err = client.RegisterAgent("dev", agent.AgentConfig{
    System: "You are a helpful assistant.",
})

session, err := client.CreateSession(ctx, &agent.SessionConfig{
    Agent: "dev",
    Tools: []agent.Tool{readTool, writeTool, editTool, shellTool},
})

for event, err := range session.Send(ctx, agent.MessageOptions{Prompt: "Read main.go, fix the bug, and verify"}) {
    if err != nil {
        break
    }
    switch e := event.(type) {
    case agent.TextDelta:   os.Stdout.WriteString(e.Delta)
    case agent.ToolCalled:  log.Printf("calling %s", e.Name)
    }
}
```

---

## 3. Architecture overview

The Go directories directly mirror opencode's TS monorepo package structure (see §4). Dependency direction:

```
┌────────────────────────────────────────────────────────────────┐
│  root package agent      ← high-level public API (implemented)  │
│  High-level API (Agent, Session, Prompt, Stream)               │
└───────────────────────────┬────────────────────────────────────┘
                            │ depends on
┌───────────────────────────v────────────────────────────────────┐
│  Agent loop (loop / turn / session / context / steer / compact) │
└──────┬──────────────┬──────────────┬──────────────┬────────────┘
       │              │              │              │
┌──────v───────┐ ┌────v─────┐ ┌──────v─────┐ ┌──────v───────────┐
│ core/tool/   │ │ mcp/     │ │ skill/     │ │ core/permission  │
│ Registry     │ │ Client   │ │ Loader     │ │ Ruleset / assert │
│ write, ...   │ │ (impl.)  │ │ (impl.)    │ │                  │
└──────┬───────┘ └──────────┘ └────────────┘ └──────────────────┘
       │ depends on
┌──────v─────────────────────────────────────────────────────────┐
│  llm/                                                           │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ llm/provider/   Provider Facades                          │ │
│  │                 OpenAI / Anthropic / OpenRouter / ...     │ │
│  │                        ↓ uses                             │ │
│  │ llm/route/      Route = Protocol + Endpoint + Auth        │ │
│  │                        + Transport + Framing              │ │
│  │                        ↓ composes                         │ │
│  │ llm/protocols/  openai_chat / openai_responses /          │ │
│  │                 anthropic_messages / bedrock_converse /   │ │
│  │                 gemini  (+ utils/)                        │ │
│  │                        ↓ uses                             │ │
│  │ llm/route/transport/   HTTP │ SSE                        │ │
│  └───────────────────────────────────────────────────────────┘ │
│                                                                 │
│  llm/schema/    LLMRequest / LLMEvent / LLMError / ID          │
└─────────────────────────────────────────────────────────────────┘
       │
┌──────v─────────────────────────────────────────────────────────┐
│  schema/        ID generation, Location / Project / Permission  │
└─────────────────────────────────────────────────────────────────┘
```

### Dependency direction

```
agent ──> core/tool ──┐
          mcp ────────┤
          skill ──────┼──> llm ──> llm/schema ──> schema
          core/permission ─┘
```

- `core/` provides the tool framework (tool / permission / file-mutation / location-mutation) and infrastructure (git / database / process / fs-util)
- `llm/` provides the protocol layer; `llm/schema` holds the LLM canonical types
- `schema/` holds cross-domain shared types (ID, Location, Project, Permission)
- `mcp/` wraps the official Go SDK `github.com/modelcontextprotocol/go-sdk` (local stdio / remote StreamableHTTP→SSE fallback) and does not involve LLM concepts; the adapter layer in `mcp/tool.go` converts an MCP Tool into an LLM-consumable ToolDefinition

## 4. Package structure

Go directories **directly mirror opencode's TS monorepo package structure**: `packages/<pkg>/src/<path>` → `<pkg>/<path>` (kebab-case → snake_case), without using `pkg/` or `internal/`. The full conventions (symbol prefixes, circular-dependency handling, index.ts rules) are in `AGENTS.md`.

```
go-agent-sdk/                            opencode TS
├── *.go            module root package agent (no corresponding TS dir; modeled on copilot-sdk/go)
│   ├── client.go / session.go / runtime.go    Client/Session/runtime assembly
│   ├── task.go / task_run.go                   task (subagent)
│   ├── events.go / hooks.go / permission.go    events/hooks/permissions
│   └── ...
├── llm/            ← packages/llm/src/          (@opencode-ai/llm)
│   ├── llm.go                       package entry, LLM re-exports (llm.ts)
│   ├── tool.go                      LLM-layer Tool definition (tool.ts)
│   ├── tool_runtime.go              tool runtime orchestration (tool-runtime.ts)
│   ├── cache_policy.go              cache policy (cache-policy.ts)
│   ├── provider.go                  Provider type definitions (provider.ts)
│   ├── provider_error.go            provider-error.ts (re-exports llm/schema's implementation)
│   ├── schema/                      LLM canonical types (schema/)
│   │   ├── ids.go                   ProviderID, ModelID, ProtocolID, FinishReason, MessageRole
│   │   ├── events.go                LLMEvent (events.ts)
│   │   ├── messages.go              LLMRequest, LLMResponse, Message (messages.ts)
│   │   ├── options.go               GenerationOptions (options.ts)
│   │   ├── errors.go                LLMError + ProviderError (errors.ts)
│   │   └── provider_error.go        the canonical IsContextOverflow implementation (shared by llm and llm/route)
│   ├── route/                       Route layer (route/)
│   │   ├── client.go                Route + LLMClient (route/client.ts)
│   │   ├── transport.go             Transport / TransportRuntime / TransportPrepareInput
│   │   ├── protocol.go              Protocol (route/protocol.ts)
│   │   ├── endpoint.go              Endpoint (route/endpoint.ts)
│   │   ├── auth.go                  Auth (route/auth.ts)
│   │   ├── auth_options.go          AuthOptions (route/auth-options.ts)
│   │   ├── framing.go               Framing SSE (route/framing.ts)
│   │   ├── executor.go              RequestExecutor (route/executor.ts)
│   │   └── transport/               HttpTransport (route/transport/)
│   ├── protocols/                   per-provider wire protocols (flat, symbols prefixed by protocol)
│   │   ├── openai_chat.go           protocols/openai-chat.ts
│   │   ├── openai_responses.go      protocols/openai-responses.ts
│   │   ├── anthropic_messages.go    protocols/anthropic-messages.ts
│   │   ├── bedrock_converse.go      protocols/bedrock-converse.ts
│   │   ├── bedrock_event_stream.go  protocols/bedrock-event-stream.ts
│   │   ├── gemini.go                protocols/gemini.ts
│   │   ├── shared.go                protocols/shared.ts
│   │   └── utils/                   protocols/utils/
│   ├── provider/                    Provider Facades (providers/)
│   └── utils/                       general utilities (utils/record.ts)
├── core/           ← packages/core/src/          (@opencode-ai/core)
│   ├── schema.go                    re-exports (schema.ts)
│   ├── global.go                    Global (global.ts)
│   ├── provider.go                  ProviderV2 re-exports (provider.ts)
│   ├── model.go                     ModelV2 re-exports + parse (model.ts)
│   ├── file_mutation.go             file-mutation.ts
│   ├── location_mutation.go         location-mutation.ts
│   ├── location.go                  location.ts
│   ├── permission.go                permission.ts
│   ├── project.go                   project.ts
│   ├── file.go                      file.ts
│   ├── fs_util.go                   fs-util.ts
│   ├── process.go                   process.ts
│   ├── git.go                       git.ts
│   ├── database/                    SQLite + migrations (database/)
│   ├── effect/                      Effect runtime utilities (effect/keyed-mutex.ts, ...)
│   ├── project/                     project/schema.ts, project/directories.ts
│   ├── tool/                        tool framework (tool/)
│   │   ├── tool.go                  tool.ts
│   │   ├── registry.go              registry.ts
│   │   ├── read.go / read_filesystem.go  read.ts / read-filesystem.ts
│   │   ├── write.go                 write.ts
│   │   ├── edit.go                  edit.ts
│   │   └── bash.go                  bash.ts
│   └── util/                        general utilities (util/wildcard.ts, util/hash.ts, util/markdown.ts)
│       └── markdown.go              ConfigMarkdown (frontmatter parsing, config/markdown.ts)
├── a2a/            A2A (Agent2Agent) protocol support (no TS counterpart; Go-specific; see DESIGN-A2A.md)
│   ├── client/     outbound thin interface (SDK's own types, does not leak a2a-go)
│   ├── a2ago/      outbound a2a-go adapter (the only outbound layer importing a2a-go)
│   └── server/     inbound: AgentExecutor + taskstore.Store + AgentCard + http.Handler
├── opencode/       ← packages/opencode/src/      (opencode CLI)
│   ├── config/                      content config loading (agent/mode markdown)
│   │   ├── agent.go                 config/agent.ts (LoadAgentEntries)
│   │   ├── agent_info.go            the AgentInfo type from config/agent.ts
│   │   ├── entry_name.go            config/entry-name.ts
│   │   └── agent_apply.go           the application logic of config/plugin/agent.ts
│   ├── skill/                       skill discovery (discoverSkills in skill/index.ts)
│   │   └── skill.go                 DiscoverSkills (scans only the given directories)
│   └── id/                          prefixed monotonic ID generation (id/id.ts)
└── schema/         ← packages/schema/src/        (@opencode-ai/schema)
    ├── schema.go                    schema.ts
    ├── identifier.go                identifier.ts
    ├── event.go                     event.ts
    ├── session_event.go             session-event.ts (includes the durable/live-only list)
    ├── session_message*.go          session-message.ts + JSON decoding
    ├── agent.go / permission.go / skill.go / location.go / project.go
    ├── model.go / provider.go / question.go / prompt.go / ...
    └── v1/                          legacy SessionV1 event shapes
```

> The above is only a **representative** tree, not the complete list (`schema/` has about 42 files, `core/` has many more modules).

> Implemented: the module root package `agent` public API, the core/session main loop, 12 built-in tools + `task`, Skill, MCP, Plugin (narrowed), the Reference service. Not yet ported: the `lsp` tool.

> **A2A (Agent2Agent)**: a Go-specific capability (no TS counterpart); see `docs/DESIGN-A2A.md` for design and implementation; inbound/outbound, HITL, the `turn` first-class entity, and the Phase 5 productionization components have all landed.

> **Database**: `core/database` uses `database/sql` + modernc SQLite. The pool is `MaxOpenConns=4`, per-connection pragmas (`journal_mode=WAL`/`synchronous`/`busy_timeout`/`cache_size`/`foreign_keys`) are injected via the DSN, and `_txlock=immediate` makes every transaction write serially with BEGIN IMMEDIATE; WAL allows concurrent reads, improving multi-session throughput (writes remain serial, reporting busy past `busy_timeout`).

---

## 5. LLM layer detailed design

### 5.1 Route — the core abstraction

A Route represents one concrete API deployment: 1 URL + 1 auth method + 1 wire protocol + 1 stream framing.

```go
// llm/route/client.go
type Route struct {
    ID               string
    Provider         schema.ProviderID
    Protocol         schema.ProtocolID
    Endpoint         Endpoint
    Auth             *Auth
    Defaults         RouteDefaults
    Body             RouteBody
    Transport        Transport
    With             func(patch RoutePatch) Route
    Model            func(input RouteModelInput) schema.Model
    PrepareTransport func(body any, request *schema.LLMRequest) (any, error)
    StreamPrepared   func(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

// Build the request body: LLMRequest → Provider-native body
type RouteBody struct {
    Schema schema.JsonSchema
    From   func(request *schema.LLMRequest) (any, error)
}
```

A Route is composed of four orthogonal layers:

```
Route = Protocol + Endpoint + Auth + Framing
```

```go
// Build a Route from a ready Transport (used by protocols/*.go)
func MakeFromTransport(input MakeTransportInput) Route
```

> TS's `Route<Body, Prepared>` generic is erased with `any` in Go (heterogeneous collections require it).

### 5.2 Protocol

A Protocol encapsulates the semantics of one provider-native API: body construction, body schema, and the streaming event state machine.

```go
// llm/route/protocol.go
type Protocol struct {
    ID     schema.ProtocolID
    Body   ProtocolBody
    Stream ProtocolStream
}

type ProtocolBody struct {
    // Protocol body contract (jsonschema AST); compiled when the route is built, compile() validates each body.
    Schema *jsonschema.Schema
    From   func(request *schema.LLMRequest) (any, error)
}

type ProtocolStream struct {
    DecodeEvent func(frame string) (any, error)
    Initial     func(request *schema.LLMRequest) any
    Step        func(state any, event any) (any, []schema.LLMEvent, error)
    Terminal    func(event any) bool
    OnHalt      func(state any) []schema.LLMEvent
}
```

> **Body validation**: each protocol defines its `Schema` in `llm/protocols/body_schema.go` (aligned with TS's Effect body schema); `MakeFromTransport` compiles the `Validation()` projection with `jsonschema.CompileValidation`, and `llm.Client.compile` validates the marshalled body after `Body.From`, returning `InvalidRequestReason` if invalid (aligned with TS `ProviderShared.validateWith`). This layer caught several lowering deviations (e.g. `messages: null` instead of `[]`, `parameters: null`).

### 5.3 LLMEvent (streaming events)

```go
// llm/schema/events.go
type LLMEventType string

const (
    EventStepStart      LLMEventType = "step-start"
    EventTextDelta      LLMEventType = "text-delta"
    EventReasoningDelta LLMEventType = "reasoning-delta"
    EventToolInputStart LLMEventType = "tool-input-start"
    EventToolInputDelta LLMEventType = "tool-input-delta"
    EventToolCall       LLMEventType = "tool-call"
    EventToolResult     LLMEventType = "tool-result"
    EventToolError      LLMEventType = "tool-error"
    EventStepFinish     LLMEventType = "step-finish"
    EventFinish         LLMEventType = "finish"
    EventProviderError  LLMEventType = "provider-error"
)

type LLMEvent struct {
    Type   LLMEventType
    ID     string
    Index  int
    Text   string
    Name   string
    Input  any
    Result ToolResultValue
    Output *ToolOutput
    Reason FinishReason
    Usage  *Usage
    Error  error
    // ...
}
```

### 5.4 Provider Facades

> The SDK's `agent` layer **does not use** these facades directly: it looks up the `llm/protocols` registry by `ModelInfo.API.Protocol` (protocol id → route + auth scheme) and applies `schema.Credential` itself. Facades are for callers using the `llm` package directly.

```go
// llm/provider/openai.go —— takes preconfigured routes (see llm/protocols/)
openai := provider.NewOpenAI(responsesRoute, chatRoute, provider.OpenAIConfig{
    APIKey: &apiKey,
})
model := openai.Model("gpt-4o")       // default protocol
chat  := openai.Chat("gpt-4o")        // bound to the Chat protocol
resp  := openai.Responses("gpt-4o")   // bound to the Responses protocol

// llm/provider/anthropic.go
anthropic := provider.NewAnthropic(anthropicMessagesRoute, provider.AnthropicConfig{
    APIKey: &apiKey,
})
model := anthropic.Model("claude-sonnet-4-20250514")
```

### 5.5 LLMClient

```go
// llm/route/client.go
type LLMClient interface {
    Prepare(ctx context.Context, req *schema.LLMRequest) (*schema.PreparedRequest, error)
    Stream(ctx context.Context, req *schema.LLMRequest) (iter.Seq2[schema.LLMEvent, error], error)
    Generate(ctx context.Context, req *schema.LLMRequest) (*schema.LLMResponse, error)
}
```

### 5.6 Protocol implementations

| Protocol | Location | Provider |
|------|------|----------|
| OpenAI Chat | `llm/protocols/openai_chat.go` | OpenAI, DeepSeek, TogetherAI, Cerebras |
| OpenAI Responses | `llm/protocols/openai_responses.go` | OpenAI |
| Anthropic Messages | `llm/protocols/anthropic_messages.go` | Anthropic (Claude) |
| Bedrock Converse | `llm/protocols/bedrock_converse.go` | AWS Bedrock |
| Google Gemini | `llm/protocols/gemini.go` | Google |
| OpenAI Compatible Chat | `llm/protocols/openai_compatible_chat.go` | Generic OpenAI-compatible |

---

## 6. Agent loop

> **Implemented.** Corresponds to opencode `packages/core/src/agent*`, `packages/opencode/src/session/`; implemented in `core/session/` (`run_coordinator.go`, `runner/`, `compaction.go`, `context_epoch.go`, etc.) and the root package's `runtime.go`.

### 6.1 Session Runner main loop

```
SessionRunner.Run(sessionID, force)
  → loop until there is no pending input:
      runTurn(sessionID, input)
        → loop until no continuation is needed:
            1. Load session, agent, model, system context
            2. Assemble the LLMRequest
            3. Check for context compaction
            4. LLMClient.Stream(request) — one provider call
            5. For each tool-call event:
                - Persist a record of the tool call
                - Permission check
                - ToolRegistry.Execute(tool, params, ctx)
                - Publish a tool-result event
            6. Track the step count, terminate if over the limit
            7. If there were tool calls → reload history → provider call again (continuation)
            8. Context overflow → compact and retry
            9. Steer events → promote at the next turn boundary (without interrupting the in-flight provider stream)
      → check queued input → continue the outer loop
```

> Context compaction depends on the model's `Limits` (`ModelInfo.Limit` → route defaults → `compactionModelLimits`). **When `Limits` is unset, automatic compaction is disabled**, and an over-long request fails directly with a provider 400. The SDK does not ship a model catalog; the caller supplies `Context`/`Output` limits as needed. Besides automatic compaction (threshold-triggered) and post-overflow compaction, `Session.Compact(ctx)` provides **manual compaction** (on an idle session, no `Limits` needed, with `reason` recorded as `manual`).

### 6.2 Agent definition

```go
type Agent struct {
    Name        string
    Description string
    Mode        AgentMode       // Primary | SubAgent | All
    Hidden      bool
    Permission  permission.Ruleset
    Model       *ModelOverride  // providerID + modelID
    Prompt      string          // system prompt override
    Steps       int             // max turn steps; 0 = unlimited (consistent with TS, no default)
    Temperature float64
    TopP        float64
}
```

### 6.3 Subagent (TaskTool) — implemented

Corresponds to opencode `packages/opencode/src/tool/task.ts`. `task` is a built-in tool: after the primary agent calls it, another agent runs in a **separate child session** and its result text is returned. Implemented in the root package's `task.go` (tool definition + `DeriveSubagentSessionPermission`) and `task_run.go` (`runtime.runTask`). The child session inherits the parent session's `SessionHooks` and per-session permission/question handler (`runtime.inheritSession`), so `OnPreToolUse`/`OnPostToolUse`/`OnToolDefinition` also take effect for the subagent's tool calls.

**Tool parameters**: `description`, `prompt`, `subagent_type`, `task_id` (resume the same child session), `command`, `background` (not yet supported; returns a failure).

**Execution flow**:
1. Depth check: count child-session depth along `parent_id`; error if it exceeds `ClientOptions.SubagentDepth` (default 1).
2. Permission check: action `task`, resource `subagent_type`.
3. Resolve the agent: `subagent_type` must be a registered agent, otherwise report `Unknown agent type`.
4. Derive permissions: `DeriveSubagentSessionPermission(parent session permissions, child agent)` — inherit the parent's deny rules + `external_directory`, and by default append `deny` for `todowrite` / `task` (unless the child agent itself already declares that action). Write into the child session's `session.permission` column.
5. Create the child session: `parent_id = current session`, `agent = subagent_type`, `permission = derived rules`; if `task_id` is given and the session exists, reuse it (reusing its stored agent and permission).
6. `Admit` + `coordinator.Run(childID)` to run a turn; return the last text segment of the last assistant message from `session_message`. When the tool context is cancelled, `coordinator.Interrupt(childID)`.
7. After finishing, remove the child session's dispatcher / permission registration (keeping the session row so `task_id` can resume).

**How permissions take effect**: `core.PermissionSessionInfo.Permissions` makes `configured` return `merge(agent.permissions, session.permissions)`; the runner's `MaterializeFor` also merges session rules (tool visibility). Both align with TS's `Permission.merge(agent.permission, session.permission)`.

**Tool description**: dynamically injects, via `SubagentTypes(agents, caller permissions)`, the list of agents with `mode != "primary"` whose `task` permission is not denied, aligned with TS `registry.describeTask`.

**Observability**: `runTask` publishes a **durable** `session.next.tool.progress` event on the parent session via the tool's progress callback (`structured` carries `sessionId` / `parentSessionId` / `subagentType` / `description` / `model`), aligned with v2's tool invocation progress (`Tool.Context.Progress`). Subscribing globally with `Client.On` lets you observe the child session's reasoning/text/tool events (`Session.On` filters by session and cannot see child sessions). Event dispatch is uniformly **synchronous and lossless**: `Client.On`/`PluginContext.On`, like `Session.On`/`Session.Send`, are called in order on the publisher's goroutine (a slow observer slows down the publishing turn rather than losing events); observer callbacks should stay lightweight. Handler panics are always recovered (without crashing the process) and can be observed via `ClientOptions.OnPanic`.

**Relationship to `mode`**: an agent with `mode: subagent` (or `all`) appears in the `subagent_type` list; `primary` is only a primary agent.

**Known deviation**: `background` (experimental background subagents) is not implemented and returns a failure when requested (aligned with TS's behavior when the flag is off).

---

## 7. Tool system

### 7.1 Tool definition

```go
// core/tool/tool.go
type Context struct {
    SessionID          string
    Agent              string
    AssistantMessageID string
    ToolCallID         string
    // Publish running-tool state (durable session.next.tool.progress)
    Progress           func(structured map[string]any, content []Content) error
}

// Generic tool definition (Input / Output / Structured are Go types)
type Def[Input, Output, Structured any] struct { /* runtime */ }

// The type-erased tool handle used by the registry
type Tool interface { /* unexported */ }

type Config[Input, Output, Structured any] struct {
    Description        string
    Input              *jsonschema.Schema
    Output             *jsonschema.Schema
    Structured         *jsonschema.Schema
    Execute            func(execCtx context.Context, input Input, ctx Context) (Output, error)
    ToStructuredOutput func(input Input, output any) (Structured, error)
    ToModelOutput      func(input Input, output any) []Content
}

func Make[Input, Output, Structured any](cfg Config[Input, Output, Structured]) *Def[Input, Output, Structured]
```

> Corresponds to TS `core/src/tool/tool.ts`'s `Tool.make()` / `Tool.Definition` / `Tool.Context`. TS's `WeakMap<AnyTool, Runtime>` is inlined directly as `Def.rt` in Go.
>
> Tool schemas use the typed `jsonschema.Schema` (modeled on Effect's `Schema` AST), producing two projections from a single source: `Model()` is the JSON Schema sent to the model (objects closed with `additionalProperties:false`, optional fields excluded from `required`, literal → typed enum, int check → `allOf`), and `Validation()` is used for validation (objects not closed, tolerating extra fields). The two differ only in "whether the object is closed", and **neither advertises values the decoder would reject** (optional does not accept `null`, number does not accept strings like `"NaN"` — Effect would advertise them, but its decoder actually rejects them). External JSON Schema is wrapped with `jsonschema.Raw`. Note: Effect lifts schemas with an `identifier` into `$defs` and references them with `$ref`, whereas the Go side **inlines** them (the LLM layer does not resolve `$ref`; inlining is safer for providers).

### 7.2 Built-in tool definition

```go
// core/tool/write.go
func MakeWrite(deps WriteDeps) *Def[WriteInput, WriteOutput, WriteOutput] {
    return Make(Config[WriteInput, WriteOutput, WriteOutput]{
        Description: "...",
        Input:       WriteInputSchema,
        Output:      WriteOutputSchema,
        Execute: func(execCtx context.Context, input WriteInput, ctx Context) (WriteOutput, error) { /* ... */ },
        ToModelOutput: func(input WriteInput, output any) []Content { /* ... */ },
    })
}
```

### 7.3 Registry

```go
// core/tool/registry.go
type Registry struct { /* ... */ }

func NewRegistry(opts ...RegistryOption) *Registry

// Register tools, returning a cleanup function for when the scope ends
func (r *Registry) Register(tools map[string]Tool) (cleanup func(), err error)

// Merge application + local registrations, filter by permission, produce definitions + settle
func (r *Registry) Materialize(permissions core.PermissionRuleset) *Materialization

// Convenience registration for built-in tools
func RegisterWrite(registry *Registry, deps WriteDeps) (func(), error)
```

> MCP is implemented (see §8, the `mcp/` package). Plugin is implemented (narrowed): `core/plugin.go`'s lifecycle manager (`Add`/`Remove`/`Wait`/`Close`, including loading-cycle detection and failure caching). The root package's `PluginContext` embeds `core.PluginContext` (`Agents`/`Skills`/`Reference`/`Plugin`/`Events`), and adds `Options` (from `PluginConfig.Options`), `Tools` (`*ToolRegistrar`, registering application-level tools at runtime, effective for both new and existing sessions) and `On` (subscribing to high-level `agent.Event`, including child sessions). `PluginConfig.Tools` provides static tool contributions; `Client.RegisterTools` dynamically registers/unregisters after startup (sharing the same lock as session creation, avoiding missed registrations under concurrency). `SessionHooks.OnToolDefinition` rewrites each tool definition before it is sent to the model (an error aborts the turn, aligned with TS `tool.definition`). TS's dynamic tools (external JSON Schema) are not ported; MCP tools go through this path via `jsonschema.Raw`.

> **Tool output limits**: `core.ToolOutputStore` (`MAX_LINES=2000` / `MAX_BYTES=50KB`) spills over-limit tool results to `tool-output/tool_*` on disk and sends only a head/tail preview to the model (`core/tool/output_store.go` adapts `tool.OutputStore`, injected by the root package's `runtime.go` via `tool.WithOutputStore`). **When not injected, the registry's `store == nil` and the limit is skipped**, so a single large `grep`/`read` can blow the context window.
>
> The spill directory is `{Global.Data}/tool-output`, i.e. `{ClientOptions.Root}/data/tool-output`; `ClientOptions.Root` is required. `ToolOutputStore.Cleanup` removes files older than 7 days (`ToolOutputStoreRetention`) by file mtime: the `agent` layer starts a goroutine in `newRuntime` that runs once at startup and then hourly, exiting with the runtime's lifetime context on `Client.Close()`; it can also be triggered manually with `Client.CleanupToolOutput()` (aligned with v2 `tool-output-store.ts`'s `cleanupLayer` + `cleanupNode`).

### 7.4 Built-in tools

| Tool | ID | Status |
|------|----|------|
| Write | `write` | ✅ ported (`core/tool/write.go`) |
| Read | `read` | ✅ ported (`core/tool/read.go`) |
| Edit | `edit` | ✅ ported (`core/tool/edit.go`) |
| Bash | `bash` | ✅ ported (`core/tool/bash.go`) |
| Glob | `glob` | ✅ ported (`core/tool/glob.go`) |
| Grep | `grep` | ✅ ported (`core/tool/grep.go`) |
| WebFetch | `webfetch` | ✅ ported (`core/tool/webfetch.go`) |
| Task | `task` | ✅ ported (root package `task.go`; child sessions + permission derivation, see §6.3) |
| Question | `question` | ✅ ported (`core/tool/question.go`) |
| Skill | `skill` | ✅ ported (`core/tool/skill.go`) |
| TodoWrite | `todowrite` | ✅ ported (`core/tool/todowrite.go`) |
| LSP | `lsp` | to be ported (lives in the opencode v1 layer) |
| ApplyPatch | `apply_patch` | ✅ ported (`core/tool/apply_patch.go`) |

## 8. MCP integration

**Implemented** (the `mcp/` package). Ports opencode `packages/opencode/src/mcp/`'s **client + adapter** only; OAuth/credentials/browser/TUI/v1 config are narrowed away per AGENTS.md and replaced by a passed-in `ServerConfig`.

The protocol layer uses the official Go SDK `github.com/modelcontextprotocol/go-sdk` (parallel to TS using the official `@modelcontextprotocol/sdk`).

```go
// mcp/client.go
type Client struct { /* sessions, status per server */ }
func NewClient(directory, version string) *Client
func (c *Client) Connect(ctx, name string, cfg ServerConfig) Status      // local: stdio; remote: StreamableHTTP → SSE fallback
func (c *Client) ConnectTransport(ctx, name string, t sdk.Transport, timeout time.Duration) Status
func (c *Client) Disconnect(name string)
func (c *Client) Status() map[string]Status
func (c *Client) Tools(ctx) (map[string]AdaptedTool, error)              // key: "<server>_<tool>"
func (c *Client) CallTool(ctx, server, tool string, args any) (*sdk.CallToolResult, error)

// mcp/config.go
type ServerConfig struct {
    Type string // "local" | "remote"
    Command []string; Cwd string; Environment map[string]string // local
    URL string; Headers map[string]string                       // remote
    Enabled *bool; Timeout time.Duration
}
type Status struct { Status string; Error string } // connected|disabled|failed|needs_auth|needs_client_registration

// mcp/catalog.go —— corresponds to McpCatalog
func Sanitize(value string) string
func ToolName(server, name string) string   // sanitize(server)+"_"+sanitize(name)
func ListTools/ListPrompts/ListResources/ListResourceTemplates(ctx, session)

// mcp/tool.go —— corresponds to McpCatalog.convertTool
func AdaptMCPTool(def *sdk.Tool, session *sdk.ClientSession, server string, timeout time.Duration) tool.Tool
type AdaptedTool struct { Def *sdk.Tool; Session *sdk.ClientSession; Server string; Timeout time.Duration }
func (t AdaptedTool) CoreTool() tool.Tool
```

- Runtime wiring (root package): `runtime.connectMCP` connects to each server **in parallel** and caches the "converted MCP tool set"; it subscribes to the `mcp.tools.changed` event to refresh the cache (`runtime.mcpToolSet` reads the cache), so a server's `ToolListChanged` takes effect for new sessions without rebuilding on every session creation.
- `schema/mcp_event.go`: `mcp.tools.changed` / `mcp.browser.open.failed`.
- Remote auth: **fully aligned with TS** — only `ServerConfig.Headers` (e.g. `Authorization: Bearer <token>` or `X-API-Key: <key>`), no OAuth (TS's `oauth-provider.ts`/`oauth-callback.ts`/`browser.ts`/`auth.ts` are not ported). It also does not wire in `EventV2Bridge`/`InstanceState`/`CrossSpawnSpawner`/`ConfigV1`.
- Other aligned items: `GetPrompt`/`ReadResource`, server `instructions`, `clientName` filtering, runtime `Add`, server logs (`WithLogHandler`).
- The only thing that cannot be aligned: `callTool`'s `resetTimeoutOnProgress`/`onprogress` (progress resets the timeout) — the Go SDK's `CallTool` does not expose that option, so only a context timeout can be used.

## 9. Skill system

> **Implemented.** Corresponds to opencode `packages/core/src/skill.ts`, `packages/core/src/skill/` (see `core/skill.go` / `core/skill/`).

```go
type Skill struct {
    Name        string
    Description string
    Prompt      string       // the full SKILL.md content
    Source      SkillSource  // File | URL | Git
}

type SkillLoader interface {
    Discover(ctx context.Context, dir string) ([]Skill, error)
    Load(ctx context.Context, path string) (*Skill, error)
}
```

Discovery paths (each directory in `ClientOptions.ConfigDirs`, in priority order):
1. `{skill,skills}/**/SKILL.md`
2. `.agents/skills/**/SKILL.md`
3. `.claude/skills/**/SKILL.md`

> Only the explicitly given directories are scanned; there is no environment-variable/global-directory/upward search (see §11.1).

---

## 10. Permission system

```go
// core/permission.go (corresponds to packages/schema/src/permission.ts + packages/core/src/permission.ts)
type PermissionRule struct {
    Action   string           // glob (e.g. "edit", "read.*", "*")
    Resource string           // glob
    Effect   PermissionEffect // allow | deny | ask
}

type PermissionRuleset []PermissionRule

// Take the last rule matching both action and resource; if none matches, default to ask
func PermissionEvaluate(action, resource string, rulesets ...PermissionRuleset) PermissionRule
func PermissionMerge(rulesets ...PermissionRuleset) PermissionRuleset

// Evaluate + assert: deny → error; ask → create a pending approval request and block waiting for a reply
type Permission interface {
    Ask(input PermissionAssertInput) (*PermissionAskResult, error)
    Assert(input PermissionAssertInput) error
    Reply(input PermissionReplyInput) error
    Get(id PermissionID) *PermissionRequest
    ForSession(sessionID string) []PermissionRequest
    List() []PermissionRequest
}

func NewPermission(cfg PermissionConfig) Permission
```

---

## 11. SDK layer (high-level API)

The public entry point is `pkg/agent`, import path `github.com/jiaweisky/go-agent-sdk/pkg/agent`. **Implemented**, modeled on `github/copilot-sdk/go` (`Client` → `CreateSession` → `Send`/`On`).

```go
import "github.com/jiaweisky/go-agent-sdk/pkg/agent"

// ─── Client ─────────────────────────────────────────────
type ClientOptions struct {
    // Location
    WorkingDirectory string                    // project/worktree root; empty uses the process cwd
    Root             string                    // SDK state root (data/cache/state/tmp); **required**; the library picks no default location
    DatabaseMaxConns int                       // SQLite pool size; 0 uses the default (4)
    ConfigDirs       []string                  // content config directories (agent/skill markdown, see "configuration model")
    Home             string                    // for ~ expansion; empty uses the OS home

    // Model and provider
    Model            string                    // default model
    Protocol         schema.ProtocolID         // protocol id ("openai-responses"|"anthropic-messages"|"gemini"|...)
    ModelInfo        ModelInfoProvider         // optional: catalog metadata (protocol/endpoint/limits/headers)
    ContextLimit     int                       // override the model context window (tokens), the automatic compaction threshold; 0 uses ModelInfo's limit
    OutputLimit      int                       // override the model output budget (tokens); 0 uses ModelInfo's limit
    Credentials      CredentialProvider        // provider → secret (APIKey / AWS)
    LLM              route.LLMClient           // optional: inject a custom LLM client

    // Tools and extensions
    Tools            []Tool                    // application-level tools
    MCP              []MCPConfig               // MCP servers (tools registered as application-level)
    Plugins          []PluginConfig            // plugins loaded at startup (ID/Options/Tools/Func)
    References       map[string]core.ReferenceSource // named reference sources
    ToolOutput       *ToolOutputConfig         // tool output spill limits (MaxLines / MaxBytes)

    // Runtime behavior
    Permission       PermissionHandlerFunc     // default permission callback (default ApproveAll)
    Question         QuestionHandlerFunc       // default question callback (default RejectQuestions)
    OnPanic          func(PanicInfo)           // reports user event-handler panics (panics are always recovered)
    SubagentDepth    int                       // max depth of task child sessions (default 1)
    Shell            string                    // shell used by the bash tool (default sh)
    Locale           string                    // locale for list ordering; empty uses LC_ALL/LC_COLLATE/LANG
    Compaction       *CompactionConfig         // context compaction (Auto / PreserveRecentTokens / Reserved)
    Title            *TitleConfig              // automatic session titles (Auto, off by default); see §
}
// TitleConfig controls automatic title generation by the built-in "title" agent: after the first user turn,
// if the session still has the default title and the history is exactly one user message, generate the title
// with the "title" agent and persist it (session.next.title.updated).
// Model precedence: TitleConfig.Model → the "title" agent's model → the session model. The built-in agent's
// model can be specified via RegisterAgent("title", AgentConfig{Model: ...}) or a plugin.
type TitleConfig struct {
    Auto  *bool  // whether enabled; nil means off by default
    Model string // overrides the title model; empty falls back to the title agent / session model
}
// Build a supported ModelInfo with NewModelInfo(protocol, modelID, baseURL, limit).
// ModelInfo is the narrow-input replacement for the models.dev catalog: the caller supplies
// limits/endpoint/headers/variants, and the SDK builds a route via runner.FromCatalogModel
// (Protocol decides auth and routing).
type ModelInfoProvider func(modelID string) (schema.ModelInfo, error)
func NewClient(*ClientOptions) (*Client, error)
func (c *Client) RegisterAgent(name string, cfg AgentConfig) error
func (c *Client) CreateSession(ctx, *SessionConfig) (*Session, error)
func (c *Client) ResumeSession(ctx, sessionID string, *SessionConfig) (*Session, error)
func (c *Client) ListSessions(ctx) ([]SessionInfo, error) // SessionInfo.ParentID distinguishes top-level/child sessions
func (c *Client) DeleteSession(ctx, sessionID string) error
func (c *Client) On(func(Event)) (unsubscribe func())   // global subscription (including subagent sessions)
func (c *Client) SessionMessages(sessionID string) ([]schema.SessionMessage, error) // read any session's messages (including child sessions)
func (c *Client) RegisterTools(tools ...Tool) (unregister func(), err error) // dynamically register/unregister application-level tools after startup
func (c *Client) Close() error

// ─── Session ────────────────────────────────────────────
type SessionConfig struct {
    Agent        string
    Model        string
    Title        string
    SystemPrompt *SystemPromptConfig           // append | replace | customize (built-in agent_system/instructions + custom sections)
    Permissions  schema.PermissionRuleset      // this session's rules, merged over the agent's permissions (session rules win)
    Tools        []Tool
    OnPermissionRequest PermissionHandlerFunc  // overrides the client-level permission callback
    OnQuestion   QuestionHandlerFunc           // overrides the client-level question callback
    Hooks        *SessionHooks
}
type MessageOptions struct { Prompt string; Mode string } // Mode: "queue"(default)|"steer"

func (s *Session) ID() string
func (s *Session) Messages() ([]schema.SessionMessage, error)       // history read
func (s *Session) Send(ctx, MessageOptions) iter.Seq2[Event, error] // this turn's stream, ends with Idle
func (s *Session) Steer(prompt string) error                        // enqueue only (steer), returns immediately; absorbed by the active turn at the next step boundary
func (s *Session) Compact(ctx) (bool, error)                        // manually compact history (when idle); returns ErrSessionBusy during an active turn
func (s *Session) On(func(Event)) (unsubscribe func())
func (s *Session) Abort()
func (s *Session) Close() error

// Usage is a single-turn event carrying the provider's **raw usage object** (placed in ProviderMetadata,
// indexed by provider name, e.g. {"openai": {...}}, including DeepSeek extensions like prompt_cache_hit_tokens).
// The SDK does not aggregate it or compute cost; the app subscribes and interprets/bills per provider semantics.
// (Raw usage flows through LLMEvent.Usage.ProviderMetadata → LLMEventTokens.ProviderMetadata → step.ended;
//   every protocol mapper sets providerMetadata: {<provider>: usage}, consistent with TS.)

// ─── Events (typed union) ────────────────────────────────
type Event interface{ eventKind() string }
// TextDelta / TextEnded / ReasoningDelta / ReasoningStarted / ReasoningEnded /
// ToolInputStarted / ToolCalled / ToolProgress / ToolSuccess / ToolFailed /
// StepStarted / StepEnded / StepFailed / Retried / SessionStatus / TitleUpdated /
// Usage / CompactionStarted / CompactionEnded / ShellStarted / ShellEnded /
// ErrorEvent / Idle

// ─── User tools ─────────────────────────────────────────
func DefineTool[In, Out any](name, description string,
    handler func(In, ToolContext) (Out, error)) Tool
func SchemaForType(reflect.Type) *jsonschema.Schema // reflect → AST

// ─── Permissions / Hooks ────────────────────────────────
type PermissionHandlerFunc func(PermissionRequest) (PermissionDecision, error)
var ApproveAll PermissionHandlerFunc
type SessionHooks struct { /* Pre/PostToolUse, UserPromptSubmitted,
                              SessionStart/End, ErrorOccurred */ }
```

Implementation points:
- Runs in-process (no external runtime process): `NewClient` assembles the DB + event service + projectors + tool registration + runner + coordinator.
- Tools are **isolated per session**: one registry per session (built-ins + client tools + session tools), dispatched via `core/session/runner.ToolRegistry.MaterializeFor(sessionID, permissions)`.
- Permissions: the `OnPermissionRequest` callback bridges `core.Permission` via a decorator (allow/deny/ask → callback → automatic reply).
- Tool Hooks (Pre/PostToolUse) are implemented by wrapping settlement with a `hookedRegistry`.

> Wired in: MCP tools (`connectMCP`), Plugin (narrowed), the Reference service. The granularity of system prompt sections is coarser than copilot. Cancellation semantics (aligned with TS `run-coordinator.ts`'s `uninterruptibleMask`): **only `Abort()`** cancels the drain ctx via `coordinator.Interrupt` and interrupts the in-flight LLM stream and tool execution; cancelling the ctx of `Send(ctx)` only stops waiting and does not interrupt the current turn. `Client.Close()` first calls `coordinator.StopAll` (interrupts and waits for all active turns, bounded by `shutdownGrace`; after stopping, the coordinator rejects new turns with `session.ErrStopped`), then closes sessions and the database, avoiding closing the DB under a running turn; on close it also drains reference cache warmup with a bounded wait. `context` is threaded through `SessionRunner.Run` → `Transport.Frames` → `http.NewRequestWithContext`, so `Abort()` can genuinely interrupt provider requests. The default LLM HTTP client sets `defaultLLMResponseHeaderTimeout` on provider response headers (without limiting the streaming body); inject `ClientOptions.LLM` for a different policy.

### 11.1 Configuration model (code config + content directories)

The SDK **does not use `opencode.json`** and does no directory discovery (no `OPENCODE_CONFIG_DIR` / `XDG_*` / upward search for `.opencode`). All **settings** are passed in code via `ClientOptions` (see above).

### 11.2 State directory (one Root, reading no XDG/OPENCODE env)

TS's `global.ts` is a CLI module: at load time it reads `XDG_*` / `OPENCODE_CONFIG_DIR` and other env. **The SDK deliberately deviates** — an embeddable library should not read the host process's env to locate its own state, keeping only "host-fact" inputs (`$HOME`, Windows `COMSPEC`, `LC_*`/`LANG` as locale defaults). All state is derived from `ClientOptions.Root` (`core.NewGlobal(root, home)` is a pure function that reads no env):

```
{Root}/data/    agent.db, snapshot, reference clone, plans, tool-output, log
{Root}/cache/   downloaded binaries (ripgrep), skill content cache
{Root}/state/   runtime state (reserved, aligned with TS's flock)
{Root}/tmp/     temporary files
```

`Root` is **required**: the library picks no default location, otherwise it would read/write in a directory the host did not specify (e.g. the opencode CLI's `~/.local/share/opencode`), and behavior would be affected by an existing DB / clone / snapshot on the machine. A host wanting to follow the CLI convention computes it itself (`$XDG_DATA_HOME/opencode` or `~/.local/share/opencode`, which is `dataRoot()` in the examples; `~` comes from `os.UserHomeDir`, a host fact). Apart from `$HOME` / `COMSPEC` / locale defaults, the SDK reads no `XDG_*` / `OPENCODE_*`; pointing each instance at its own `Root` fully isolates it (tests, sandboxes, multi-tenancy), so the `agent` / `examples` tests no longer need to pollute `XDG_*`.

The same principle applies to repositories: `core/repository.go` no longer reads `OPENCODE_REPO_CLONE_GITHUB_BASE_URL`, taking explicit input instead:

- github mirror: `core.WithGitHubBaseURL(base)` is passed to `RepositoryParse` / `NewRepositoryCache` (TS routes **all** github.com hosts through `githubRemote`, and an explicit URL is likewise rewritten to keep origin and clone target consistent).
- Ordering locale: `util.CompareLocale(left, right, tag)` takes the tag explicitly, and `ClientOptions.Locale` is threaded all the way to the read tool's directory listing, skill/reference guidance, and the task's subagent list; `util.ResolveLocale(name)` handles resolution, falling back to the env-inferred `util.LocaleTag` when empty (aligned with JS `localeCompare`'s default behavior).
- `core/flag` is **not ported**: `flag.ts` is the CLI's env module (at import time it snapshots ~25 `OPENCODE_*` / `OTEL_*` into the `Flag` table); the SDK has no consumer, and the only tool that used it, websearch, was removed for the reason below.
- **The `websearch` tool is not ported**: it hardcodes the MCP endpoints of two commercial backends, Exa / Parallel, and requires the host env to pass provider keys — TS itself notes this is a compromise "retained in V2 core for launch parity". A host wanting search can wire it in itself with `agent.DefineTool`; the generic `webfetch` (HTTP + HTML→markdown) is retained.
- **`share` / `unshare` are not ported**: session sharing is a **hosted service + OAuth account + network sync** (V1 `packages/opencode/src/share/` depends on `Account`/`EventV2Bridge`/`InstanceState`, uses `HttpClient` against `${baseUrl}/api/share|shares/{create,sync,remove}`, and uploads session/message/part/diff queues with a `secret`). v2 core has only the data model (`session_share` table, `share_url` column, `share_url: info.share?.url` in the projector), with **no service / `setShare` / events**, and is CLI infrastructure outside the SDK's scope. The database still retains the `share_url` column and projects `V1SessionShare` as-is, to align with v2 core's `info.share?.url`.

Files carry **content only** (agent / mode / skill markdown), and the source must be **explicitly** declared: `ClientOptions.ConfigDirs`. Each directory is scanned with the following patterns:

- `{agent,agents}/**/*.md` — subagents
- `{mode,modes}/*.md` — primary agents (formerly "mode")
- `{skill,skills}/**/SKILL.md` — skills

Precedence: built-in agents (code) < `ConfigDirs` (files) < `RegisterAgent` (code).

Difference from TS: TS searches upward from `.opencode` and merges `opencode.json` + environment variables + global directories; the Go SDK reads only the directories the caller gives, avoiding implicit I/O and global state. `core/config.go` (Config.Info/Document discovery), `core/config/`, and `core/v1/config/` (legacy ConfigV1) have been **deleted**.

## 12. Error handling

```go
// llm/schema/errors.go
type LLMError struct {
    Module string          // e.g. "ProviderShared"
    Method string          // e.g. "stream"
    Reason LLMErrorReason
}

type LLMErrorReason struct {
    Kind    LLMErrorReasonKind // InvalidRequest | NoRoute | Authentication | RateLimit |
                               // QuotaExceeded | ContentPolicy | ProviderInternal |
                               // Transport | InvalidProviderOutput | UnknownProvider
    Message string
    // Optional fields: Parameter / Classification / Status / Http / Route / Provider / Model / ...
}

// Tool failure (corresponds to TS @opencode-ai/llm's ToolFailure)
type ToolFailure struct {
    Message  string
    Cause    error
    Metadata map[string]any
}
```

> Permission-related errors are in `core/permission.go`: `PermissionBlockedError`, `PermissionDeclinedError`, `PermissionCorrectedError`, `PermissionNotFoundError`.

---

## 13. Streaming event model

> **Implemented.** The canonical events are `schema/session_event.go`'s `SessionEvent*` (core session events, durable persistence + live-only deltas); the runner translates `llm/schema.LLMEvent` into session events via `LLMEventPublisher`. The root package `agent` then maps session events into the high-level `agent.Event` union (`events.go`), and `Session.Send` returns `iter.Seq2[agent.Event, error]` (ending with `Idle`); `Client.On` can subscribe globally (including subagent sessions). The raw provider → caller stream remains `iter.Seq2[schema.LLMEvent, error]` (see §5.3).

> **Session running state**: the SDK emits `session.status` (`busy`/`idle`, live-only) over the drain lifecycle, mapped by the agent layer to `agent.SessionStatus`. Subscribers use it to tell whether a session is running, so they render a spinner/heartbeat only while it is (aligned with TS `session.status`; TS's publisher is in the v1 CLI layer, whereas Go publishes it in the runtime's coordinator closure).

---

## 14. Build and test

```bash
# Tests
go test ./...
go test -race ./...
```

---

## 15. References

- [opencode GitHub](https://github.com/anomalyco/opencode)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)
- [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses)
- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [OpenRouter API](https://openrouter.ai/docs)
