# go-agent-sdk — Coding Agent Reference

## Git: do not commit automatically

**Never commit automatically.** Run `git commit` only when the current request explicitly says to commit; otherwise leave the changes uncommitted (staged or unstaged) for review. Never `git push` unless explicitly asked.

## Project documentation

- **`docs/DESIGN.md`** — architecture design, detailed module design, MVP scope, SDK public API (**project scope and public API are governed by this document**)
- **`docs/PORTING-STRATEGY.md`** — the TS/Effect → Go porting strategy and decision trees
- **`docs/DESIGN-A2A.md`** — the sub-volume design for A2A protocol support (draft). A2A has **no TS counterpart** in opencode; it is a Go-specific capability and the porting rules above do not apply

## Go code in the documents

The Go code in both documents is illustrative only; details such as function signatures, type definitions and error handling are **governed by opencode's TypeScript source**. When porting, compare against the full logic of the corresponding opencode file.

## Porting rules

### Alignment target: v2 core

**The alignment target is v2 core (`packages/core/src/`)**: Go's behavior and structure should align with v2 core; for parts v2 core has not yet implemented or defined (including TODOs in its source), by default **do not improvise** — wait for v2 to land and follow it.

**Parts that require V1 alignment are exceptions**: they must first be discussed and confirmed with the user, then recorded in the "exception list" below. For judgment signals, refer to the legacy signals in the "Porting scope" section.

**Exception list**:
- **V1 alignment** (not in v2 core, but implemented): the `task` tool and child sessions (`task.go` / `task_run.go`), and automatic title generation (`TitleConfig` / `ensureTitle`).
- **Deliberately excluded within v2 scope**: the `websearch` tool — v2 core `tool/websearch.ts` is implemented and registered, but the MVP's built-in tool scope is limited to 11 (see `docs/DESIGN.md`).
- **Go-side deviation (not in v2 core)**: **agent tool allowlist** — the agent definition frontmatter gains a `tools:` field (`schema.AgentInfo.Tools`): when set, only the listed tool names are materialized for that agent's sessions (model-facing definitions filtered, and the settle path rejects anything else as unknown); the field absent means every registered tool, and an explicit `tools: []` means no tools at all. v2 core has no such field (tool availability is permission-driven only). Discussed and confirmed with the user.
- **Go-side deviation (not in v2 core)**: the **inbound media delivery policy** (`docs/DESIGN-MEDIA.md` {M16} §4.12) — the source of an inbound attachment (inline / url / provider `file`) is chosen by an **operator-configured policy** (`media:`), with the caller only overriding it, instead of being caller-declared only. Discussed and confirmed with the user.
- **Go-side deviation (not in v2 core)**: **reference hosting** (`docs/DESIGN-MEDIA.md` {M17} §4.10 Axis 3) — a non-inline media form comes from one of **two hosts**: the **provider-independent `MediaStore`** (object storage → url, e.g. S3/COS/nginx) and the **provider-bound `MediaUploader(providerID)`** (its Files API → `file_id`/`fileUri`). `url` is **no longer caller-only**, and capability is declared **per modality**. Needed to reach url-fetching providers with no Files API (e.g. self-hosted vLLM) and to keep the store provider-agnostic. Discussed and confirmed with the user.


### Porting scope: legacy / v1 code is not ported by default

By default port only **v2 core** (`packages/core/src/`). **legacy / v1 code is not ported by default**, unless it is confirmed to be a necessary dependency of v2 core — in which case **confirm with the user first** before starting.

Signals that something is legacy (any one suffices):
- It lives in `packages/opencode/src/session/` (the v1 CLI orchestration layer)
- It imports v1 modules: `v1/`, `SessionV1`, `ConfigV1`, `MessageV2`, `SessionProcessor`, `Plugin`, legacy `Provider`, `EventV2Bridge`, `RuntimeFlags`, `InstanceState`, etc.
- It is an HTTP handler / `app-runtime` aimed at the opencode CLI

