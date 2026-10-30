# opencode → Go porting reference (historical)

> **Status: historical.** Porting from opencode's TypeScript/Effect source is
> complete. This document preserves the operational rules, exceptions and
> file/package mappings for provenance and for understanding why the Go code is
> shaped the way it is. It is **not** an active implementation plan; use
> `docs/DESIGN.md` and the other current design documents for active work.

## 1. Alignment target: v2 core

The alignment target was opencode v2 core (`packages/core/src/`): Go behavior
and structure were aligned with v2 core. For parts v2 core had not implemented
or defined (including TODOs in its source), the rule was **not to improvise** —
wait for v2 and follow it.

Parts that required V1 alignment were exceptions, confirmed with the user and
recorded below.

### Exception list

- **V1 alignment** (not in v2 core, but implemented): the `task` tool and child
  sessions (`task.go` / `task_run.go`), and automatic title generation
  (`TitleConfig` / `ensureTitle`).
- **Deliberately excluded within v2 scope**: the `websearch` tool — v2 core
  `tool/websearch.ts` is implemented and registered, but the MVP's built-in
  tool scope is limited to 11 (see `docs/DESIGN.md`).
- **Go-side deviation (not in v2 core)**: **agent tool allowlist** — the agent
  definition frontmatter gains a `tools:` field (`schema.AgentInfo.Tools`):
  when set, only the listed tool names are materialized for that agent's
  sessions (model-facing definitions filtered, and the settle path rejects
  anything else as unknown); the field absent means every registered tool, and
  an explicit `tools: []` means no tools at all. v2 core has no such field
  (tool availability is permission-driven only). Discussed and confirmed with
  the user.
- **Go-side deviation (not in v2 core)**: **agent tool blacklist** — the agent
  definition frontmatter gains a `tools_exclude:` field
  (`schema.AgentInfo.ToolsExclude`): the named tools are removed after the
  `tools:` allowlist is applied, so an agent can start from every registered
  tool (including MCP tools discovered at runtime) and subtract only the
  unwanted ones. It is functionally equivalent to denying those tools via
  permission rules (`deny *` style), but reads better in configuration. The
  field absent or empty removes nothing. Discussed and confirmed with the user.
- **Go-side deviation (not in v2 core)**: the **inbound media delivery policy**
  (`docs/DESIGN-MEDIA.md` {M16} §4.12) — the source of an inbound attachment
  (inline / url / provider `file`) is chosen by an **operator-configured
  policy** (`media:`), with the caller only overriding it, instead of being
  caller-declared only. Discussed and confirmed with the user.
- **Go-side deviation (not in v2 core)**: **reference hosting**
  (`docs/DESIGN-MEDIA.md` {M17} §4.10 Axis 3) — a non-inline media form comes
  from one of **two hosts**: the **provider-independent `MediaStore`** (object
  storage → url, e.g. S3/COS/nginx) and the **provider-bound
  `MediaUploader(providerID)`** (its Files API → `file_id`/`fileUri`). `url` is
  **no longer caller-only**, and capability is declared **per modality**.
  Needed to reach url-fetching providers with no Files API (e.g. self-hosted
  vLLM) and to keep the store provider-agnostic. Discussed and confirmed with
  the user.

## 2. Porting scope: legacy / v1 code was not ported by default

Only **v2 core** (`packages/core/src/`) was ported by default. Legacy / v1 code
was not ported unless confirmed as a necessary dependency of v2 core.

Signals that something was legacy (any one suffices):

- It lived in `packages/opencode/src/session/` (the v1 CLI orchestration layer).
- It imported v1 modules: `v1/`, `SessionV1`, `ConfigV1`, `MessageV2`,
  `SessionProcessor`, `Plugin`, legacy `Provider`, `EventV2Bridge`,
  `RuntimeFlags`, `InstanceState`, etc.
- It was an HTTP handler / `app-runtime` aimed at the opencode CLI.

