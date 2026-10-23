# go-agent-sdk porting technical plan

> **Status: historical.** The TypeScript/Effect → Go porting phase is complete.
> This plan is retained for provenance and to explain past implementation
> decisions; it is not an active implementation plan. Current scope and API are
> governed by `docs/DESIGN.md` and the other current design documents.

## Overview

This plan defines the technical strategy for porting opencode (TypeScript/Effect) to Go. Goal: produce a Go-native, strongly typed, maintainable SDK.

> ⚠️ **The Go code in this document is illustrative only**, expressing the porting strategy and patterns; it does not represent the final implementation. Every function signature, type definition and error-handling detail is governed by opencode's TypeScript source. When porting, compare against the full logic of the corresponding opencode file.

### Scope definition

go-agent-sdk is an **embedded library**, not a client-server framework. The following opencode capabilities are out of scope for porting:

| Not ported | Reason |
|---|---|
| HTTP server / REST API | The SDK provides no server side and exposes no HTTP endpoints |
| SSE global event subscription | An embedded SDK has no HTTP server; events are returned via Go channels / iter.Seq2 |
| ACP (Agent Client Protocol) | A standard protocol between agent and client, for remote agents; the SDK calls the target directly |
| Plugin system | An inter-process plugin mechanism; not considered at present |
| TUI / CLI / Web UI | The SDK is a core library; the UI layer is implemented by the user |

Porting focus: **the LLM protocol layer, the Agent loop, the tool system, MCP integration, and the permission model**.

### Target Go version and available features

Based on Go 1.27.1, the standard library and language features this SDK may use:

| Go version | Relevant feature | Meaning for us |
|---|---|---|
| **1.24** | **`iter.Seq` / `iter.Seq2` (stabilized)** | **Stream replacement; safe to use** |
| 1.24 | Generic type aliases | Type aliases may carry generic parameters |
| 1.24 | `os.Root` | Safe directory access, sandboxing tool file operations |
| 1.24 | `weak` package | Weak-reference caching, replacing the WeakMap pattern |
| 1.24 | `runtime.AddCleanup` | Improved finalizers, resource cleanup |
| 1.25 | `testing/synctest` (stable) | Testing agent-loop concurrency behavior |
| 1.26 | `new(expression)` for arbitrary expressions | `new(ReadParams{Name: "foo"})` constructs a pointer concisely |
| 1.26 | Generic self-referential constraints | Complex constraint scenarios |
| 1.26 | Green Tea GC (default) | Lower GC overhead, no code changes needed |
| **1.27** | **generic concrete methods** | **Methods may carry their own type parameters, simplifying protocol implementations** |

---

## 1. Overall decision framework

Each porting decision is judged along three dimensions:
1. **Data flow**: public API / internal implementation / JSON boundary
2. **Number of variants**: ≤4 / 5-10 / >10
3. **Semantic needs**: distinguishing "unset vs zero value" / needing defaults / needing exhaustive checks

Below is a per-item decision tree for opencode's high-frequency patterns.

---

## 2. Nullable / optional fields

### Problem

TS/Effect can declare a field optional; Go has no native optional syntax.

### Decision tree

```
Schema.optional(field)
│
├─ The field is at a JSON boundary (API body / persistence)
│  ├─ Need to distinguish "not passed" from "passed a zero value" → *T (pointer)
│  │   e.g. Age  *int  `json:"age,omitempty"`
│  │        nil → not passed; &0 → passed 0
│  │
│  └─ No need to distinguish (zero value = not passed) → value type + omitempty
│      e.g. Email string `json:"email,omitempty"`
│      Note: an empty string "" and "not passed" cannot be distinguished
│
├─ The field is in internal logic (not a JSON boundary)
│  ├─ There is a sensible default → value type + constructor
│  │   e.g. type Config struct { MaxTokens int }
│  │        func NewConfig() Config {
│  │          return Config{MaxTokens: 4096}
│  │        }
│  │
│  ├─ Optional behavior (multiple orthogonal switches) → functional options
│  │   e.g. NewClient(WithTimeout(5s), WithRetry(3))
│  │
│  └─ Need explicit "present/absent" semantics → Optional[T]
│      e.g. type Optional[T any] struct { v T; ok bool }
│           func (o Optional[T]) Ok() bool
```

### Comparison table

| Scenario | Approach | Distinguishes not-passed vs zero | Native JSON support |
|---|---|---|---|
| JSON API body | `*T` + `omitempty` | ✅ | ✅ native |
| JSON body, zero value means not-passed | value type + `omitempty` | ❌ | ✅ native |
| Internal config with defaults | value type + constructor | ❌ | N/A |
| Multiple orthogonal settings | functional options | n/a | N/A |
| Cross-module, needs explicit semantics | `Optional[T]` | ✅ | ❌ must implement yourself |

> **Go 1.26+** `new(expression)` supports evaluating an expression and taking its pointer: `new(ReadParams{Name: "foo"})` replaces `v := ReadParams{Name: "foo"}; return &v`.

---

## 3. Union types

### Problem

TS `type X = A | B | C` has no Go equivalent.

### Decision tree

```
type E = A | B | C
│
├─ ≤ 4 variants with large structural differences between them
│  → separate structs + marker interface
│     type Event interface { isEvent() }
│     type TextEvent struct { Text string }
│     type ImageEvent struct { URL string }
│     func (TextEvent) isEvent() {}   // implicit interface satisfaction
│     consumers dispatch with a type switch
│
├─ 5-10 variants with highly overlapping fields
│  → Tagged Struct + constructors ★(most common)
│     type EventType int
│     type Event struct {
│       Type EventType
│       Text string
│       ToolCall *ToolCallPart  // nil = not a tool_call
│     }
│     constructors guarantee legal states: NewEventText(text)
│     consumers read Type + the corresponding field
│
├─ >10 variants passed across modules (JSON persistence)
│  → json.RawMessage + deferred dispatch
│     type Event struct {
│       Type string          `json:"type"`
│       Data json.RawMessage `json:"data"`
│     }
│     register dispatchers: router.Handle("session.created", handler)
│     each handler Unmarshals itself
│
└─ The variants are a known constant enum (no data structure)
    → type T string + const
       type FinishReason string
       const Stop FinishReason = "stop"
```

