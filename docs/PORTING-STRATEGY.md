# go-agent-sdk 移植技术方案

## 概述

本方案定义从 opencode (TypeScript/Effect) 移植到 Go 时的技术策略。目标：产出 Go native、强类型、可维护的 SDK。

> ⚠️ **本文档中的 Go 代码仅为示意**，用于表达移植策略和模式，不代表最终实现。每个函数签名、类型定义、错误处理的细节都以 opencode 的 TypeScript 源码为准。移植时请对照 opencode 对应文件的完整逻辑。

### 范围界定

go-agent-sdk 是**嵌入式库**，不是一个客户端-服务器框架。以下 opencode 功能不在移植范围内：

| 不移植 | 原因 |
|---|---|
| HTTP 服务器 / REST API | SDK 不提供服务端，不暴露 HTTP 端点 |
| SSE 全局事件订阅 | 嵌入式 SDK 无 HTTP 服务器，事件通过 Go channel / iter.Seq2 返回 |
| ACP (Agent Client Protocol) | agent-client 间的标准协议，适用于远端 agent，SDK 直接调用目标 |
| Plugin 系统 | 进程间插件机制，当前不考虑 |
| TUI / CLI / Web UI | SDK 是核心库，UI 层由使用者实现 |

移植重点：**LLM 协议层、Agent 循环、工具系统、MCP 集成、权限模型**。

### 目标 Go 版本与可用特性

基于 Go 1.27.1，本 SDK 可使用的标准库及语言特性：

| Go 版本 | 相关特性 | 对我们的意义 |
|---|---|---|
| **1.24** | **`iter.Seq` / `iter.Seq2`（正式稳定）** | **Stream 替代，可放心使用** |
| 1.24 | 泛型类型别名 | 类型别名可携带泛型参数 |
| 1.24 | `os.Root` | 安全目录访问，工具文件操作沙箱 |
| 1.24 | `weak` 包 | 弱引用缓存，替代 WeakMap 模式 |
| 1.24 | `runtime.AddCleanup` | 改进的 finalizer，资源清理 |
| 1.25 | `testing/synctest`（正式） | 测试 agent 循环并发行为 |
| 1.26 | `new(expression)` 支持任意表达式 | `new(ReadParams{Name: "foo"})` 简洁构造指针 |
| 1.26 | 泛型自引用约束 | 复杂约束场景 |
| 1.26 | Green Tea GC（默认） | 更低 GC 开销，无需代码改动 |
| **1.27** | **泛型方法（generic concrete methods）** | **方法可携带独立类型参数，简化协议实现** |

---

## 一、决策总纲

每条移植决策按三个维度判断：
1. **数据流向**：公开 API / 内部实现 / JSON 边界
2. **变体数量**：≤4 / 5-10 / >10
3. **语义需求**：需要区分"未设置 vs 零值" / 需要默认值 / 需要穷举检查

下面是按 opencode 高频模式的逐项决策树。

---

## 二、可空字段 / 可选字段

### 问题

TS/Effect 可以声明字段可选，Go 没有原生可选语法。

### 决策树

```
Schema.optional(field)
│
├─ 字段在 JSON 边界（API body / 持久化）
│  ├─ 需要区分"未传"和"传了零值" → *T (指针)
│  │   例: Age  *int  `json:"age,omitempty"`
│  │       nil → 未传；&0 → 传了0
│  │
│  └─ 不需要区分（零值=未传）→ 值类型 + omitempty
│      例: Email string `json:"email,omitempty"`
│      注意: 空串 "" 和"未传"无法区分
│
├─ 字段在内部逻辑（非JSON边界）
│  ├─ 有合理默认值 → 值类型 + 构造器
│  │   例: type Config struct { MaxTokens int }
│  │       func NewConfig() Config {
│  │         return Config{MaxTokens: 4096}
│  │       }
│  │
│  ├─ 可选行为（多个正交开关）→ functional options
│  │   例: NewClient(WithTimeout(5s), WithRetry(3))
│  │
│  └─ 需要明确"有/无"语义 → Optional[T]
│      例: type Optional[T any] struct { v T; ok bool }
│          func (o Optional[T]) Ok() bool
```

