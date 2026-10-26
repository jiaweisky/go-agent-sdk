# go-agent-sdk — Coding Agent Reference

## Git: do not commit automatically

**Never commit automatically.** Run `git commit` only when the current request explicitly says to commit; otherwise leave the changes uncommitted (staged or unstaged) for review. Never `git push` unless explicitly asked.

## Project documentation

- **`docs/DESIGN.md`** — architecture design, detailed module design, MVP scope, SDK public API (**project scope and public API are governed by this document**)
- **`docs/DESIGN-AGENTD.md`** — the multi-transport `agentd` command design
- **`docs/DESIGN-A2A.md`** — A2A protocol support
- **`docs/DESIGN-HTTP.md`** — HTTP/SSE server API
- **`docs/DESIGN-MEDIA.md`** — media delivery and hosting
- **`docs/DESIGN-MEDIA-PROVIDERS.md`** — proposed provider-aware media uploads and capability matrix
- **`docs/DESIGN-ISOLATION.md`** — per-project isolation
- **`docs/PORTING-STRATEGY.md`** and **`docs/PORTING-REFERENCE.md`** — **historical**: the opencode TypeScript/Effect → Go porting phase is complete. These documents are retained for provenance and for understanding past decisions; they are not active implementation plans.

## Slash commands

Project commands live in `.opencode/command/`:

- **`/review-diff [optional focus]`** — review only the current uncommitted changes (staged, unstaged, untracked).
- **`/review-code <file|directory|package>`** — review existing code for design, error handling, duplication, naming, comments, readability and complexity.
- **`/review-layout <file|directory|package>`** — review existing code layout: declaration order, method grouping, file cohesion, blank lines and naming.

All three are review-only: they must not modify code. Use them when a review is requested; for a narrower request, pass the focus or target as the argument. Command changes are loaded when opencode starts, so restart opencode after editing files under `.opencode/command/`.

## Confirmed Go-specific deviations

The code intentionally keeps deviations from the original upstream design. Do not revert them without discussing and confirming with the user:

- the `task` tool and child sessions;
- automatic title generation;
- no `websearch` tool in the MVP built-in set;
- the agent tool allowlist (`tools:` in agent definitions);
- the operator-configured inbound media delivery policy;
- two media-hosting paths: provider-independent `MediaStore` and provider-bound `MediaUploader`.

The rationale and historical exception list are in `docs/PORTING-REFERENCE.md`.

## Go version

Go 1.27.1; the following features are safe to use:

- `iter.Seq` / `iter.Seq2` — stream/iterator replacement
- Generic methods — `func (List[E]) Map[R any](...)`
- Generic type aliases — `type StringMap[V any] = map[string]V`
- `weak` package — weak-reference caching
- `os.Root` — safe directory access
- `testing/synctest` — testing concurrent behavior
- `new(expression)` — `new(ReadParams{Name: "foo"})`

## Package architecture

The module is split by visibility: the **public API lives under `pkg/`**
(importable by external code) and the **engine internals live under `internal/`**
(importable only within this module). The module root is package-free.