### Comparison table (concrete opencode types)

| TS type | Variants | Structural difference | JSON persistence | Go approach |
|---|---|---|---|---|
| `LLMEvent` | 9 | Highly overlapping fields | ❌ | Tagged Struct |
| `ToolContent` | 4 | Large | ✅ → json.RawMessage | marker interface |
| `FinishReason` | 5-6 | Pure values, no structure | ✅ | `type string` + const |
| `SessionEvent` | 20+ | Large | ✅ | json.RawMessage + dispatch |
| `ToolChoice` | 3 | 1 with a parameter | ❌ | struct {Type string; ToolName *string} |
| `ProviderOptions` | N | Varies per provider | ❌ | map[string]any + constructors |

### On marker interfaces

`type Event interface { isEvent() }` fits scenarios with **few variants and large structural differences**. It provides no compiler exhaustiveness check (Go has no sealed interfaces), but is safer than `any` — a runtime type switch to an unregistered type simply will not compile (that type does not implement `isEvent()`). It suits `ToolContent`, which has only 4 variants.

---

## 4. Effect / Schema framework

### Problem

opencode makes heavy use of `Effect.gen`, `Context.Tag`, `Layer`, `Schema.Struct`, `Stream`.

### Decision tree

```
Effect.gen(function* () { yield* service.method() })
│
├─ The function has external dependencies (database, network, etc.)
│  → explicit dependency parameters
│     func myFunc(ctx context.Context, db *DB, input string) (Result, error)
│
├─ The function needs dependency-graph injection (multiple services)
│  → inject at construction
│     type Service struct { db *DB; cache *Cache }
│     func NewService(db *DB, cache *Cache) *Service
│
└─ The function needs a Scope / Finalizer (resource cleanup)
    → context.WithCancel + defer
       ctx, cancel := context.WithCancel(parent)
       defer cancel()
```

```
Effect Stream<T, E>
│
├─ Internal processing (protocol parsing, event conversion)
│  → iter.Seq2[T, error]
│
├─ Needs broadcasting to multiple consumers
│  → chan T (channel)
│
└─ Needs backpressure
    → iter.Seq2 (naturally pull-based) + the consumer controls yield calls
```

```
Schema.Struct({ field: Schema.Type })
│
├─ Intra-module data passing (no JSON boundary)
│  → plain Go struct + compile-time type checking
│
├─ External input (JSON API / user input)
│  → JSON Schema (`santhosh-tekuri/jsonschema/v6`) compile validation + `json.Unmarshal`
│     type Req struct { Name string `json:"name"` }
│     // schema := compileSchema(jsonSchema); schema.Validate(input)
│
└─ Complex Effect Schema composition (Union, Transform, Filter)
    → split into multiple Go structs + hand-written conversion functions
       type Raw struct { ... }     // raw JSON structure
       type Normalized struct { ... }  // business type
       func normalize(r Raw) (Normalized, error)
```

### Comparison table

| TS pattern | Go replacement | Condition |
|---|---|---|
| `Effect.gen` | `(T, error)` | all scenarios |
| `Context.Tag` + `Service` | constructor injection | long-lived dependencies |
| `Layer` | manual `NewX(deps)` | dependency-graph assembly |
| `Scope` / `Effect.addFinalizer` | `defer` / `context.WithCancel` | resource cleanup |
| `Stream<T, E>` | `iter.Seq2[T, error]` | all scenarios |
| `Schema.Struct` | Go struct + `json` tag | simple containers |
| `Schema.Struct` + validate | struct tag + `validator` | validation needed |
| `Schema.Union` / `Schema.TaggedUnion` | see chapter 3 | see chapter 3 |
| `Schema.Codec` (encode/decode) | `json.Marshal` / `json.Unmarshal` | pure JSON |
| `Schema.transform` | hand-written conversion function | specific mapping |
| `Schema.decodeUnknown` | `json.Unmarshal` + constructor validation | untrusted external queue |

---

## 5. Generics

### Problem

TS generics are flexible (higher-kinded generics, conditional types, mapped types); Go generics are limited. Go 1.27 adds **generic methods** (method-level type parameters), but **generic interface methods** are still unsupported.

### Capability boundaries