### 对照表

| 场景 | 做法 | 区分未传 vs 零值 | JSON 原生支持 |
|---|---|---|---|
| JSON API body | `*T` + `omitempty` | ✅ | ✅ 原生 |
| JSON body，零值即未传 | 值类型 + `omitempty` | ❌ | ✅ 原生 |
| 内部配置，有默认值 | 值类型 + 构造器 | ❌ | N/A |
| 多个正交配置 | functional options | 不适用 | N/A |
| 跨模块，需明确语义 | `Optional[T]` | ✅ | ❌ 需自实现 |

> **Go 1.26+** `new(expression)` 支持对表达式求值后取指针：`new(ReadParams{Name: "foo"})` 取代 `v := ReadParams{Name: "foo"}; return &v`。

---

## 三、Union 类型

### 问题

TS `type X = A | B | C` 没有 Go 等价物。

### 决策树

```
type E = A | B | C
│
├─ 变体 ≤ 4 且变体间结构差异大
│  → 独立的 struct + marker interface
│     type Event interface { isEvent() }
│     type TextEvent struct { Text string }
│     type ImageEvent struct { URL string }
│     func (TextEvent) isEvent() {}   // 隐式实现接口
│     消费者用 type switch 分发
│
├─ 变体 5-10 且变体间字段高度重叠
│  → Tagged Struct + 构造器 ★(最常用)
│     type EventType int
│     type Event struct {
│       Type EventType
│       Text string
│       ToolCall *ToolCallPart  // nil = 非 tool_call
│     }
│     构造器保证合法状态: NewEventText(text)
│     消费者读 Type + 对应字段
│
├─ 变体 >10 且跨模块传递（JSON 持久化）
│  → json.RawMessage + 延迟分发
│     type Event struct {
│       Type string          `json:"type"`
│       Data json.RawMessage `json:"data"`
│     }
│     注册分发器: router.Handle("session.created", handler)
│     各 handler 自己 Unmarshal
│
└─ 变体是已知常量枚举（无数据结构）
    → type T string + const
       type FinishReason string
       const Stop FinishReason = "stop"
```

### 对照表（opencode 具体类型）

| TS 类型 | 变体数 | 结构差异 | JSON 持久化 | Go 方案 |
|---|---|---|---|---|
| `LLMEvent` | 9 | 字段高度重叠 | ❌ | Tagged Struct |
| `ToolContent` | 4 | 差异大 | ✅ → json.RawMessage | marker interface |
| `FinishReason` | 5-6 | 纯值，无结构 | ✅ | `type string` + const |
| `SessionEvent` | 20+ | 差异大 | ✅ | json.RawMessage + 分发 |
| `ToolChoice` | 3 | 1 个带参 | ❌ | struct {Type string; ToolName *string} |
| `ProviderOptions` | N | 各 provider 不同 | ❌ | map[string]any + 构造器 |

### 关于 marker interface

`type Event interface { isEvent() }` 适用于**变体少且结构差异大**的场景，它本质上不提供编译器穷举检查（Go 不支持 sealed interface），但比 `any` 安全——运行时 type switch 到未注册类型会直接编译不过（该类型没实现 `isEvent()`）。适合 `ToolContent` 这种只有 4 个变体的场景。

---

## 四、Effect / Schema 框架

### 问题

opencode 大量使用 `Effect.gen`、`Context.Tag`、`Layer`、`Schema.Struct`、`Stream`。

### 决策树

```
Effect.gen(function* () { yield* service.method() })
│
├─ 函数有外部依赖（数据库、网络等）
│  → 显式依赖参数
│     func myFunc(ctx context.Context, db *DB, input string) (Result, error)
│
├─ 函数需要依赖图注入（多个 service）
│  → 构造时注入
│     type Service struct { db *DB; cache *Cache }
│     func NewService(db *DB, cache *Cache) *Service
│
└─ 函数需要 Scope / Finalizer（资源清理）
    → context.WithCancel + defer
       ctx, cancel := context.WithCancel(parent)
       defer cancel()
```