Example: `packages/core/src/session/compaction.ts` (v2, port) vs
`packages/opencode/src/session/compaction.ts` (v1 legacy, do not port; it
depends on the former only because it reuses core's `buildPrompt`).

> Note: utility modules under `packages/opencode/src/` (such as
> `tool/json-schema.ts`) were not legacy and were still ported as needed.

## 3. CLI infrastructure: not copied; used SDK-native narrow inputs

CLI-facing infrastructure modules were not ported as-is, for example `catalog`
(the models.dev model catalog + provider registry), `credential` (`auth.json`
credential storage), and `integration` (the OAuth connection flow/storage).

Judgment signals (any one suffices):

- Its responsibility was "data source / persistence / OAuth / DI service"
  rather than the pure logic of the agent loop.
- It depended on Effect's `Context.Service` + `Layer` (DI node) without having
  substantial algorithms itself.

Approach: port only the pure transformation logic, replacing the data source
with an SDK-native narrow input (constructor injection).

Example: `session/runner/model.ts`'s `fromCatalogModel` / `withDefaults` /
`withVariant` depend only on `schema.ModelInfo` + an API key + `llm`'s
route/protocol/Auth. The Go implementation uses a "`ModelInfo` provider + API
key" constructor in place of `Catalog.Service` / `Integration.Service` /
`Credential.Value`.

## 4. Read the entire TypeScript source before porting

A mistake was made once: `events.ts` has 618 lines, and only the first half
(type definitions) was read before writing, missing the 15+ methods in the
second half (the state machine, the `LLMResponse` namespace). Every file had to
be read completely first, including:

- type definitions (struct/interface);
- all exported functions and methods;
- static methods in namespaces;
- private helper functions (which may affect the public API design).

## 5. Same-package symbol naming

Each TypeScript file was a separate module (with its own namespace); a Go
package has only one namespace. When multiple flat files under
`packages/<pkg>/src/` entered the same Go package, same-named symbols were
distinguished with a module-name prefix:

- Exported symbols: **PascalCase prefix** — `OpenAIChatProtocol`,
  `OpenAIResponsesHTTPRoute`, `FileMutationTarget`, `LocationMutationTarget`,
  `PermissionRule`.
- Unexported symbols: **camelCase prefix** — `openaiChatBody`,
  `openaiResponsesParserState`, `anthropicMessagesImageMIMEs`.
- Symbols that already carried a prefix did not get another one (e.g.
  `OpenAIChatDefaultBaseURL`).

Typical scenarios: `llm/protocols/` (5 protocols), `core/` (file-mutation /
location-mutation / permission / ...), `schema/` (event / location / permission
/ project / ...).

## 6. Porting workflow

When porting a TypeScript file:

1. Read the target TypeScript file completely (see above).
2. Analyze dependencies: list all TypeScript modules it imports, including
   transitive dependencies.
3. Compare each against the Go implementation:
   - already implemented → compare **line by line** against the TypeScript
     source to verify logical consistency (not just signatures); fix
     differences found;
   - not implemented → record it as to-be-ported.
4. Port in dependency order: unimplemented dependencies first (dependency
   roots/leaves first), and the target file last.
5. Track all review/comparison and porting tasks with a todo list; update status
   as each item completes.

Do not skip dependencies and write the target file directly; do not compare
only signatures while ignoring implementation logic.

## 7. Key porting decisions

| TypeScript/Effect pattern | Go approach |
|---|---|
| `Effect<T, E>` / `yield*` | `(T, error)` + explicit argument passing |
| `Stream<T, E>` | `iter.Seq2[T, error]` |
| `Schema.Struct({...})` + `Schema.optional` | Go struct + json tag + `*T` |
| `type E = A \| B \| C` (≤4, large structural differences) | marker interface + type switch |
| `type E = A \| B \| C` (5-10, overlapping fields) | tagged struct + constructors |
| `type E = A \| B \| C` (>10, JSON persistence) | `json.RawMessage` + deferred dispatch |
| `Context.Tag` + `Layer` | constructor injection |
| Multiple TypeScript modules in one directory flattened into one Go package | module-name prefix (`OpenAIChatProtocol` / `openaiChatBody`) |
| `Route<Body, Prepared>` | `llm/route/` struct + `any` type erasure |
| `WeakMap<AnyTool, Runtime>` | generic `Def[Input, Output, Structured]` + `Tool` interface |
| `Schema.Codec` | `santhosh-tekuri/jsonschema/v6` validation |
| Tool schema (`Schema.Struct`, etc.) | typed `jsonschema.Schema` AST: `Model()` sent to the model + `Validation()` for validation |
| MCP (`@modelcontextprotocol/sdk`) | the official Go SDK `github.com/modelcontextprotocol/go-sdk` |
| CLI env/XDG defaults (`global.ts`, `flag.ts`) | **deliberate deviation**: the library does not read host env such as `XDG_*` (keeping only host facts like `$HOME`/`COMSPEC`/`LC_*`); the state directory is derived from the **required** `ClientOptions.Root` (`core.NewGlobal(root, home)` is a pure function), and configuration is declared explicitly by `ConfigDirs` |

## 8. Directory/file name alignment

Rules:

- `packages/<pkg>/src/<path>/<name>.ts` → `<pkg>/<path>/<name>.go`,
  **kebab-case → snake_case**, keeping the directory hierarchy — under `pkg/`
  when public, under `internal/` when engine-internal.
- A directory's `index.ts`, if it contained implementation, →
  `<pkg>/<path>/<directory-name>.go`; a pure barrel (re-export only) was not
  ported, because Go has no re-export.

| opencode TypeScript | Go |
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

Common pitfalls:

- Do not create a same-named subpackage for a flat file:
  `core/src/file-mutation.ts` → `pkg/core/file_mutation.go` (package `core`),
  **not** `pkg/core/filemutation/filemutation.go`.
- Type names collide within the same Go package; distinguish them with a
  module-name prefix (e.g. `FileMutationTarget` vs `LocationMutationTarget`,
  `OpenAIChatProtocol` vs `OpenAIResponsesProtocol`).
- `packages/opencode/src/tool/json-schema.ts` was not ported: it only served
  `$ref` inlining for CLI tool schemas, and there is no consumer on the Go side.
  The tool framework is only `pkg/core/tool`.
- `packages/core/src/effect/app-node.ts` and `layer-node.ts` were Effect DI
  infrastructure and were not ported.
- When porting a `packages/core/src/` module, visibility was decided first: if
  the public API named its types, it went to `pkg/core/...`; otherwise
  `internal/core/...`. Run `make check-boundary` after adding a public type.
- Go has no circular dependencies; a TypeScript module cycle (e.g.
  `protocols/` ↔ `protocols/utils/`) had to be refactored away (e.g. move
  `ValidateMedia`, which `utils` reverse-depends on, into `utils`).
- `packages/llm/src/route/transport/index.ts`'s `Transport` /
  `TransportRuntime` / `TransportPrepareInput` go in
  `llm/route/transport.go` (package `route`), because the `transport` package
  reverse-depends on `route` (`Framing`) and putting them in `transport/` would
  create a cycle.

## 9. TypeScript → Go package mapping

The original mapping is preserved here for provenance:

```text
go-agent-sdk/                       opencode TS
├── pkg/            public API (external code imports pkg/...)
│   ├── agent/      ← (no TS dir) high-level public API
│   ├── schema/     ← packages/schema/src/
│   ├── llm/        ← packages/llm/src/
│   │   ├── schema/    LLMRequest, LLMEvent, LLMError, ID types
│   │   ├── route/     Route, Protocol, Endpoint, Auth, Framing, Executor, Transport
│   │   ├── protocols/ per-provider wire protocols
│   │   └── provider/  Provider facades
│   ├── core/       ← packages/core/src/
│   │   ├── tool/
│   │   ├── skill/
│   │   └── systemcontext/
│   ├── mcp/        ← packages/opencode/src/mcp/
│   ├── a2a/        A2A (Go-specific; no TS counterpart)
│   ├── server/     HTTP/SSE API (Go-specific; no TS counterpart)
│   ├── jsonschema/ Go-specific typed JSON Schema AST
│   └── session/    public session-store interfaces
├── internal/       engine internals
│   ├── core/       ← packages/core/src/ internals
│   ├── defs/       load agent/skill definitions
│   └── llm/        ← packages/llm/src/ internals
└── cmd/            standalone commands (Go-specific; no TS counterpart)
```

This mapping is historical; current package responsibilities are documented in
`AGENTS.md` and the current design documents.