| Capability | Go 1.26 | Go 1.27+ |
|---|---|---|
| Generic functions | ✅ | ✅ |
| Generic types | ✅ | ✅ |
| Generic methods (a method using the type's parameter) | ✅ `func (List[E]) Len() int` | ✅ |
| Generic methods (a method with its own parameters) | ❌ | ✅ `func (List[E]) Map[R any](f func(E) R) List[R]` |
| Generic interface methods | ❌ | ❌ `type I interface { M[P any]() }` — unsupported |
| Heterogeneous generic collections | ❌ `[]Def[I,O,S]` | ❌ different P,R are still different types |

### Decision tree

```
TS: function fn<T>(x: T): T
│
├─ The type parameter is in the public interface (passed across packages)
│  → erase to an interface (generic interface methods are still unsupported)
│     type Route interface {
│       Stream(ctx, req) iter.Seq2[LLMEvent, error]
│     }
│     the concrete implementation is a generic struct inside the package (unexported)
│
├─ The type parameter is only in the internal implementation, and the method needs extra parameters
│  → use a generic method (Go 1.27+)
│     type protocolImpl[Body, State any] struct { }
│     func (p *protocolImpl[B, S]) Step[F any](state S, frame F) (S, []LLMEvent, error)
│
├─ The type parameter is only in the internal implementation, and the method needs no extra parameters
│  → a method on the generic struct uses the type parameter directly
│     type routeImpl[Body, Prepared any] struct { }
│     func (r *routeImpl[B, P]) Stream(ctx, req) iter.Seq2[LLMEvent, error]
│
├─ Pure data processing (collections, options)
│  → generic method or generic function
│     func (list List[E]) Map[R any](f func(E) R) List[R]   // method
│     func Map[T, R any](items []T, fn func(T) R) []R       // function (slices do not support methods)
│
└─ Needs advanced generics such as conditional types / mapped types
    → split into multiple concrete functions, or use an interface + type assertions
       TS: type Payload<T> = T extends "chat" ? ChatBody : RealtimeBody
       Go: bodyForChat() / bodyForRealtime() separately
```

### Comparison table

| TS generic scenario | Go approach | Condition |
|---|---|---|
| `Route<Body, Prepared>` | internal `impl[B,P]`, external `Route` interface | across package boundaries |
| `Protocol<Body, Frame, State>` | `impl[B,S]` + a `Step[F]` generic method | method-level parameter |
| `Tool<Params, Success>` | `Def[I,O,S]` (built via `Make(Config[I,O,S]{...})`) + external `Tool` interface | heterogeneous registry collections |
| `Promise<T>` / `Effect<T>` | no generics; return `(T, error)` | no erasure needed |
| Collection operations | generic method or generic function | methods for custom types, functions for slices |
| functional options | `type Option[T any] func(*T)` | internal utilities |

> **Go 1.24+** generic type aliases: `type RouteLike[B,P] = routeImpl[B,P]` gives a short alias for a complex generic type.
> **Go 1.27+** generic methods: this document's `Protocol.Step[F]` method uses this feature.

---

## 6. any / unknown / weak typing

### Problem

opencode has many `unknown` variables that converge to known types after Schema validation. Go has no such runtime-narrowing chain.

### Decision tree

```
TS: const x: unknown = payload
     const valid = Schema.decode(Schema.T, x)
│
├─ The variable is at a public API boundary (exported symbol)
│  → never use any; it must be defined as a concrete type
│     e.g. // ❌ Bad: func Execute(params any) any
│          // ✅ Good: func Execute(params *ReadParams) *ReadResult
│
├─ The variable is at a provider protocol decoding boundary
│  → define a temporary native struct, converting to the canonical type at the boundary
│     e.g. // llm/protocols/openai_chat.go
│          type rawChoice struct { Delta *rawDelta `json:"delta"` }
│          func toLLMEvent(raw rawChoice) LLMEvent
│
├─ The variable is at an MCP / plugin tool boundary (external JSON Schema)
│  → map[string]any, unwrapped with a constructor
│     e.g. func readParamsFromMap(m map[string]any) (*ReadParams, error)
│
└─ The variable is at an event-dispatch boundary (SessionEvent)
    → json.RawMessage + each Unmarshals itself
```

### Principles

- Exported API: zero tolerance for `any` / `interface{}`; enforce concrete types
- In-package implementation: temporarily allowed at protocol boundaries, but must be converted to a concrete type before being passed onward
- The only legitimate `any` use: the `map[string]any` input of MCP/plugin tools, unwrapped immediately in a constructor

---

## 7. System context assembly (SystemContext)

### Problem

opencode's `packages/core/src/system-context/index.ts` defines registrable, composable, diffable context sources (upstream git status, current files, agent prompt, time, etc.), assembled into the system prompt before each agent-loop turn. Each `Source<A>` has:

```typescript
interface Source<A> {
  key: Key
  codec: Schema.Codec<A>           // runtime encode/decode
  load: Effect<A | Unavailable>    // fetch the current value
  baseline: (current: A) => string  // produce the initial text
  compare: (prev: A, cur: A) => string  // produce the diff text
}
```

Multiple Sources are merged into one `SystemContext` via `combine()` and sent to the LLM.

### Decision tree

```
Source<A> + codec (runtime encode/decode)
│
├─ The context source is static text (prompt, rules)
│  → return a string directly, no codec/encode-decode
│     type StaticSource struct { Key, Text string }
│
├─ The context source is a dynamically computed value (git diff, file list)
│  → struct interface + functions
│     type ContextSource interface {
│       Key() string
│       Load(ctx) (string, error)         // current value → plain text
│       Compare(ctx, prev string) (string, bool, error)
│     }
│     type GitDiffSource struct { Dir string }
│     func (s GitDiffSource) Load(ctx) (string, error) { /* git diff */ }
│     func (s GitDiffSource) Compare(ctx, prev string) (string, bool, error) {
│       // diff against the previous value
│     }
│
└─ The context source needs to compare before/after changes (incremental update)
    → Compare returns (content string, changed bool, err).
       Only when changed=true is it appended to the system prompt's "diff" section.
```

### Comparison table

| TS concept | Go approach | Note |
|---|---|---|
| `Source<A>` | `ContextSource` interface | interface methods return string, no generics |
| `codec: Schema.Codec<A>` | no counterpart | Source returns already-rendered text, not re-encoded at runtime |
| `baseline(current)` | `Load() string` | returns the plain text of the current value |
| `compare(previous, current)` | `Compare(prev string) (string, bool)` | returns the diff text + whether it changed |
| `combine(sources)` | `func Combine(sources []ContextSource) string` | strings.Builder concatenates in key order |
| `Unavailable` | `Load() (string, error)` — error means unavailable | unavailable sources are skipped |
| Registration | `ContextRegistry map[string]ContextSource` | registered at construction, looked up by key at runtime |

---

## 8. Tool definition and execution runtime

### Problem

In opencode, `Tool.make()` returns a plain object, but runtime information is attached via `WeakMap<AnyTool, Runtime>`. This is an Effect environment-isolation technique — decoupling the tool definition from the execution logic.

```typescript
type Runtime = {
  definition: (name: string) => ToolDefinition   // JSON Schema definition
  settle: (call: ToolCall, ctx: Context) => Effect<ToolOutput, ToolFailure>
}
const runtimes = new WeakMap<AnyTool, Runtime>()
```

### Decision tree

```
Tool.make({ description, input, output, execute })
│
├─ Built-in tools (read, write, shell, etc., with known parameter types)
│  → define `*Def[I,O,S]` with `Make(Config[I,O,S]{...})` and register it in the Registry
│     writeTool := Make(Config[WriteInput, WriteOutput, WriteOutput]{
│       Description: "...", Input: writeInputSchema,
│       Execute: func(input WriteInput, ctx Context) (WriteOutput, error) { ... },
│     })
│     registry.Register(map[string]Tool{"write": writeTool})
│
├─ MCP tools (unknown parameter types, external JSON Schema)
│  → implement the `Tool` interface directly, with `map[string]any` parameters
│     type mcpToolAdapter struct {
│       name string; schema json.Schema; call func(map[string]any) (any, error)
│     }
│     func (a *mcpToolAdapter) Name() string { return a.name }
│     func (a *mcpToolAdapter) Invoke(ctx Context, params map[string]any) (*ToolResult, error) {
│       result, err := a.call(params)
│       if err != nil { return nil, err }
│       return &ToolResult{Output: fmt.Sprint(result)}, nil
│     }
│
└─ Plugin tools (parameters defined by the Plugin)
    → same as MCP tools, map[string]any + constructor unwrapping
```

### WeakMap replacement

TS's `WeakMap<AnyTool, Runtime>` is inlined directly in Go: `Make` returns an opaque handle `*Def[Input, Output, Structured]` that internally holds the `runtime` (definition + settle); the registry erases types via the `Tool` interface (which includes an unexported method).

```go
// core/tool/tool.go
type Def[Input, Output, Structured any] struct { rt *runtime }

type Tool interface { getRuntime() *runtime }

type Config[Input, Output, Structured any] struct {
    Description        string
    Input              *jsonschema.Schema
    Output             *jsonschema.Schema
    Structured         *jsonschema.Schema
    Execute            func(input Input, ctx Context) (Output, error)
    ToStructuredOutput func(input Input, output any) (Structured, error)
    ToModelOutput      func(input Input, output any) []Content
}

func Make[Input, Output, Structured any](cfg Config[Input, Output, Structured]) *Def[Input, Output, Structured]
```

No WeakMap is needed: `Def` holds the runtime directly; `*Def[I,O,S]` is erased through the `Tool` interface and registered in the `Registry`. `WithPermission` returns a new `Def` sharing the runtime (overriding only the permission).

> **Go 1.24+ `weak` package**: if runtime metadata for tool instances later needs caching (like opencode's WeakMap), the standard library `weak` package's weak references can replace it. `weak.Make(ptr)` creates a weak pointer that GC does not prevent from being collected.

### Filesystem safety

Tools that access the filesystem, such as `ReadTool`, `WriteTool`, `GlobTool`, use `os.Root` to restrict access to the session's working directory, preventing path-traversal attacks:

```go
func safeOpen(root *os.Root, path string) (*os.File, error) {
    // os.Root automatically prevents path escapes (e.g. ../../etc/passwd)
    return root.Open(path)
}
```

### Comparison table

| TS pattern | Go approach | Rationale |
|---|---|---|
| `WeakMap<AnyTool, Runtime>` | `Def[I,O,S]` with inlined runtime + `Tool` interface erasure | no WeakMap needed |
| `Schema.Codec` validates input | `santhosh-tekuri/jsonschema/v6` validation + `json.Unmarshal` | the validation schema is derived from `jsonschema.Schema.Validation()` |
| `Schema.encode` encodes output | `json.Marshal` directly | better performance, simpler code |
| `input: Schema.Type<Input>` | `Config.Input: *jsonschema.Schema` | typed AST; `Model()` is sent to the model, `Validation()` is used for validation |
| `withPermission(tool, perm)` | `WithPermission(tool, perm)` returns a new `Def` | shares the runtime, overrides the permission |

---

## 9. Session message state machine

### Problem

opencode's `SessionV1.Part` has type variants (text / tool / file / reasoning / step-start / step-finish / agent), and `ToolPart` has an internal state machine:

```typescript
type ToolPart = {
  type: "tool"
  tool: string
  state:
    | { status: "pending"; input: any }
    | { status: "running"; input: any; title?: string }
    | { status: "completed"; input: any; output: string; title: string }
    | { status: "error"; input: any; error: string }
}
```

### Decision tree

```
TS: type Part = TextPart | ToolPart | FilePart | ReasoningPart | StepStart | ...
│
├─ Part as a persisted session event (JSON storage, cross-process)
│  → json.RawMessage + deferred dispatch (same as strategy 3)
│     type Part struct {
│       Type string          `json:"type"`
│       Data json.RawMessage `json:"data,omitempty"`
│     }
│
├─ Part processed in memory (message-list rendering, context assembly)
│  → Tagged struct + constructors (same as strategy 1)
│     type PartType int
│     type Part struct {
│       Type     PartType
│       Text     string
│       ToolID   string
│       ToolInput map[string]any
│       ToolOutput string
│       ToolError  string
│       // ...
│     }
│
└─ Tool state machine (pending → running → completed/error)
    → a struct with state, driven by methods
       type ToolState int
       type ToolPart struct {
         Tool     string
         Input    map[string]any
         State    ToolState
         Output   string
         Title    string
         Error    string
       }
       func (p *ToolPart) ToRunning() { p.State = StateRunning }
       func (p *ToolPart) ToCompleted(output, title string) { p.State = StateCompleted; ... }
```

### Comparison table

| TS pattern | Go approach | Condition |
|---|---|---|
| `Part = TextPart \| ToolPart \| ...` in memory | Tagged Struct | high-frequency operations, needs a fast type switch |
| `Part = TextPart \| ToolPart \| ...` JSON persistence | json.RawMessage + dispatch | cross-process, event sourcing |
| `ToolPart.state = pending \| running \| ...` | struct + State + methods | state transitions within a single type |
| `type {status: "completed"; output: string}` | `ToolPart{State: Completed, Output: "..."}` | unified handling via a tagged struct |

---

## 10. Agent-loop concurrent tool execution

### Problem

opencode's SessionRunner dispatches immediately on receiving a streaming `tool-call` event (eager execution). Multiple tools can run concurrently (FiberSet), and a continuation is issued after all complete.

```typescript
// pseudocode
stream: for await (event of llm.stream(request)) {
  if (event.type === "tool-call") {
    executes.add(fiber(event))   // execute concurrently immediately
  }
}
await FiberSet.join(executes)   // wait for all to complete
// reload history and continue to the next round
```

### Decision tree

```
The LLM returns tool-call events via streaming
│
├─ Few tool-calls (1-3)
│  → goroutine + errgroup
│     eg, ctx := errgroup.WithContext(ctx)
│     for _, call := range toolCalls {
│       call := call
│       eg.Go(func() error { return executeTool(ctx, call) })
│     }
│     if err := eg.Wait(); err != nil { ... }
│     // all done, continue to the next round
│
├─ Many tool-calls (10+), concurrency must be limited
│  → worker pool + channel
│     calls := make(chan ToolCall, len)
│     for i := 0; i < poolSize; i++ {
│       eg.Go(func() error { for c := range calls { executeTool(c) }; return nil })
│     }
│     for _, c := range toolCalls { calls <- c }
│     close(calls)
│     eg.Wait()
│
└─ Need to keep processing LLM stream events while tools execute
    → two goroutines: one receiving stream events, one executing tools
       // goroutine 1: stream events
       go func() {
         for event := range stream { handleStreamEvent(event) }
       }()
       // goroutine 2: tool execution
       eg.Go(func() error { for call := range toolCalls { execute(call) }; return nil })
```

### FiberSet replacement

```go
// In opencode FiberSet is an appendable concurrent collection that returns after all are joined.
// In Go, replace it with errgroup.Group:
type ToolExecutor struct {
    eg      *errgroup.Group
    ctx     context.Context
    results chan ToolResult
}

func (e *ToolExecutor) Submit(call ToolCall) {
    e.eg.Go(func() error {
        result := executeTool(e.ctx, call)
        e.results <- result
        return result.Err
    })
}

func (e *ToolExecutor) Wait() []ToolResult {
    e.eg.Wait()
    close(e.results)
    var results []ToolResult
    for r := range e.results { results = append(results, r) }
    return results
}
```

---

## 11. MCP JSON-RPC protocol

### Problem

opencode's MCP uses JSON-RPC 2.0 over HTTP/SSE, with Schema defining the request/response structures.

```typescript
const McpRequest = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Literal(1),
  method: Schema.Literal("tools/call"),
  params: Schema.Struct({ name: Schema.String, arguments: args }),
})
```

### Decision tree

```
JSON-RPC 2.0 protocol
│
├─ Request construction
│  → plain struct + json.Marshal
│     type RPCRequest struct {
│       JSONRPC string `json:"jsonrpc"`
│       ID      int    `json:"id"`
│       Method  string `json:"method"`
│       Params  any    `json:"params"`
│     }
│
├─ Response parsing
│  → struct + json.Unmarshal
│     type RPCResponse struct {
│       JSONRPC string          `json:"jsonrpc"`
│       ID      int             `json:"id"`
│       Result  json.RawMessage `json:"result,omitempty"`
│       Error   *RPCError       `json:"error,omitempty"`
│     }
│
├─ Transport layer
│  ├─ stdio child process → os/exec.Cmd + bufio
│  ├─ HTTP/SSE  → net/http + bufio.Scanner parsing the `data:` prefix line by line
│  └─ unified interface
│     type MCPTransport interface {
│       Send(ctx context.Context, msg []byte) error
│       Recv(ctx context.Context) ([]byte, error)
│       Close() error
│     }
│
└─ Tool adaptation
    → MCP tool definition → Tool interface
       func AdaptTool(mcpTool MCPTool) Tool {
         return &mcpToolAdapter{name: mcpTool.Name, schema: mcpTool.InputSchema,
           call: mcpTool.Call}
       }
       type mcpToolAdapter struct {
         name string; schema json.Schema; call func(map[string]any) (any, error)
       }
       func (a *mcpToolAdapter) Name() string { return a.name }
       func (a *mcpToolAdapter) InputSchema() json.Schema { return a.schema }
       func (a *mcpToolAdapter) Invoke(ctx Context, params map[string]any) (*ToolResult, error) {
         result, err := a.call(params)
         if err != nil { return nil, err }
         return &ToolResult{Output: fmt.Sprint(result)}, nil
       }
```

### Comparison table

| TS pattern | Go approach | Note |
|---|---|---|
| `Schema.Struct({jsonrpc: "2.0"...})` | plain Go struct + json tag | no Schema runtime overhead |
| `Schema.decodeUnknown(body)` | `json.Unmarshal(body, &rpcResp)` | standard-library deserialization |
| `HttpClient.filterStatusOk(http)` | `resp.StatusCode >= 200 && < 300` | direct check |
| `HttpClientRequest.schemaBodyJson(...)` | `json.Marshal(req)` + `bytes.NewReader` | explicit encoding |
| Effect Timeout | `context.WithTimeout` | standard library |

---

## 12. Config loading: user-defined agents

### Problem

opencode's agent and skill definitions come from `{agent,agents}/**/*.md`, `{mode,modes}/*.md` and `{skill,skills}/**/SKILL.md`, in the format of YAML frontmatter + markdown body.

### Strategy

**Support markdown only** (frontmatter + body), with a format exactly matching TS, without introducing a dual YAML format:

- The parser `core/util/markdown.go` (`ParseMarkdownOption`) ports TS's `ConfigMarkdown.parse` line by line (including `SanitizeMarkdown` retry), without depending on a third-party frontmatter library.
- Frontmatter is deserialized into `defs/config.AgentInfo` (json tags), and the body becomes `system` (mode files additionally set `mode: primary`).
- **No directory discovery**: directories are given explicitly by `ClientOptions.ConfigDirs`, and the SDK scans only those (see DESIGN.md §11.1).
- Legacy v1 agent markdown (old field names → `V1MigrateAgent`) is not ported; `core/v1/config` has been deleted.

### Comparison table

| opencode approach | Go approach |
|---|---|
| `Schema.decodeUnknownExit(Schema)(config)` | `json.Unmarshal(frontmatter, &AgentInfo)` |
| `ConfigMarkdown.parse` (gray-matter) | `util.ParseMarkdownOption` (ported line by line, with sanitize retry) |
| `{agent,agents}/**/*.md` | same (`core.Glob` + `doublestar`) |
| markdown body → system prompt | `AgentInfo.System` |
| directory discovery (upward search for `.opencode` + env + global dirs) | `ClientOptions.ConfigDirs` explicit directories |

---

## 13. Summary: one-line decisions

| TS pattern | One-line decision |
|---|---|
| `Schema.optional(field)` | use `*T` at JSON boundaries, and a value type + constructor internally when a default exists |
| `type E = A \| B \| C` | few variants with large differences → marker interface; many overlapping variants → tagged struct; cross-module persistence → json.RawMessage |
| `Effect.gen` / `Effect<T,E>` | write `(T, error)` directly, passing dependencies explicitly |
| `Stream<T,E>` | use `iter.Seq2[T, error]` |
| `Context.Tag` + `Layer` | constructor injection, no DI framework |
| `Schema.Struct` | Go struct + json tag |
| `Route<Body,Prepared>` | in-package generic struct + exported interface erasure (heterogeneous collections require it) |
| `Protocol<Body,Frame,State,Event>` | `impl[B,S]` + a `Step[F]` generic method (Go 1.27+) |
| `Tool<Params,Success>` | `Def[I,O,S]` generic definition + `Tool` interface-erased registration |
| `unknown` | zero tolerance in exported APIs; convert to a concrete type at in-package boundaries |
| `Source<A>` (SystemContext) | `ContextSource` interface returning text, no Codec |
| `WeakMap<AnyTool, Runtime>` | inline the runtime field in `Def` |
| `Part = TextPart \| ToolPart \| ...` | in memory → tagged struct; persistence → json.RawMessage |
| `FiberSet.join(executes)` | `errgroup.Group` + goroutines |
| `McpRequest({jsonrpc:"2.0"...})` | plain struct + `json.Marshal` |
| `{agent,agents}/**/*.md` frontmatter | dual support for `*.md` + `*.yaml` |

## 14. Symbol cross-reference (Go ↔ TS)

Used when reviewing a port to locate the TS source. File names themselves follow `docs/PORTING-REFERENCE.md`'s rules
(`packages/<pkg>/src/<path>/<name>.ts` → `<pkg>/<path>/<name>.go`, kebab-case → snake_case);
the table below lists only what **cannot be derived by the rule**: when the TS source file and the Go file have different names, the actual path is given, and when a symbol name differs from the Go name, the mapping is given.

| Go | TS source | Symbols with different names |
|---|---|---|
| `agent/task.go` | packages/opencode/src/tool/task.txt | `AgentPermissions` ↔ `registry.describeTask` |
| `core/agent.go` | packages/core/src/agent.ts | `AgentDefaultID` ↔ `AgentV2.defaultID` |
| `core/agent_defaults.go` | packages/core/src/plugin/agent.ts | — |
| `core/filesystem.go` | packages/core/src/filesystem.ts, packages/schema/src/filesystem.ts | `FileSystemEntry` ↔ `FileSystem.Entry`, `FileSystemSubmatch` ↔ `FileSystem.Submatch`, `FileSystemMatch` ↔ `FileSystem.Match` |
| `core/fs_util.go` | packages/core/src/fs-util.ts | `RemoveAll` ↔ `fs.remove` |
| `core/image.go` | packages/core/src/image.ts | `Image.Normalize` ↔ `Image.normalize`; the Go-side resizer is a compile-time dependency, and `Image.ResizerUnavailableError` is not ported |
| `core/location_mutation.go` | packages/core/src/location-mutation.ts | `locationMutationSlash` ↔ `FSUtil.slash` |
| `core/model.go` | packages/core/src/model.ts | `ParseModel` ↔ `ModelV2.parse` |
| `core/patch.go` | packages/core/src/patch.ts | `PatchParse` ↔ `Patch.parse`, `PatchDerive` ↔ `Patch.derive`, `PatchJoinBom` ↔ `Patch.joinBom`, `patchSplitBom` ↔ `patch.ts` |
| `core/plugin.go` | packages/core/src/plugin.ts | `PluginID` ↔ `Plugin.ID`, `Plugin` ↔ `PluginV2.Service` |
| `core/process.go` | packages/core/src/process.ts | — |
| `core/process_unix.go` | packages/core/src/process-unix.ts | `applyProcessAttrs` ↔ `ChildProcess`, `processTerminate` ↔ `ChildProcess` |
| `core/process_windows.go` | packages/core/src/process-windows.ts | `applyProcessAttrs` ↔ `ChildProcess`, `processTerminate` ↔ `ChildProcess` |
| `core/provider.go` | packages/core/src/provider.ts | `ProviderInfoEmpty` ↔ `ProviderV2.Info.empty` |
| `core/reference.go` | packages/core/src/reference.ts | `Reference` ↔ `Reference.Service` |
| `core/reference/guidance.go` | packages/core/src/reference/guidance.ts | — |
| `core/repository.go` | packages/core/src/repository.ts | `RepositoryReference` ↔ `Repository.Reference`, `RepositoryParse` ↔ `Repository.parse`, `RepositoryIsFile` ↔ `Repository.isFile`, `RepositoryIsRemote` ↔ `Repository.isRemote`, `RepositoryValidateBranch` ↔ `Repository.validateBranch`, `RepositoryCachePath` ↔ `Repository.cachePath`, `RepositoryCacheIdentity` ↔ `Repository.cacheIdentity`, `RepositorySame` ↔ `Repository.same` |
| `core/repository_cache.go` | packages/core/src/repository-cache.ts | `RepositoryCacheResult` ↔ `RepositoryCache.Result`, `RepositoryCacheEnsureInput` ↔ `RepositoryCache.EnsureInput`, `RepositoryCache` ↔ `RepositoryCache.Service` |
| `core/ripgrep.go` | packages/core/src/ripgrep.ts | `ripgrepTruncateText` ↔ `text.length` |
| `core/session/compaction.go` | packages/core/src/session/compaction.ts | `CompactionEntry` ↔ `SessionCompaction.Entry`, `CompactionSettings` ↔ `SessionCompaction.Settings`, `CompactionDependencies` ↔ `SessionCompaction.Dependencies`, `CompactionInput` ↔ `SessionCompaction.Input`, `NewCompaction` ↔ `SessionCompaction.make`, `BuildCompactionPrompt` ↔ `SessionCompaction.buildPrompt`, `compactionUTF16Length` ↔ `String.prototype.length`, `compactionUTF16Slice` ↔ `String.prototype.slice` |
| `core/session/context_epoch.go` | packages/core/src/session/context-epoch.ts | `ContextEpochInitialize` ↔ `SessionContextEpoch.initialize`, `ContextEpochPrepare` ↔ `SessionContextEpoch.prepare`, `ContextEpochReset` ↔ `SessionContextEpoch.reset`, `latestEventSequence` ↔ `EventV2.latestSequence` |
| `core/session/error.go` | packages/core/src/session/error.ts | — |
| `core/session/history.go` | packages/core/src/session/history.ts | `HistoryEntry` ↔ `SessionHistory.entriesForRunner` |
| `core/session/info.go` | packages/core/src/session/info.ts | — |
| `core/session/input.go` | packages/core/src/session/input.ts | `AdmitInput` ↔ `SessionInput.admit`, `ProjectAdmittedInput` ↔ `SessionInput.projectAdmitted`, `ProjectPromptedInput` ↔ `SessionInput.projectPrompted` |
| `core/session/message_updater.go` | packages/core/src/session/message-updater.ts | — |
| `core/session/projector.go` | packages/core/src/session/projector.ts | `projectorAdapter` ↔ `SessionProjector` |
| `core/session/revert.go` | packages/core/src/session/revert.ts | `RevertMessageNotFoundError` ↔ `SessionRevert.MessageNotFoundError`, `RevertPlan` ↔ `SessionRevert.plan`, `RevertStage` ↔ `SessionRevert.stage`, `RevertClear` ↔ `SessionRevert.clear`, `RevertCommit` ↔ `SessionRevert.commit` |
| `core/session/run_coordinator.go` | packages/core/src/session/run-coordinator.ts | — |
| `core/session/runner/llm.go` | packages/core/src/session/runner/llm.ts | `LLMRunner` ↔ `SessionRunner.layer`, `errInterrupted` ↔ `Effect.interrupt` |
| `core/session/runner/max_steps.go` | packages/core/src/session/runner/max-steps.ts | `MaxStepsPrompt` ↔ `MAX_STEPS_PROMPT` |
| `core/session/runner/model.go` | packages/core/src/session/runner/model.ts | `runnerModelWithDefaults` ↔ `withDefaults`, `runnerModelWithVariant` ↔ `withVariant` |
| `core/session/runner/publish_llm_event.go` | packages/core/src/session/runner/publish-llm-event.ts | `NewLLMEventPublisher` ↔ `createLLMEventPublisher` |
| `core/session/runner/runner.go` | packages/core/src/session/runner/runner.ts | — |
| `core/session/runner/to_llm_message.go` | packages/core/src/session/runner/to-llm-message.ts | `llmToolResult` ↔ `toolResult`, `llmResultMetadata` ↔ `provider.resultMetadata` |
| `core/session/store.go` | packages/core/src/session/store.ts | `Store` ↔ `SessionStore.Service`, `StoreMessage` ↔ `SessionStore.message` |
| `core/session/todo.go` | packages/core/src/session/todo.ts | `Todo` ↔ `SessionTodo.Service` |
| `core/skill.go` | packages/core/src/skill.ts | `SkillAvailable` ↔ `SkillV2.available`, `Skill` ↔ `SkillV2.Service` |
| `core/skill/discovery.go` | packages/core/src/skill/discovery.ts | — |
| `core/skill/guidance.go` | packages/core/src/skill/guidance.ts | — |
| `core/snapshot.go` | packages/core/src/snapshot.ts | `SnapshotError` ↔ `Snapshot.Error`, `Snapshot` ↔ `Snapshot.Service` |
| `core/state.go` | packages/core/src/state.ts | `StateOptions` ↔ `State.Options`, `State` ↔ `State.create`, `StateBatch` ↔ `State.batch` |
| `core/systemcontext/registry.go` | packages/core/src/systemcontext/registry.ts | — |
| `core/systemcontext/systemcontext.go` | packages/core/src/systemcontext/systemcontext.ts | — |
| `core/tool/apply_patch.go` | packages/core/src/tool/apply-patch.ts | — |
| `core/tool/bash.go` | packages/core/src/tool/bash.ts | `bashResolve` ↔ `FSUtil.resolve` |
| `core/tool/glob.go` | packages/core/src/tool/glob.ts | `GlobMaxSafeInteger` ↔ `Number.MAX_SAFE_INTEGER`, `globResolve` ↔ `path.resolve` |
| `core/tool/grep.go` | packages/core/src/tool/grep.ts | `GrepMaxSafeInteger` ↔ `Number.MAX_SAFE_INTEGER`, `grepResolve` ↔ `path.resolve` |
| `core/tool/skill.go` | packages/core/src/tool/skill.ts | `SkillToModelOutput` ↔ `SkillTool.toModelOutput` |
| `core/tool/todowrite.go` | packages/core/src/tool/todowrite.ts | `TodoWriteToModelOutput` ↔ `TodoWriteTool.toModelOutput` |
| `core/tool/webfetch.go` | packages/core/src/tool/webfetch.ts | — |
| `core/tool/webfetch_turndown.go` | packages/core/src/tool/webfetch-turndown.ts | `turndownTrimSpace` ↔ `String.prototype.trim` |
| `core/tool_output_store.go` | packages/core/src/tool-output-store.ts | `ToolOutputStoreBoundInput` ↔ `ToolOutputStore.BoundInput`, `ToolOutputStoreBoundResult` ↔ `ToolOutputStore.BoundResult`, `ToolOutputStoreStorageError` ↔ `ToolOutputStore.StorageError` |
| `core/util/diff.go` | packages/core/src/util/diff.ts | — |
| `core/util/glob.go` | util/glob.ts | `GlobOptions` ↔ `Glob.Options`, `GlobScan` ↔ `Glob.scan`, `GlobMatch` ↔ `Glob.match` |
| `core/util/jsonc.go` | packages/core/src/util/jsonc.ts | — |
| `core/util/locale.go` | packages/core/src/util/locale.ts | `CompareLocale` ↔ `String.prototype.localeCompare` |
| `core/util/markdown.go` | packages/core/src/util/markdown.ts | `ParseMarkdown` ↔ `ConfigMarkdown.parse`, `ParseMarkdownOption` ↔ `ConfigMarkdown.parseOption`, `SanitizeMarkdown` ↔ `ConfigMarkdown.sanitize` |
| `examples/internal/util/util.go` | — | — |
| `jsonschema/jsonschema.go` | — | — |
| `mcp/catalog.go` | index.ts | `serverCapabilities` ↔ `client.getServerCapabilities()`, `ListPrompts` ↔ `McpCatalog.prompts`, `ListResources` ↔ `McpCatalog.resources`, `ListResourceTemplates` ↔ `McpCatalog.resourceTemplates` |
| `mcp/client.go` | packages/opencode/src/mcp/client.ts | `timeouts` ↔ `convertTool`, `sanitizedPromptKey` ↔ `McpCatalog.fetch`, `escapedResourceKey` ↔ `McpCatalog.fetch` |
| `mcp/config.go` | packages/opencode/src/mcp/config.ts | `DefaultTimeout` ↔ `DEFAULT_TIMEOUT` |
| `mcp/tool.go` | packages/opencode/src/mcp/tool.ts | `AdaptMCPTool` ↔ `McpCatalog.convertTool`, `MCPInputSchema` ↔ `convertTool` |
| `defs/config/entry_name.go` | packages/opencode/src/config/entry-name.ts | — |
| `schema/agent.go` | packages/schema/src/agent.ts | `AgentInfoEmpty` ↔ `Agent.Info.empty` |
| `schema/json_required.go` | packages/schema/src/json-required.ts | `allowsJSONNull` ↔ `Schema.Unknown` |
| `schema/llm.go` | packages/schema/src/llm.ts | — |
| `schema/model.go` | packages/schema/src/model.ts | `ModelRef` ↔ `Model.Ref`, `ModelCapabilities` ↔ `Model.Capabilities`, `ModelCost` ↔ `Model.Cost`, `ModelRequest` ↔ `Model.Info.request`, `ModelInfo` ↔ `ModelV2.Info`, `ModelInfoEmpty` ↔ `Model.Info.empty` |
| `schema/plugin.go` | packages/schema/src/plugin.ts | `PluginID` ↔ `Plugin.ID`, `PluginEventAdded` ↔ `Plugin.Event.Added` |
| `schema/prompt.go` | packages/schema/src/prompt.ts | `PromptSource` ↔ `Prompt.Source`, `PromptFileAttachment` ↔ `Prompt.FileAttachment`, `PromptAgentAttachment` ↔ `Prompt.AgentAttachment`, `PromptFromUserMessage` ↔ `Prompt.Prompt.fromUserMessage` |
| `schema/prompt_input.go` | packages/schema/src/prompt-input.ts | `PromptInputFileAttachment` ↔ `PromptInput.FileAttachment`, `PromptInput` ↔ `PromptInput.Prompt` |
| `schema/provider.go` | packages/schema/src/provider.ts | `ProviderAISDK` ↔ `Provider.AISDK`, `ProviderNative` ↔ `Provider.Native`, `ProviderAPI` ↔ `Provider.Api`, `ProviderRequest` ↔ `Provider.Request`, `ProviderInfo` ↔ `ProviderV2.Info`, `ProviderInfoEmpty` ↔ `Provider.Info.empty` |
| `schema/reference.go` | packages/schema/src/reference.ts | `ReferenceLocalSource` ↔ `Reference.LocalSource`, `ReferenceGitSource` ↔ `Reference.GitSource`, `ReferenceInfo` ↔ `Reference.Info`, `ReferenceEventUpdated` ↔ `Reference.Event.Updated` |
| `schema/revert.go` | packages/schema/src/revert.ts | `RevertFileDiff` ↔ `Revert.FileDiff`, `RevertState` ↔ `Revert.State` |
| `schema/session.go` | packages/schema/src/session.ts | `SessionInfo` ↔ `SessionV2.Info`, `SessionListAnchor` ↔ `Session.ListAnchor` |
| `schema/session_delivery.go` | packages/schema/src/session-delivery.ts | `SessionDelivery` ↔ `SessionDelivery.Delivery` |
| `schema/session_event.go` | packages/schema/src/session-event.ts | `SessionEventRetryError` ↔ `SessionEvent.RetryError`, `SessionEventDurableDefinitions` ↔ `SessionEvent.DurableDefinitions`, `SessionEventDefinitions` ↔ `SessionEvent.Definitions` |
| `schema/session_input.go` | packages/schema/src/session-input.ts | `SessionInputAdmitted` ↔ `SessionInput.Admitted` |
| `schema/session_message.go` | packages/schema/src/session-message.ts | — |
| `schema/session_status_event.go` | packages/schema/src/session-status-event.ts | — |
| `schema/session_todo.go` | packages/schema/src/session-todo.ts | `SessionTodoInfo` ↔ `SessionTodo.Info` |
| `schema/skill.go` | packages/schema/src/skill.ts | `SkillSourceEquals` ↔ `Skill.Source.equals`, `SkillSourceKey` ↔ `Skill.Source.key` |
| `schema/v1/session.go` | packages/schema/src/v1/session.ts | `V1SessionSummary` ↔ `SessionV1.SessionSummary`, `V1SessionTokens` ↔ `SessionV1.SessionTokens`, `V1SessionRevert` ↔ `SessionV1.SessionRevert`, `V1SessionModel` ↔ `SessionV1.SessionModel`, `V1SessionTime` ↔ `SessionV1.SessionInfo.time`, `V1SessionInfo` ↔ `SessionV1.SessionInfo` |
| `schema/workspace_event.go` | packages/schema/src/workspace-event.ts | `WorkspaceConnectionStatus` ↔ `WorkspaceEvent.ConnectionStatus` |