```
Effect Stream<T, E>
│
├─ 内部处理（协议解析、事件转换）
│  → iter.Seq2[T, error]
│
├─ 需要广播给多个消费者
│  → chan T（channel）
│
└─ 需要 backpressure
    → iter.Seq2（天然 pull-based）+ 消费者控制 yield 调用
```

```
Schema.Struct({ field: Schema.Type })
│
├─ 模块内部数据传递（无 JSON 边界）
│  → 普通 Go struct + 编译时类型检查
│
├─ 外部入参（JSON API / 用户输入）
│  → JSON Schema（`santhosh-tekuri/jsonschema/v6`）编译校验 + `json.Unmarshal`
│     type Req struct { Name string `json:"name"` }
│     // schema := compileSchema(jsonSchema); schema.Validate(input)
│
└─ Effect Schema 的复杂组合（Union、Transform、Filter）
    → 拆为多个 Go struct + 手写转换函数
       type Raw struct { ... }     // JSON 原始结构
       type Normalized struct { ... }  // 业务类型
       func normalize(r Raw) (Normalized, error)
```

### 对照表

| TS 模式 | Go 替代 | 条件 |
|---|---|---|
| `Effect.gen` | `(T, error)` | 所有场景 |
| `Context.Tag` + `Service` | 构造器注入 | 有长生命周期依赖 |
| `Layer` | 手动 `NewX(deps)` | 依赖图组装 |
| `Scope` / `Effect.addFinalizer` | `defer` / `context.WithCancel` | 资源清理 |
| `Stream<T, E>` | `iter.Seq2[T, error]` | 所有场景 |
| `Schema.Struct` | Go struct + `json` tag | 简单容器 |
| `Schema.Struct` + validate | struct tag + `validator` | 需要校验 |
| `Schema.Union` / `Schema.TaggedUnion` | 见第三章 | 见第三章 |
| `Schema.Codec` (encode/decode) | `json.Marshal` / `json.Unmarshal` | 纯 JSON |
| `Schema.transform` | 手写转换函数 | 特定映射 |
| `Schema.decodeUnknown` | `json.Unmarshal` + 构造器校验 | 不信任的外部队列 |

---

## 五、泛型

### 问题

TS 泛型灵活（高阶泛型、条件类型、映射类型），Go 泛型受限。Go 1.27 新增了 **Generic Methods**（方法级别类型参数），但**泛型接口方法**仍然不支持。

### 能力边界

| 能力 | Go 1.26 | Go 1.27+ |
|---|---|---|
| 泛型函数 | ✅ | ✅ |
| 泛型类型 | ✅ | ✅ |
| 泛型方法（方法使用类型的参数） | ✅ `func (List[E]) Len() int` | ✅ |
| 泛型方法（方法有自己的参数） | ❌ | ✅ `func (List[E]) Map[R any](f func(E) R) List[R]` |
| 泛型接口方法 | ❌ | ❌ `type I interface { M[P any]() }` — 不支持 |
| 异构泛型集合 | ❌ `[]Def[I,O,S]` | ❌ 不同 P,R 仍是不同类型 |

### 决策树