Example: `packages/core/src/session/compaction.ts` (v2, port) vs `packages/opencode/src/session/compaction.ts` (v1 legacy, do not port; it depends on the former only because it reuses core's `buildPrompt`).

> Note: **utility modules** under `packages/opencode/src/` (such as `tool/json-schema.ts`) are not legacy and are still ported as needed.

### CLI infrastructure: do not copy; use SDK-native narrow inputs

In opencode, **CLI-facing infrastructure modules are not ported as-is by default**, for example `catalog` (the models.dev model catalog + provider registry), `credential` (`auth.json` credential storage), `integration` (the OAuth connection flow/storage), etc.

Judgment signals (any one suffices):
- Its responsibility is "data source / persistence / OAuth / DI service" rather than the pure logic of the agent loop
- It depends on Effect's `Context.Service` + `Layer` (DI node) without having substantial algorithms itself

Approach: **port only its pure transformation logic**, replacing the data source with an SDK-native narrow input (constructor injection).
- Example: `session/runner/model.ts`'s `fromCatalogModel` / `withDefaults` / `withVariant` depend only on `schema.ModelInfo` + an API key + `llm`'s route/protocol/Auth. When porting, use a "`ModelInfo` provider + API key" constructor in place of `Catalog.Service` / `Integration.Service` / `Credential.Value`; there is no need to port the three services.
- Reference: `docs/DESIGN.md`'s public API directly takes a `Protocol` (protocol id) + `Credentials` (`schema.Credential`) + `Model`, and the SDK goes through the `llm/protocols` registry + `llm/route`, introducing no catalog/credential/integration.

If some piece of infrastructure is confirmed to be a truly necessary dependency, **confirm with the user first** before porting it.

### Read the entire TS source file before starting

A mistake was made before: `events.ts` has 618 lines, and only the first half (type definitions) was read before starting to write, missing the 15+ methods in the second half (the state machine, the LLMResponse namespace). **Every file must be read completely before starting**, including:
- Type definitions (struct/interface)
- All exported functions and methods
- Static methods in namespaces
- Private helper functions (which may affect the public API design)

### Same-package symbol naming: module-name prefix

Each TS file is a separate module (with its own namespace); a Go package has only one namespace. When multiple flat files under `packages/<pkg>/src/` enter the **same Go package**, same-named symbols collide and must be distinguished with a module-name prefix:

- Exported symbols: **PascalCase prefix** — `OpenAIChatProtocol`, `OpenAIResponsesHTTPRoute`, `FileMutationTarget`, `LocationMutationTarget`, `PermissionRule`
- Unexported symbols: **camelCase prefix** — `openaiChatBody`, `openaiResponsesParserState`, `anthropicMessagesImageMIMEs`
- Symbols that already carry a prefix do not get another one (e.g. `OpenAIChatDefaultBaseURL` stays as-is)

Typical scenarios: `llm/protocols/` (5 protocols), `core/` (file-mutation / location-mutation / permission / ...), `schema/` (event / location / permission / project / ...).

### Porting workflow

When porting a TS file:

1. **Read the target TS file completely** (see above)
2. **Analyze dependencies**: list all TS modules it imports (including transitive dependencies)
3. **Compare each against the Go implementation**:
   - Already implemented → compare **line by line** against the TS source to verify logical consistency (not just signatures); fix any differences found
   - Not implemented → record it as to-be-ported
4. **Port in dependency order**: port unimplemented dependencies first (dependency roots/leaves first), and the target file last
5. **Track all review (comparison) and porting tasks with a todo list**; update the status as each item is completed

**Do not**: skip dependencies and write the target file directly; compare only signatures while ignoring implementation logic.

## Go version

Go 1.27.1; the following features are safe to use:
- `iter.Seq` / `iter.Seq2` — replaces Effect Stream
- Generic methods — `func (List[E]) Map[R any](...)`
- Generic type aliases — `type StringMap[V any] = map[string]V`
- `weak` package — weak-reference caching
- `os.Root` — safe directory access
- `testing/synctest` — testing concurrent behavior
- `new(expression)` — `new(ReadParams{Name: "foo"})`

## Key porting decisions quick reference

| TS/Effect pattern | Go approach |
|---|---|
| `Effect<T, E>` / `yield*` | `(T, error)` + explicit argument passing |
| `Stream<T, E>` | `iter.Seq2[T, error]` |
| `Schema.Struct({...})` + `Schema.optional` | Go struct + json tag + `*T` |
| `type E = A \| B \| C` (≤4, large structural differences) | marker interface + type switch |
| `type E = A \| B \| C` (5-10, overlapping fields) | tagged struct + constructors |
| `type E = A \| B \| C` (>10, JSON persistence) | `json.RawMessage` + deferred dispatch |
| `Context.Tag` + `Layer` | constructor injection |
| Multiple TS modules in one directory flattened into one Go package | distinguish symbols with a module-name prefix (`OpenAIChatProtocol` / `openaiChatBody`) |
| `Route<Body, Prepared>` | `llm/route/` struct + `any` type erasure |
| `WeakMap<AnyTool, Runtime>` | generic `Def[Input, Output, Structured]` + `Tool` interface |
| `Schema.Codec` | `santhosh-tekuri/jsonschema/v6` validation |
| Tool schema (`Schema.Struct`, etc.) | typed `jsonschema.Schema` AST: `Model()` sent to the model + `Validation()` for validation |
| MCP (`@modelcontextprotocol/sdk`) | the official Go SDK `github.com/modelcontextprotocol/go-sdk` |
| CLI env/XDG defaults (`global.ts`, `flag.ts`) | **deliberate deviation**: the library does not read host env such as `XDG_*` (keeping only host facts like `$HOME`/`COMSPEC`/`LC_*`); the state directory is derived from the **required** `ClientOptions.Root` (`core.NewGlobal(root, home)` is a pure function), and configuration is declared explicitly by `ConfigDirs` |

## Package architecture

Go directories mirror opencode's TS **package** structure, but split by visibility:
the **public API lives under `pkg/`** (importable by external code) and the
**engine internals live under `internal/`** (importable only within this module).
The module root is package-free.

```
go-agent-sdk/                       opencode TS
├── pkg/            public API (external code imports pkg/...)
│   ├── agent/      ← (no TS dir) high-level public API: Client / Session / Send / On / DefineTool / Hooks / Permission
│   ├── schema/     ← packages/schema/src/     (@opencode-ai/schema)
│   ├── llm/        ← packages/llm/src/         (@opencode-ai/llm)
│   │   ├── schema/    LLMRequest, LLMEvent, LLMError, ID types
│   │   ├── route/     Route, Protocol, Endpoint, Auth, Framing, Executor, Transport
│   │   ├── protocols/ per-provider wire protocols (flat: openai_chat.go, ...)
│   │   └── provider/  Provider facades (openai, anthropic, openrouter, ...)
│   ├── core/       ← packages/core/src/        (@opencode-ai/core) public core types
│   │   ├── tool/      tool framework (tool, registry, write, ...)
│   │   ├── skill/     skill discovery
│   │   └── systemcontext/ agent system context
│   ├── mcp/        ← packages/opencode/src/mcp/ (MCP client + adapter)
│   ├── a2a/        A2A (Agent2Agent) support (Go-specific; no TS counterpart)
│   │   ├── client/    outbound thin interface (does not leak a2a-go)
│   │   ├── a2ago/     outbound a2a-go adapter (the only outbound layer importing a2a-go)
│   │   └── server/    inbound: AgentExecutor + taskstore.Store + AgentCard + http.Handler
│   ├── server/     HTTP/SSE API for non-Go hosts (Go-specific; no TS counterpart)
│   ├── jsonschema/ Go-specific typed JSON Schema AST for tool schemas
│   └── session/    public session-store interfaces (e.g. TurnStore)
├── internal/       engine internals (not importable outside this module)
│   ├── core/       ← packages/core/src/ internals
│   │   ├── session/ session engine (+ session/runner)
│   │   ├── database/ SQLite + migrations
│   │   ├── effect/  Effect runtime utilities (keyed-mutex)
│   │   ├── util/    general utilities (wildcard, hash, glob)
│   │   ├── project/ project resolution + directories
│   │   ├── reference/ ripgrep/ installation/ wire/
│   ├── defs/       ← (no TS dir) load agent/skill definitions from explicit ConfigDirs
│   │   ├── config/  parse {agent,agents}/**/*.md, {mode,modes}/*.md
│   │   └── skill/   scan {skill,skills}/**/SKILL.md
│   └── llm/        ← packages/llm/src/ internals
│       ├── utils/   general utilities (record)
│       └── protocols/utils/ protocol shared utilities
└── cmd/            standalone commands (Go-specific; no TS counterpart)
```

- **Visibility rule (enforced):** no exported `pkg/...` symbol may reference an
  `internal/...` type. `make check-boundary` runs an AST check
  (`testdata/boundarycheck.go`) and builds `testdata/external` (a separate module
  that imports only `pkg/...`). A type referenced by the public API must live in
  `pkg/...`; only code used solely within the module goes to `internal/...`.
- Public vs internal is decided by **who must name the type**, not by porting order:
  a ported module that the public API exposes goes to `pkg/...`; a pure engine
  module (session engine, database, effect, defs loading, protocol utils) goes to
  `internal/...`. Some `packages/core/src/` subdirs are public (`tool`, `skill`,
  `systemcontext`), the rest are internal.
- `jsonschema/` is a Go-specific typed JSON Schema AST (modeled on Effect `Schema`),
  used by `core/tool` to define tool schemas; no corresponding TS directory.
- `a2a/` is a Go-specific capability (**no corresponding TS directory**) and the "port
  v2 core only" rule does not apply; see `docs/DESIGN-A2A.md`. It depends only on
  `github.com/a2aproject/a2a-go/v2`, and a2a-go types do not cross the `a2a/a2ago/`
  and `a2a/server/` boundaries into the public API.
- `server/` is a Go-specific HTTP/SSE API (no TS counterpart); see `docs/DESIGN-HTTP.md`.
  It uses `net/http` with plain Go structs, keeps `jsonschema` confined to `core/tool`,
  and deliberately does **not** do schema-first/OpenAPI codegen.
- `cmd/` holds standalone commands: `cmd/agentd` (multi-transport server: HTTP/SSE
  and/or A2A, one client per configured server) with its shared packages
  `cmd/agentd/config` (DTOs + validation), `client` (assembly), `media`, `storage`,
  `modelinfo`, `sandbox`, `sandboxmode`, `sandboxpolicy`, and `cmd/agentd-supervisor`
  (a host supervisor that speaks the `supervisor_command` JSON protocol and fronts
  agentd's control API).
- Effect's `Context.Tag` + `Layer` (DI) is not ported; constructor injection is used instead.

## Directory/file name alignment with opencode

Rules:
- `packages/<pkg>/src/<path>/<name>.ts` → `<pkg>/<path>/<name>.go`, **kebab-case → snake_case**, keeping the directory hierarchy — under `pkg/` when public, under `internal/` when engine-internal
- A directory's `index.ts`, if it needs porting (contains implementation) → `<pkg>/<path>/<directory-name>.go`; a pure barrel (re-export only) is not ported, because Go has no re-export

| opencode TS | Go |
|---|---|
| `packages/llm/src/route/client.ts` | `pkg/llm/route/client.go` |
| `packages/llm/src/schema/messages.ts` | `pkg/llm/schema/messages.go` |
| `packages/llm/src/protocols/openai-chat.ts` | `pkg/llm/protocols/openai_chat.go` (flat in the same package; symbols get a protocol prefix) |
| `packages/llm/src/protocols/utils/tool-stream.ts` | `internal/llm/protocols/utils/tool_stream.go` |
| `packages/core/src/file-mutation.ts` | `pkg/core/file_mutation.go` |
| `packages/core/src/location-mutation.ts` | `pkg/core/location_mutation.go` |
| `packages/core/src/tool/tool.ts` | `pkg/core/tool/tool.go` |
| `packages/core/src/tool/write.ts` | `pkg/core/tool/write.go` |
| `packages/core/src/effect/keyed-mutex.ts` | `internal/core/effect/keyed_mutex.go` |
| `packages/core/src/util/wildcard.ts` | `internal/core/util/wildcard.go` |
| `packages/core/src/project/directories.ts` | `internal/core/project/directories.go` |
| `packages/schema/src/identifier.ts` | `pkg/schema/identifier.go` |
| `packages/opencode/src/id/id.ts` | not ported (`pkg/schema/identifier.go` already provides ID generation) |

**Common pitfalls:**
- Do not create a same-named subpackage for a flat file: `core/src/file-mutation.ts` → `pkg/core/file_mutation.go` (package `core`), **not** `pkg/core/filemutation/filemutation.go`
- Type names collide within the same Go package; distinguish them with a module-name prefix (e.g. `FileMutationTarget` vs `LocationMutationTarget`, `OpenAIChatProtocol` vs `OpenAIResponsesProtocol`)
- `packages/opencode/src/tool/json-schema.ts` is **not ported**: it only serves `$ref` inlining for CLI tool schemas, and there is no consumer on the Go side. The tool framework is only `pkg/core/tool`
- `packages/core/src/effect/app-node.ts` and `layer-node.ts` are Effect DI infrastructure and are not ported
- When porting a `packages/core/src/` module, first decide its visibility: if the public API names its types, port it to `pkg/core/...`; otherwise `internal/core/...`. Run `make check-boundary` after adding a public type.
- Go has no circular dependencies; a TS module cycle (e.g. `protocols/` ↔ `protocols/utils/`) must be refactored away (e.g. move `ValidateMedia`, which `utils` reverse-depends on, into `utils`)
- `packages/llm/src/route/transport/index.ts`'s `Transport`/`TransportRuntime`/`TransportPrepareInput` go in `llm/route/transport.go` (package `route`), because the `transport` package reverse-depends on `route` (`Framing`) and putting them in `transport/` would create a cycle