```
go-agent-sdk/
├── pkg/            public API (external code imports pkg/...)
│   ├── agent/      high-level public API: Client / Session / Send / On /
│   │               DefineTool / Hooks / Permission
│   ├── schema/     shared data types and IDs
│   ├── llm/        LLM protocol layer
│   │   ├── schema/    LLMRequest, LLMEvent, LLMError, ID types
│   │   ├── route/     Route, Protocol, Endpoint, Auth, Framing, Executor, Transport
│   │   ├── protocols/ per-provider wire protocols (flat: openai_chat.go, ...)
│   │   └── provider/  provider facades (openai, anthropic, openrouter, ...)
│   ├── core/       public core types
│   │   ├── tool/      tool framework (tool, registry, write, ...)
│   │   ├── skill/     skill discovery
│   │   └── systemcontext/ agent system context
│   ├── mcp/        MCP client + adapter
│   ├── a2a/        A2A (Agent2Agent) support
│   │   ├── client/    outbound thin interface (does not leak a2a-go)
│   │   ├── a2ago/     outbound a2a-go adapter
│   │   └── server/    inbound: AgentExecutor + taskstore.Store + AgentCard + http.Handler
│   ├── server/     HTTP/SSE API for non-Go hosts
│   ├── jsonschema/ typed JSON Schema AST for tool schemas
│   └── session/    public session-store interfaces (e.g. TurnStore)
├── internal/       engine internals (not importable outside this module)
│   ├── core/       session engine, database, effect utilities, project/reference helpers
│   │   ├── session/ session engine (+ session/runner)
│   │   ├── database/ SQLite + migrations
│   │   ├── effect/  keyed-mutex and other runtime utilities
│   │   ├── util/    wildcard, hash, glob
│   │   ├── project/ project resolution + directories
│   │   └── reference/ ripgrep / installation / wire
│   ├── defs/       load agent/skill definitions from explicit ConfigDirs
│   │   ├── config/  parse {agent,agents}/**/*.md, {mode,modes}/*.md
│   │   └── skill/   scan {skill,skills}/**/SKILL.md
│   └── llm/        LLM internals
│       ├── utils/   record and shared helpers
│       └── protocols/utils/ protocol shared utilities
└── cmd/            standalone commands
    └── agentd/     multi-transport server and its packages
```

- **Visibility rule (enforced):** no exported `pkg/...` symbol may reference an
  `internal/...` type. `make check-boundary` runs an AST check
  (`testdata/boundarycheck.go`) and builds `testdata/external` (a separate
  module that imports only `pkg/...`). A type referenced by the public API must
  live in `pkg/...`; only code used solely within the module goes to
  `internal/...`.
- Public vs internal is decided by **who must name the type**: a type exposed by
  the public API goes to `pkg/...`; a pure engine type goes to `internal/...`.
- `jsonschema/` is a Go-specific typed JSON Schema AST, used by `core/tool` to
  define tool schemas.
- `a2a/` is a Go-specific capability. It depends only on
  `github.com/a2aproject/a2a-go/v2`, and a2a-go types do not cross the
  `a2a/a2ago/` and `a2a/server/` boundaries into the public API.
- `server/` is a Go-specific HTTP/SSE API. It uses `net/http` with plain Go
  structs, keeps `jsonschema` confined to `core/tool`, and deliberately does
  **not** use schema-first/OpenAPI codegen.
- `cmd/` holds standalone commands. `cmd/agentd` is the multi-transport server
  (HTTP/SSE and/or A2A, one client per configured server) with shared packages
  `cmd/agentd/config` (DTOs + validation), `client` (assembly), `media`,
  `storage`, `modelinfo`, `sandbox`, `sandboxmode`, `sandboxpolicy`.
  `cmd/agentd-supervisor` is a host supervisor that speaks the
  `supervisor_command` JSON protocol and fronts agentd's control API.
- Dependency injection is constructor-based; there is no DI container.

## Same-package symbol naming

When several source modules or protocol files are flattened into one Go
package, distinguish same-named symbols with a module/protocol prefix:

- exported: **PascalCase prefix** — `OpenAIChatProtocol`,
  `OpenAIResponsesHTTPRoute`, `FileMutationTarget`, `LocationMutationTarget`;
- unexported: **camelCase prefix** — `openaiChatBody`,
  `openaiResponsesParserState`, `anthropicMessagesImageMIMEs`.

Symbols that already carry a prefix do not get another one (e.g.
`OpenAIChatDefaultBaseURL`).

## Time and unit suffixes

- A time value stored as a **bare integer** must carry an explicit unit suffix:
  `Seconds` for seconds, `Millis` for milliseconds. Do **not** use `Secs` or
  `MS`.
- Prefer `time.Duration` for in-memory time values, and duration strings (for
  example `"15s"`) for config fields parsed with `time.ParseDuration`; those
  field names do not need a unit suffix (`Timeout`, `KeepAlive`, `PresignTTL`).
- Byte-sized fields follow the same principle where the unit is not obvious
  from the type or surrounding block (for example `MaxBodyBytes`).