```
TS: function fn<T>(x: T): T
│
├─ 类型参数在公开接口中（跨包传递）
│  → 擦除为 interface（泛型接口方法仍不支持）
│     type Route interface {
│       Stream(ctx, req) iter.Seq2[LLMEvent, error]
│     }
│     具体实现用泛型 struct 在包内（非导出）
│
├─ 类型参数只在内部实现，方法需要额外参数
│  → 用泛型方法（Go 1.27+）
│     type protocolImpl[Body, State any] struct { }
│     func (p *protocolImpl[B, S]) Step[F any](state S, frame F) (S, []LLMEvent, error)
│
├─ 类型参数只在内部实现，方法不需要额外参数
│  → 泛型 struct 的方法直接用类型参数
│     type routeImpl[Body, Prepared any] struct { }
│     func (r *routeImpl[B, P]) Stream(ctx, req) iter.Seq2[LLMEvent, error]
│
├─ 纯数据处理（集合、选项）
│  → 泛型方法或泛型函数
│     func (list List[E]) Map[R any](f func(E) R) List[R]   // 方法
│     func Map[T, R any](items []T, fn func(T) R) []R       // 函数（slice 不支持方法）
│
└─ 需要条件类型 / 映射类型等高级泛型
    → 拆为多个具体函数，或用接口 + 类型断言
       TS: type Payload<T> = T extends "chat" ? ChatBody : RealtimeBody
       Go: bodyForChat() / bodyForRealtime() 分开
```

### 对照表

| TS 泛型场景 | Go 方案 | 条件 |
|---|---|---|
| `Route<Body, Prepared>` | 内部 `impl[B,P]`，外部 `Route` 接口 | 跨包边界 |
| `Protocol<Body, Frame, State>` | `impl[B,S]` + `Step[F]` 泛型方法 | 方法级参数 |
| `Tool<Params, Success>` | `Def[I,O,S]`（经 `Make(Config[I,O,S]{...})` 构造）+ 外部 `Tool` 接口 | 注册中心异构集合 |
| `Promise<T>` / `Effect<T>` | 不用泛型，返回 `(T, error)` | 无需擦除 |
| 集合操作 | 泛型方法 or 泛型函数 | 方法用于自定义类型，函数用于 slice |
| functional options | `type Option[T any] func(*T)` | 内部工具 |

> **Go 1.24+** 泛型类型别名：`type RouteLike[B,P] = routeImpl[B,P]` 可为复杂泛型类型起简短别名。
> **Go 1.27+** 泛型方法：本文中 `Protocol` 的 `Step[F]` 方法利用了此特性。

---

## 六、any / unknown / 弱类型

### 问题

opencode 中有不少 `unknown` 变量，经 Schema 验证后收敛为已知类型。Go 没有这种运行时收窄链条。

### 决策树

```
TS: const x: unknown = payload
     const valid = Schema.decode(Schema.T, x)
│
├─ 变量在公开 API 边界（导出符号）
│  → 永远不用 any，必须定义为具体类型
│     例: // ❌ Bad: func Execute(params any) any
│         // ✅ Good: func Execute(params *ReadParams) *ReadResult
│
├─ 变量在 provider 协议解码边界
│  → 定义临时原生 struct，边界处转为规范类型
│     例: // llm/protocols/openai_chat.go
│         type rawChoice struct { Delta *rawDelta `json:"delta"` }
│         func toLLMEvent(raw rawChoice) LLMEvent
│
├─ 变量在 MCP / 插件工具边界（外部 JSON Schema）
│  → map[string]any，用构造器拆包
│     例: func readParamsFromMap(m map[string]any) (*ReadParams, error)
│
└─ 变量在事件分发边界（SessionEvent）
    → json.RawMessage + 各自 Unmarshal
```

### 原则

- 导出 API：零容忍 `any` / `interface{}`，强制具体类型
- 包内实现：协议边界可临时用，但必须在边界处转为具体类型后传递
- 唯一合法的 `any` 使用场景：MCP/插件工具的 `map[string]any` 入参，且立即在构造器中拆包

---

## 七、系统上下文组装 (SystemContext)

### 问题

opencode 的 `packages/core/src/system-context/index.ts` 定义了可注册、可组合、可 diff 的上下文源（上游 git 状态、当前文件、agent prompt、时间等），在每轮 agent 循环前组装为 system prompt。每个 `Source<A>` 有：

```typescript
interface Source<A> {
  key: Key
  codec: Schema.Codec<A>           // 运行时编解码
  load: Effect<A | Unavailable>    // 获取当前值
  baseline: (current: A) => string  // 生成初始文本
  compare: (prev: A, cur: A) => string  // 生成 diff 文本
}
```

多个 Source 通过 `combine()` 合并成一个 `SystemContext`，送给 LLM。

### 决策树

```
Source<A> + codec(运行时编解码)
│
├─ 上下文源是静态文本（prompt、规则）
│  → 直接返回 string，无 codec/编解码
│     type StaticSource struct { Key, Text string }
│
├─ 上下文源是动态计算值（git diff、文件列表）
│  → struct 接口 + 函数
│     type ContextSource interface {
│       Key() string
│       Load(ctx) (string, error)         // 当前值 → 纯文本
│       Compare(ctx, prev string) (string, bool, error)
│     }
│     type GitDiffSource struct { Dir string }
│     func (s GitDiffSource) Load(ctx) (string, error) { /* git diff */ }
│     func (s GitDiffSource) Compare(ctx, prev string) (string, bool, error) {
│       // 与上次值 diff
│     }
│
└─ 上下文源需要比对前后变化（增量更新）
    → Compare 返回 (content string, changed bool, err)。
       changed=true 时才追加到 system prompt 的 "diff" 段落。
```

### 对照表

| TS 概念 | Go 方案 | 说明 |
|---|---|---|
| `Source<A>` | `ContextSource` 接口 | 接口方法返回 string，无泛型 |
| `codec: Schema.Codec<A>` | 无需对应 | Source 直接返回渲染好的文本，不在运行时重新编解码 |
| `baseline(current)` | `Load() string` | 返回当前值的纯文本 |
| `compare(previous, current)` | `Compare(prev string) (string, bool)` | 返回 diff 文本 + 是否变更 |
| `combine(sources)` | `func Combine(sources []ContextSource) string` | strings.Builder 按 key 顺序拼接 |
| `Unavailable` | `Load() (string, error)` — error 表示不可用 | 不可用的 source 跳过 |
| 注册机制 | `ContextRegistry map[string]ContextSource` | 构造时注册，运行时按 key 查询 |

---

## 八、工具定义与执行运行时

### 问题

opencode 中 `Tool.make()` 返回一个普通对象，但运行时信息通过 `WeakMap<AnyTool, Runtime>` 附加。这是一种 Effect 环境隔离技巧——工具定义与执行逻辑松耦合。

```typescript
type Runtime = {
  definition: (name: string) => ToolDefinition   // JSON Schema 定义
  settle: (call: ToolCall, ctx: Context) => Effect<ToolOutput, ToolFailure>
}
const runtimes = new WeakMap<AnyTool, Runtime>()
```

### 决策树

```
Tool.make({ description, input, output, execute })
│
├─ 内置工具（read, write, shell 等，参数类型已知）
│  → 用 `Make(Config[I,O,S]{...})` 定义 `*Def[I,O,S]`，注册到 Registry
│     writeTool := Make(Config[WriteInput, WriteOutput, WriteOutput]{
│       Description: "...", Input: writeInputSchema,
│       Execute: func(input WriteInput, ctx Context) (WriteOutput, error) { ... },
│     })
│     registry.Register(map[string]Tool{"write": writeTool})
│
├─ MCP 工具（参数类型未知，外部 JSON Schema）
│  → 直接实现 `Tool` 接口，参数用 `map[string]any`
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
└─ 插件工具（参数 Plugin 定义）
    → 同 MCP 工具，map[string]any + 构造器拆包
```

### WeakMap 替代

TS 的 `WeakMap<AnyTool, Runtime>` 在 Go 里直接内联：`Make` 返回一个不透明句柄 `*Def[Input, Output, Structured]`，其内部持有 `runtime`（definition + settle）；注册中心通过 `Tool` 接口（含未导出方法）做类型擦除。

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

不需要 WeakMap：`Def` 内部直接持有 runtime；`*Def[I,O,S]` 通过 `Tool` 接口擦除后注册到 `Registry`。`WithPermission` 返回共享 runtime 的新 `Def`（只覆写 permission）。

> **Go 1.24+ `weak` 包**：如果后续需要缓存工具实例的运行时元信息（类似 opencode 的 WeakMap），可使用标准库 `weak` 包的弱引用替代。`weak.Make(ptr)` 创建弱指针，GC 不阻止回收。

### 文件系统安全

`ReadTool`、`WriteTool`、`GlobTool` 等需要访问文件系统的工具，使用 `os.Root` 限制在会话的工作目录内，防止路径遍历攻击：

```go
func safeOpen(root *os.Root, path string) (*os.File, error) {
    // os.Root 自动阻止路径逃逸（如 ../../etc/passwd）
    return root.Open(path)
}
```

### 对照表

| TS 模式 | Go 方案 | 理由 |
|---|---|---|
| `WeakMap<AnyTool, Runtime>` | `Def[I,O,S]` 内联 runtime + `Tool` 接口擦除 | 无需 WeakMap |
| `Schema.Codec` 校验入参 | `santhosh-tekuri/jsonschema/v6` 校验 + `json.Unmarshal` | 校验 schema 由 `jsonschema.Schema.Validation()` 派生 |
| `Schema.encode` 编码出参 | 直接 `json.Marshal` | 性能更好，代码更简单 |
| `input: Schema.Type<Input>` | `Config.Input: *jsonschema.Schema` | 类型化 AST；`Model()` 发给模型，`Validation()` 用于校验 |
| `withPermission(tool, perm)` | `WithPermission(tool, perm)` 返回新 `Def` | 共享 runtime，覆写 permission |

---

## 九、会话消息状态机

### 问题

opencode 中的 `SessionV1.Part` 有类型变体（text / tool / file / reasoning / step-start / step-finish / agent），且 `ToolPart` 内部有状态机：

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

### 决策树

```
TS: type Part = TextPart | ToolPart | FilePart | ReasoningPart | StepStart | ...
│
├─ Part 作为会话持久化的事件（JSON 存储，跨进程）
│  → json.RawMessage + 延迟分发（同策略二）
│     type Part struct {
│       Type string          `json:"type"`
│       Data json.RawMessage `json:"data,omitempty"`
│     }
│
├─ Part 在内存中处理（消息列表渲染、上下文集装）
│  → Tagged struct + 构造器（同策略一）
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
└─ 工具状态机 (pending → running → completed/error)
    → 带状态的 struct，方法驱动状态转换
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

### 对照表

| TS 模式 | Go 方案 | 条件 |
|---|---|---|
| `Part = TextPart \| ToolPart \| ...` 内存 | Tagged Struct | 高频操作，需快速 type switch |
| `Part = TextPart \| ToolPart \| ...` JSON 持久化 | json.RawMessage + 分发 | 跨进程、事件溯源 |
| `ToolPart.state = pending \| running \| ...` | struct + State + 方法 | 单一类型内的状态迁移 |
| `type {status: "completed"; output: string}` | `ToolPart{State: Completed, Output: "..."}` | tagged struct 统一处理 |

---

## 十、Agent 循环并发工具执行

### 问题

opencode 的 SessionRunner 在流式收到 `tool-call` 事件时立即 dispatch（eager execution）。多个工具可并发执行（FiberSet），全部完成后发起 continuation。

```typescript
// 伪代码
stream: for await (event of llm.stream(request)) {
  if (event.type === "tool-call") {
    executes.add(fiber(event))   // 立即并发执行
  }
}
await FiberSet.join(executes)   // 等待全部完成
// 重新加载历史，继续下一轮
```

### 决策树

```
LLM 流式返回 tool-call 事件
│
├─ tool-call 数量少（1-3 个）
│  → goroutine + errgroup
│     eg, ctx := errgroup.WithContext(ctx)
│     for _, call := range toolCalls {
│       call := call
│       eg.Go(func() error { return executeTool(ctx, call) })
│     }
│     if err := eg.Wait(); err != nil { ... }
│     // 全部完成，继续下一轮
│
├─ tool-call 数量多（10+），需要限制并发
│  → worker pool + channel
│     calls := make(chan ToolCall, len)
│     for i := 0; i < poolSize; i++ {
│       eg.Go(func() error { for c := range calls { executeTool(c) }; return nil })
│     }
│     for _, c := range toolCalls { calls <- c }
│     close(calls)
│     eg.Wait()
│
└─ 需要在工具执行期间继续处理 LLM 流事件
    → 两个 goroutine：一个收流式事件，一个执行工具
       // goroutine 1: 流式事件
       go func() {
         for event := range stream { handleStreamEvent(event) }
       }()
       // goroutine 2: 工具执行
       eg.Go(func() error { for call := range toolCalls { execute(call) }; return nil })
```

### FiberSet 替代

```go
// opencode 中 FiberSet 是一个可追加的并发集合，全部 join 后返回
// Go 中用 errgroup.Group 替代：
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

## 十一、MCP JSON-RPC 协议

### 问题

opencode 的 MCP 使用 JSON-RPC 2.0 over HTTP/SSE，用 Schema 定义请求/响应结构。

```typescript
const McpRequest = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Literal(1),
  method: Schema.Literal("tools/call"),
  params: Schema.Struct({ name: Schema.String, arguments: args }),
})
```

### 决策树

```
JSON-RPC 2.0 协议
│
├─ 请求构造
│  → 普通 struct + json.Marshal
│     type RPCRequest struct {
│       JSONRPC string `json:"jsonrpc"`
│       ID      int    `json:"id"`
│       Method  string `json:"method"`
│       Params  any    `json:"params"`
│     }
│
├─ 响应解析
│  → struct + json.Unmarshal
│     type RPCResponse struct {
│       JSONRPC string          `json:"jsonrpc"`
│       ID      int             `json:"id"`
│       Result  json.RawMessage `json:"result,omitempty"`
│       Error   *RPCError       `json:"error,omitempty"`
│     }
│
├─ 传输层
│  ├─ stdio 子进程 → os/exec.Cmd + bufio
│  ├─ HTTP/SSE  → net/http + bufio.Scanner 按行解析 `data:` 前缀
│  └─ 统一接口
│     type MCPTransport interface {
│       Send(ctx context.Context, msg []byte) error
│       Recv(ctx context.Context) ([]byte, error)
│       Close() error
│     }
│
└─ 工具适配
    → MCP 的 tool 定义 → Tool 接口
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

### 对照表

| TS 模式 | Go 方案 | 说明 |
|---|---|---|
| `Schema.Struct({jsonrpc: "2.0"...})` | 普通 Go struct + json tag | 无 Schema 运行时开销 |
| `Schema.decodeUnknown(body)` | `json.Unmarshal(body, &rpcResp)` | 标准库直接反序列化 |
| `HttpClient.filterStatusOk(http)` | `resp.StatusCode >= 200 && < 300` | 直接判断 |
| `HttpClientRequest.schemaBodyJson(...)` | `json.Marshal(req)` + `bytes.NewReader` | 显式编码 |
| Effect Timeout | `context.WithTimeout` | 标准库原生 |

---

## 十二、Config 加载：用户自定义 Agent

### 问题

opencode 的 agent 与 skill 定义来自 `{agent,agents}/**/*.md`、`{mode,modes}/*.md` 与 `{skill,skills}/**/SKILL.md`，格式为 YAML frontmatter + markdown body。

### 策略

**只支持 markdown**（frontmatter + body），格式与 TS 完全一致，不引入 YAML 双格式：

- 解析器 `core/util/markdown.go`（`ParseMarkdownOption`）逐行移植 TS 的 `ConfigMarkdown.parse`（含 `SanitizeMarkdown` 重试），不依赖第三方 frontmatter 库。
- frontmatter 反序列化到 `defs/config.AgentInfo`（json tag），body 作为 `system`（mode 文件额外置 `mode: primary`）。
- **不做目录发现**：由 `ClientOptions.ConfigDirs` 显式给出目录，SDK 只扫描这些目录（见 DESIGN.md §11.1）。
- legacy v1 agent markdown（旧字段名 → `V1MigrateAgent`）不移植，`core/v1/config` 已删除。

### 对照表

| opencode 做法 | Go 做法 |
|---|---|
| `Schema.decodeUnknownExit(Schema)(config)` | `json.Unmarshal(frontmatter, &AgentInfo)` |
| `ConfigMarkdown.parse`（gray-matter） | `util.ParseMarkdownOption`（逐行移植，含 sanitize 重试） |
| `{agent,agents}/**/*.md` | 同（`core.Glob` + `doublestar`） |
| markdown body → system prompt | `AgentInfo.System` |
| 目录发现（`.opencode` 向上查找 + env + 全局目录） | `ClientOptions.ConfigDirs` 显式目录 |

---

## 十三、总结：一句话决策

| TS 模式 | 一句话决策 |
|---|---|
| `Schema.optional(field)` | JSON 边界用 `*T`，内部有默认值用值类型+构造器 |
| `type E = A \| B \| C` | 变体少且差异大 → marker interface；变体多且重叠 → tagged struct；跨模块持久化 → json.RawMessage |
| `Effect.gen` / `Effect<T,E>` | 直接写 `(T, error)`，依赖显式传参 |
| `Stream<T,E>` | 用 `iter.Seq2[T, error]` |
| `Context.Tag` + `Layer` | 构造器注入，不搞 DI 框架 |
| `Schema.Struct` | Go struct + json tag |
| `Route<Body,Prepared>` | 包内泛型 struct + 导出接口擦除（异构集合需要） |
| `Protocol<Body,Frame,State,Event>` | `impl[B,S]` + `Step[F]` 泛型方法（Go 1.27+） |
| `Tool<Params,Success>` | `Def[I,O,S]` 泛型定义 + `Tool` 接口擦除注册 |
| `unknown` | 导出 API 零容忍；包内边界处转具体类型 |
| `Source<A>` (SystemContext) | `ContextSource` 接口返回文本，无 Codec |
| `WeakMap<AnyTool, Runtime>` | `Def` 内联 runtime 字段 |
| `Part = TextPart \| ToolPart \| ...` | 内存 → tagged struct；持久化 → json.RawMessage |
| `FiberSet.join(executes)` | `errgroup.Group` + goroutine |
| `McpRequest({jsonrpc:"2.0"...})` | 普通 struct + `json.Marshal` |
| `{agent,agents}/**/*.md` frontmatter | `*.md` + `*.yaml` 双支持 |

## 十四、符号对照表（Go ↔ TS）

移植复核时用来定位 TS 源。文件名本身遵循 AGENTS.md 的规则
（`packages/<pkg>/src/<path>/<name>.ts` → `<pkg>/<path>/<name>.go`，kebab-case → snake_case）；
下表只列**规则推不出来**的：TS 源文件与 Go 文件不同名时给出实际路径，符号名与 Go 名不同时给出对照。

| Go | TS 源 | 非同名的符号 |
|---|---|---|
| `agent/task.go` | packages/opencode/src/tool/task.txt | `AgentPermissions` ↔ `registry.describeTask` |
| `core/agent.go` | packages/core/src/agent.ts | `AgentDefaultID` ↔ `AgentV2.defaultID` |
| `core/agent_defaults.go` | packages/core/src/plugin/agent.ts | — |
| `core/filesystem.go` | packages/core/src/filesystem.ts, packages/schema/src/filesystem.ts | `FileSystemEntry` ↔ `FileSystem.Entry`, `FileSystemSubmatch` ↔ `FileSystem.Submatch`, `FileSystemMatch` ↔ `FileSystem.Match` |
| `core/fs_util.go` | packages/core/src/fs-util.ts | `RemoveAll` ↔ `fs.remove` |
| `core/image.go` | packages/core/src/image.ts | `Image.Normalize` ↔ `Image.normalize`；Go 侧 resizer 是编译期依赖，`Image.ResizerUnavailableError` 不移植 |
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
