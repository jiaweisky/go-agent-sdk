# go-agent-sdk 设计文档

> ⚠️ **本文档中的 Go 代码仅为示意**，用于表达架构设计和模式，不代表最终实现。每个函数签名、类型定义、错误处理的细节都以 opencode 的 TypeScript 源码为准。移植时请对照 opencode 对应文件的完整逻辑。

## 1. 项目概述

go-agent-sdk 是 [opencode](https://opencode.ai) 的 Go 移植版，目标是提供一套完整可嵌入的 Agent SDK。核心能力包括：

- **LLM 协议层** — 支持 OpenAI Chat / OpenAI Responses / Anthropic Messages 三种协议，
  支持多 provider 路由（OpenAI、Anthropic、OpenRouter、Google Gemini、AWS Bedrock 等）。
- **Agent 循环** — 自动化的 tool-calling agent 循环，支持步骤管理、上下文溢出恢复、steer/queue 输入。
- **Agent 定义** — 可配置的 agent 元信息、system prompt、权限规则、模型覆盖。
- **Skill** — 从 markdown 文件加载专业知识，注入系统上下文。
- **MCP 集成** — 原生 Model Context Protocol 客户端，支持本地和远程 MCP 服务器。
- **内置工具** — read, write, edit, apply_patch, bash, glob, grep, webfetch, websearch, question, skill, todowrite（12 个，见 §7.4）。
- **权限系统** — 基于规则的 allow/deny/ask 权限模型。
- **Subagent** — agent 内通过 task tool 递归创建独立子会话。

---

## 2. MVP 范围

基于上述设计，第一版（MVP）实现以下核心闭环，支持端到端的 agent 对话：

### 包含

| 模块 | 内容 | 说明 |
|---|---|---|
| `llm/schema/` | LLMRequest, LLMEvent, LLMClient 接口, Model, options, ids, errors | 规范类型定义 |
| `llm/route/` | Route + Protocol + Transport + Framing + Auth + Endpoint | LLM 协议层核心 |
| `llm/protocols/` | OpenAI Chat / Responses、Anthropic Messages、Bedrock Converse、Gemini | 各 provider 线协议 |
| `llm/provider/` | Provider Facades（openai, anthropic, openrouter, ...） | 预配置默认值 |
| `agent/`（已实现） | 高层公开 API（Client/Session/Send/On/DefineTool/Hooks） | 组装 core/session + llm + tool |
| `core/tool/` | ToolRegistry + `registry.go` / `tool.go` | 注册、查询、权限过滤 |
| 内置工具 | 12 个（`core/tool/`，见 §7.4） | read/write/edit/apply_patch/bash/glob/grep/webfetch/websearch/question/skill/todowrite |
| `core/permission.go` | 规则引擎（allow / deny / ask） | 权限评估 |
| `core/` | file-mutation / location-mutation / git / database / process ... | 支撑模块 |
| `agent/` | `NewClient` / `CreateSession` / `Send` / `On` / `RegisterAgent` | 公开 API（已实现，见 §11） |
| `schema/` | 基础类型 | JSON Schema、ID 生成 |

### 不包含（推迟到后续版本）

| 模块 | 推迟原因 |
|---|---|
| MCP 集成 | 独立于 agent 循环，可后续增量加入 |
| Skill 系统 | 不影响核心 agent 能力 |
| Config frontmatter 加载 | MVP 无需用户自定义 agent 文件 |
| task/subagent、lsp 工具 | 非核心路径，后续追加 |
| 上下文压缩（compaction） | MVP 假设上下文窗口够用 |
| Steer/Queue 输入 | 第一版仅支持同步 `Send` |
| `weak` 包、`os.Root` 等安全特性 | 后续加固 |

### MVP 用户代码示例

```go
c := agent.New(agent.Config{
    Provider: agent.ProviderConfig{
        Type:    "openai",
        BaseURL: "https://api.openai.com/v1",
        APIKey:  os.Getenv("OPENAI_API_KEY"),
    },
})

c.RegisterAgent("dev", agent.AgentConfig{
    Model:       "gpt-4o",
    Tools:       []Tool{ReadTool, WriteTool, EditTool, ShellTool},
    SystemPrompt: "You are a helpful assistant.",
})

session := c.NewSession(ctx, "dev")

for event := range session.Send(ctx, "Read main.go, fix the bug, and verify") {
    switch e := event.(type) {
    case TextDelta:    os.Stdout.WriteString(e.Text)
    case ToolCall:     log.Printf("calling %s", e.Name)
    }
}
```

---

## 3. 架构总览

Go 目录直接镜像 opencode 的 TS monorepo 包结构（详见 §4）。依赖方向：

```
┌────────────────────────────────────────────────────────────────┐
│  agent/             ← 高层公开 API（已实现）                     │
│  High-level API (Agent, Session, Prompt, Stream)               │
└───────────────────────────┬────────────────────────────────────┘
                            │ depends on
┌───────────────────────────v────────────────────────────────────┐
│  Agent 循环 (loop / turn / session / context / steer / compact) │
└──────┬──────────────┬──────────────┬──────────────┬────────────┘
       │              │              │              │
┌──────v───────┐ ┌────v─────┐ ┌──────v─────┐ ┌──────v───────────┐
│ core/tool/   │ │ mcp/     │ │ skill/     │ │ core/permission  │
│ Registry     │ │ Client   │ │ Loader     │ │ Ruleset / assert │
│ write, ...   │ │ (未移植) │ │ (未移植)   │ │                  │
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
│  │ llm/route/transport/   HTTP │ SSE │ WebSocket             │ │
│  └───────────────────────────────────────────────────────────┘ │
│                                                                 │
│  llm/schema/    LLMRequest / LLMEvent / LLMError / ID          │
└─────────────────────────────────────────────────────────────────┘
       │
┌──────v─────────────────────────────────────────────────────────┐
│  schema/        ID 生成、Location / Project / Permission ...    │
└─────────────────────────────────────────────────────────────────┘
```

### 依赖方向

```
agent ──> core/tool ──┐
          mcp ────────┤
          skill ──────┼──> llm ──> llm/schema ──> schema
          core/permission ─┘
```

- `core/` 提供工具框架（tool / permission / file-mutation / location-mutation）与基础设施（git / database / process / fs-util）
- `llm/` 提供协议层；`llm/schema` 是 LLM 规范类型
- `schema/` 是跨领域共享类型（ID、Location、Project、Permission）
- `mcp/` 是纯 JSON-RPC 协议实现（stdio/SSE），不涉及 LLM 概念；`core/tool/` 的适配层将 MCP Tool 转为 LLM 可消费的 ToolDefinition

## 4. 包结构

Go 目录**直接镜像 opencode 的 TS monorepo 包结构**：`packages/<pkg>/src/<path>` → `<pkg>/<path>`（kebab-case → snake_case），不使用 `pkg/` 或 `internal/`。完整约定（符号前缀、循环依赖处理、index.ts 规则）见 `AGENTS.md`。

```
go-agent-sdk/                            opencode TS
├── llm/            ← packages/llm/src/          (@opencode-ai/llm)
│   ├── llm.go                       包入口、LLM re-exports (llm.ts)
│   ├── tool.go                      LLM 层 Tool 定义 (tool.ts)
│   ├── tool_runtime.go              工具运行时编排 (tool-runtime.ts)
│   ├── cache_policy.go              缓存策略 (cache-policy.ts)
│   ├── provider.go                  Provider 类型定义 (provider.ts)
│   ├── provider_error.go            provider-error.ts
│   ├── schema/                      LLM 规范类型 (schema/)
│   │   ├── ids.go                   ProviderID, ModelID, ProtocolID, FinishReason, MessageRole
│   │   ├── events.go                LLMEvent (events.ts)
│   │   ├── messages.go              LLMRequest, LLMResponse, Message (messages.ts)
│   │   ├── options.go               GenerationOptions (options.ts)
│   │   └── errors.go                LLMError + ProviderError (errors.ts)
│   ├── route/                       Route 层 (route/)
│   │   ├── client.go                Route + LLMClient (route/client.ts)
│   │   ├── transport.go             Transport / TransportRuntime / TransportPrepareInput
│   │   ├── protocol.go              Protocol (route/protocol.ts)
│   │   ├── endpoint.go              Endpoint (route/endpoint.ts)
│   │   ├── auth.go                  Auth (route/auth.ts)
│   │   ├── auth_options.go          AuthOptions (route/auth-options.ts)
│   │   ├── framing.go               Framing SSE/JSONL (route/framing.ts)
│   │   ├── executor.go              RequestExecutor (route/executor.ts)
│   │   └── transport/               HttpTransport / WebSocket (route/transport/)
│   ├── protocols/                   各 provider 线协议（平铺，符号加协议前缀）
│   │   ├── openai_chat.go           protocols/openai-chat.ts
│   │   ├── openai_responses.go      protocols/openai-responses.ts
│   │   ├── anthropic_messages.go    protocols/anthropic-messages.ts
│   │   ├── bedrock_converse.go      protocols/bedrock-converse.ts
│   │   ├── bedrock_event_stream.go  protocols/bedrock-event-stream.ts
│   │   ├── gemini.go                protocols/gemini.ts
│   │   ├── shared.go                protocols/shared.ts
│   │   └── utils/                   protocols/utils/
│   ├── provider/                    Provider Facades (providers/)
│   └── utils/                       通用工具 (utils/record.ts)
├── core/           ← packages/core/src/          (@opencode-ai/core)
│   ├── schema.go                    re-exports (schema.ts)
│   ├── config.go                    Config.Info / Document / Directory / entries (config.ts)
│   ├── global.go                    Global (global.ts)
│   ├── policy.go                    Policy allow/deny statements (policy.ts)
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
│   ├── config/                      config/ sub-schemas (config/agent.ts, ...)
│   ├── v1/config/                   legacy ConfigV1 + migrate (v1/config/)
│   ├── database/                    SQLite + migrations (database/)
│   ├── effect/                      Effect 运行时工具 (effect/keyed-mutex.ts, ...)
│   ├── project/                     project/schema.ts, project/directories.ts
│   ├── tool/                        工具框架 (tool/)
│   │   ├── tool.go                  tool.ts
│   │   ├── registry.go              registry.ts
│   │   ├── read.go / read_filesystem.go  read.ts / read-filesystem.ts
│   │   ├── write.go                 write.ts
│   │   ├── edit.go                  edit.ts
│   │   └── bash.go                  bash.ts
│   └── util/                        通用工具 (util/wildcard.ts, util/hash.ts, util/jsonc.go)
├── opencode/       ← packages/opencode/src/      (opencode CLI)
│   ├── id/                          带前缀的单调 ID 生成 (id/id.ts)
│   └── tool/                        opencode 工具层
│       ├── schema.go                tool/schema.ts
│       └── json_schema.go           tool/json-schema.ts
└── schema/         ← packages/schema/src/        (@opencode-ai/schema)
    ├── schema.go                    schema.ts
    ├── identifier.go                identifier.ts
    ├── event.go                     event.ts
    ├── location.go                  location.ts
    ├── project.go                   project.ts
    ├── project_id.go                project-id.ts
    ├── permission.go                permission.ts
    ├── permission_saved.go          permission-saved.ts
    ├── provider.go                  provider.ts
    ├── model.go                     model.ts
    ├── policy.go                    (Policy.Info shared schema)
    ├── session_id.go                session-id.ts
    └── workspace_id.go              workspace-id.ts
```

> 已实现：高层 `agent/` 公开 API、core/session 主循环、12 个内置工具、Skill、MCP、Plugin（窄化）、Reference 服务。尚未移植：task/lsp 工具。

---

## 5. LLM 层详细设计

### 5.1 Route (路由)——核心抽象

Route 表示一个具体的 API 部署：1 个 URL + 1 个认证方式 + 1 个线协议 + 1 个流帧处理。

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
    StreamPrepared   func(prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
}

// 构建请求体：LLMRequest → Provider-native body
type RouteBody struct {
    Schema schema.JsonSchema
    From   func(request *schema.LLMRequest) (any, error)
}
```

Route 由四个正交层组合而成：

```
Route = Protocol + Endpoint + Auth + Framing
```

```go
// 从已就绪的 Transport 构造 Route（protocols/*.go 使用）
func MakeFromTransport(input MakeTransportInput) Route
```

> TS 的 `Route<Body, Prepared>` 泛型在 Go 里用 `any` 擦除（异构集合需要）。

### 5.2 Protocol (协议)

Protocol 封装一个 provider 原生 API 的语义：body 构建、body schema、流式事件状态机。

```go
// llm/route/protocol.go
type Protocol struct {
    ID     schema.ProtocolID
    Body   ProtocolBody
    Stream ProtocolStream
}

type ProtocolBody struct {
    Schema schema.JsonSchema
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

### 5.3 LLMEvent (流式事件)

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

```go
// llm/provider/openai.go —— 传入预配置的 route（见 llm/protocols/）
openai := provider.NewOpenAI(responsesRoute, responsesWebSocketRoute, chatRoute, provider.OpenAIConfig{
    APIKey: &apiKey,
})
model := openai.Model("gpt-4o")       // 默认协议
chat  := openai.Chat("gpt-4o")        // 绑定 Chat 协议
resp  := openai.Responses("gpt-4o")   // 绑定 Responses 协议

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

### 5.6 协议实现

| 协议 | 位置 | Provider |
|------|------|----------|
| OpenAI Chat | `llm/protocols/openai_chat.go` | OpenAI、DeepSeek、TogetherAI、Cerebras |
| OpenAI Responses | `llm/protocols/openai_responses.go` | OpenAI |
| Anthropic Messages | `llm/protocols/anthropic_messages.go` | Anthropic (Claude) |
| Bedrock Converse | `llm/protocols/bedrock_converse.go` | AWS Bedrock |
| Google Gemini | `llm/protocols/gemini.go` | Google |
| OpenAI Compatible Chat | `llm/protocols/openai_compatible_chat.go` | 通用 OpenAI 兼容 |

---

## 6. Agent 循环

> **尚未移植。** 以下为设计目标，对应 opencode `packages/core/src/agent*`、`packages/opencode/src/session/`。

### 6.1 Session Runner 主循环

```
SessionRunner.Run(sessionID, force)
  → 循环直到没有待处理输入:
      runTurn(sessionID, input)
        → 循环直到不需要 continuation:
            1. 加载 session、agent、model、系统上下文
            2. 组装 LLMRequest
            3. 检查上下文压缩
            4. LLMClient.Stream(request) — 一次 provider 调用
            5. 对每个 tool-call 事件:
                - 持久化记录 tool call
                - 权限检查
                - ToolRegistry.Execute(tool, params, ctx)
                - 发布 tool-result 事件
            6. 跟踪 step 数，超限则终止
            7. 有工具调用 → 重载历史 → 再次 provider 调用 (continuation)
            8. 上下文溢出 → 压缩后重试
            9. Steer 事件 → 中断当前轮
      → 检查队列输入 → 继续外层循环
```

### 6.2 Agent 定义

```go
type Agent struct {
    Name        string
    Description string
    Mode        AgentMode       // Primary | SubAgent | All
    Hidden      bool
    Permission  permission.Ruleset
    Model       *ModelOverride  // providerID + modelID
    Prompt      string          // system prompt override
    Steps       int             // max turn steps (default 100)
    Temperature float64
    TopP        float64
}
```

### 6.3 Subagent (TaskTool)

```
task tool 被调用
  → 创建子 Session (独立 event store)
  → 继承父 agent 的 deny 规则 + external_directory
  → 子 agent 的 toxonomy 决定自身能力
  → 深度限制 (默认 1)
  → 后台模式: 异步执行，完成时通知
```

---

## 7. 工具系统

### 7.1 Tool 定义

```go
// core/tool/tool.go
type Context struct {
    SessionID          string
    Agent              string
    AssistantMessageID string
    ToolCallID         string
}

// 泛型工具定义（Input / Output / Structured 为 Go 类型）
type Def[Input, Output, Structured any] struct { /* runtime */ }

// 类型擦除后的工具句柄，注册中心使用
type Tool interface { /* unexported */ }

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

> 对应 TS `core/src/tool/tool.ts` 的 `Tool.make()` / `Tool.Definition` / `Tool.Context`。TS 的 `WeakMap<AnyTool, Runtime>` 在 Go 里直接内联为 `Def.rt`。
>
> 工具 schema 使用类型化的 `jsonschema.Schema`（对标 Effect 的 `Schema` AST），单一来源产出两个投影：`Model()` 是发给模型的 JSON Schema（对象闭合 `additionalProperties:false`、optional 仅不进 `required`、literal → typed enum、int check → `allOf`），`Validation()` 用于校验（对象不闭合、容忍多余字段）。两者只在「对象是否闭合」上不同，且**都不广告解码器会拒绝的值**（optional 不接受 `null`、number 不接受 `"NaN"` 等字符串——Effect 会广告但它们解码器实际拒绝）。外部 JSON Schema 用 `jsonschema.Raw` 包一层。注意：Effect 会把带 `identifier` 的 schema 提升到 `$defs` 并用 `$ref` 引用，Go 侧**内联**（LLM 层不解析 `$ref`，内联对 provider 更安全）。

### 7.2 内置工具定义

```go
// core/tool/write.go
func MakeWrite(deps WriteDeps) *Def[WriteInput, WriteOutput, WriteOutput] {
    return Make(Config[WriteInput, WriteOutput, WriteOutput]{
        Description: "...",
        Input:       WriteInputSchema,
        Output:      WriteOutputSchema,
        Execute: func(input WriteInput, ctx Context) (WriteOutput, error) { /* ... */ },
        ToModelOutput: func(input WriteInput, output any) []Content { /* ... */ },
    })
}
```

### 7.3 注册中心

```go
// core/tool/registry.go
type Registry struct { /* ... */ }

func NewRegistry(opts ...RegistryOption) *Registry

// 注册工具，返回 scope 结束时的清理函数
func (r *Registry) Register(tools map[string]Tool) (cleanup func(), err error)

// 合并 application + local 注册，按权限过滤，产出 definitions + settle
func (r *Registry) Materialize(permissions core.PermissionRuleset) *Materialization

// 内置工具便捷注册
func RegisterWrite(registry *Registry, deps WriteDeps) (func(), error)
```

> MCP 已实现（见 §8，`mcp/` 包）。Plugin 动态工具（外部 JSON Schema）尚未移植；届时需为 `Tool` 接口提供可外部实现的适配路径。

### 7.4 内置工具

| 工具 | ID | 状态 |
|------|----|------|
| Write | `write` | ✅ 已移植（`core/tool/write.go`） |
| Read | `read` | ✅ 已移植（`core/tool/read.go`） |
| Edit | `edit` | ✅ 已移植（`core/tool/edit.go`） |
| Bash | `bash` | ✅ 已移植（`core/tool/bash.go`） |
| Glob | `glob` | ✅ 已移植（`core/tool/glob.go`） |
| Grep | `grep` | ✅ 已移植（`core/tool/grep.go`） |
| WebFetch | `webfetch` | ✅ 已移植（`core/tool/webfetch.go`） |
| WebSearch | `websearch` | ✅ 已移植（`core/tool/websearch.go`） |
| Task | `task` | 待移植（v2 core 无对应实现） |
| Question | `question` | ✅ 已移植（`core/tool/question.go`） |
| Skill | `skill` | ✅ 已移植（`core/tool/skill.go`） |
| TodoWrite | `todowrite` | ✅ 已移植（`core/tool/todowrite.go`） |
| LSP | `lsp` | 待移植（位于 opencode v1 层） |
| ApplyPatch | `apply_patch` | ✅ 已移植（`core/tool/apply_patch.go`） |

## 8. MCP 集成

**已实现**（`mcp/` 包）。移植 opencode `packages/opencode/src/mcp/` 的**纯 client + 适配**；OAuth/凭据/浏览器/TUI/v1 config 按 AGENTS.md 窄化掉，改为传入 `ServerConfig`。

协议层使用官方 Go SDK `github.com/modelcontextprotocol/go-sdk`（对标 TS 用官方 `@modelcontextprotocol/sdk`）。

```go
// mcp/client.go
type Client struct { /* sessions, status per server */ }
func NewClient(directory, version string) *Client
func (c *Client) Connect(ctx, name string, cfg ServerConfig) Status      // local: stdio; remote: StreamableHTTP → SSE 回退
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

// mcp/catalog.go —— 对应 McpCatalog
func Sanitize(value string) string
func ToolName(server, name string) string   // sanitize(server)+"_"+sanitize(name)
func ListTools/ListPrompts/ListResources/ListResourceTemplates(ctx, session)

// mcp/tool.go —— 对应 McpCatalog.convertTool
func AdaptMCPTool(def *sdk.Tool, session *sdk.ClientSession, server string, timeout time.Duration) tool.Tool
type AdaptedTool struct { Def *sdk.Tool; Session *sdk.ClientSession; Server string; Timeout time.Duration }
func (t AdaptedTool) CoreTool() tool.Tool
```

- `schema/mcp_event.go`：`mcp.tools.changed` / `mcp.browser.open.failed`。
- 远程鉴权：**完全对齐 TS** —— 只用 `ServerConfig.Headers`（如 `Authorization: Bearer <token>` 或 `X-API-Key: <key>`），不做 OAuth（TS 的 `oauth-provider.ts`/`oauth-callback.ts`/`browser.ts`/`auth.ts` 均不移植）。也不接 `EventV2Bridge`/`InstanceState`/`CrossSpawnSpawner`/`ConfigV1`。
- 其他已对齐：`GetPrompt`/`ReadResource`、server `instructions`、`clientName` 过滤、运行时 `Add`、server 日志（`WithLogHandler`）。
- 唯一无法对齐：`callTool` 的 `resetTimeoutOnProgress`/`onprogress`（进度重置超时）—— Go SDK 的 `CallTool` 未暴露该选项，只能用 context 超时。

## 9. Skill 系统

> **已实现。** 对应 opencode `packages/core/src/skill.ts`、`packages/core/src/skill/`（见 `core/skill.go` / `core/skill/`）。

```go
type Skill struct {
    Name        string
    Description string
    Prompt      string       // 完整的 SKILL.md 内容
    Source      SkillSource  // File | URL | Git
}

type SkillLoader interface {
    Discover(ctx context.Context, dir string) ([]Skill, error)
    Load(ctx context.Context, path string) (*Skill, error)
}
```

发现路径 (按优先级):
1. `.opencode/skills/**/SKILL.md`
2. `.agents/skills/**/SKILL.md`
3. `.claude/skills/**/SKILL.md`
4. 用户配置目录 `{skill,skills}/**/SKILL.md`

---

## 10. 权限系统

```go
// core/permission.go（对应 packages/schema/src/permission.ts + packages/core/src/permission.ts）
type PermissionRule struct {
    Action   string           // glob（如 "edit"、"read.*"、"*"）
    Resource string           // glob
    Effect   PermissionEffect // allow | deny | ask
}

type PermissionRuleset []PermissionRule

// 取最后一条同时匹配 action 与 resource 的规则；无匹配则默认 ask
func PermissionEvaluate(action, resource string, rulesets ...PermissionRuleset) PermissionRule
func PermissionMerge(rulesets ...PermissionRuleset) PermissionRuleset

// 评估 + 断言：deny → 报错；ask → 创建待审批请求并阻塞等待 reply
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

## 11. SDK 层 (高级 API)

公开入口位于顶层 `agent/` 包，导入路径 `github.com/user/go-agent-sdk/agent`。**已实现**，形态参考 `github/copilot-sdk/go`（`Client` → `CreateSession` → `Send`/`On`）。

```go
import "github.com/user/go-agent-sdk/agent"

// ─── Client ─────────────────────────────────────────────
type ClientOptions struct {
    Provider         ProviderConfig            // Type("openai"|"anthropic")/BaseURL/APIKey
    Model            string                    // 默认模型
    WorkingDirectory string
    DataDir          string                    // 会话数据库目录
    Tools            []Tool                    // 应用级工具
    MCP              []MCPConfig               // MCP servers（工具注册为应用级）
    Plugins          []PluginConfig            // 启动时加载的插件
    Permission       PermissionHandlerFunc     // 默认权限回调
    LLM              route.LLMClient           // 可选：注入自定义 LLM client
}
func NewClient(*ClientOptions) (*Client, error)
func (c *Client) RegisterAgent(name string, cfg AgentConfig) error
func (c *Client) CreateSession(ctx, *SessionConfig) (*Session, error)
func (c *Client) ResumeSession(ctx, sessionID string, *SessionConfig) (*Session, error)
func (c *Client) ListSessions(ctx) ([]SessionInfo, error)
func (c *Client) DeleteSession(ctx, sessionID string) error
func (c *Client) Close() error

// ─── Session ────────────────────────────────────────────
type SessionConfig struct {
    Agent        string
    Model        string
    Title        string
    SystemPrompt *SystemPromptConfig           // append | replace | customize（内置 agent_system/instructions + 自定义 section）
    Tools        []Tool
    OnPermissionRequest PermissionHandlerFunc  // 覆盖 client 级权限回调
    Hooks        *SessionHooks
}
type MessageOptions struct { Prompt string; Mode string } // Mode: "queue"(默认)|"steer"

func (s *Session) ID() string
func (s *Session) Messages() ([]schema.SessionMessage, error)       // 历史读取
func (s *Session) Send(ctx, MessageOptions) iter.Seq2[Event, error] // 本轮流，结尾 Idle
func (s *Session) On(func(Event)) (unsubscribe func())
func (s *Session) Abort()
func (s *Session) Close() error

// ─── 事件（类型化并集） ──────────────────────────────────
type Event interface{ eventKind() string }
// TextDelta / TextEnded / ReasoningDelta / ReasoningEnded /
// ToolCalled / ToolSuccess / ToolFailed / StepStarted / StepEnded /
// StepFailed / CompactionEnded / ErrorEvent / Idle

// ─── 用户工具 ───────────────────────────────────────────
func DefineTool[In, Out any](name, description string,
    handler func(In, ToolContext) (Out, error)) Tool
func SchemaForType(reflect.Type) *jsonschema.Schema // reflect → AST

// ─── 权限 / Hooks ───────────────────────────────────────
type PermissionHandlerFunc func(PermissionRequest) (PermissionDecision, error)
var ApproveAll PermissionHandlerFunc
type SessionHooks struct { /* Pre/PostToolUse, UserPromptSubmitted,
                              SessionStart/End, ErrorOccurred */ }
```

实现要点：
- 进程内运行（无外部 runtime 进程）：`NewClient` 组装 DB + 事件服务 + projectors + 工具注册 + runner + coordinator。
- 工具**按会话隔离**：每个会话一个 registry（内置 + 客户端工具 + 会话工具），通过 `core/session/runner.ToolRegistry.MaterializeFor(sessionID, permissions)` 分派。
- 权限：`OnPermissionRequest` 回调经装饰器桥接 `core.Permission`（allow/deny/ask → 回调 → 自动 reply）。
- 工具 Hooks（Pre/PostToolUse）通过 `hookedRegistry` 包裹 settlement 实现。

> 未完成：MCP/Plugin 工具接入；`reference` 服务仍是 no-op；provider 仅 openai/anthropic；system prompt sections 粒度比 copilot 粗。

## 12. 错误处理

```go
// llm/schema/errors.go
type LLMError struct {
    Module string          // 例如 "ProviderShared"
    Method string          // 例如 "stream"
    Reason LLMErrorReason
}

type LLMErrorReason struct {
    Kind    LLMErrorReasonKind // InvalidRequest | NoRoute | Authentication | RateLimit |
                               // QuotaExceeded | ContentPolicy | ProviderInternal |
                               // Transport | InvalidProviderOutput | UnknownProvider
    Message string
    // 可选字段：Parameter / Classification / Status / Http / Route / Provider / Model / ...
}

// 工具失败（对应 TS @opencode-ai/llm 的 ToolFailure）
type ToolFailure struct {
    Message  string
    Cause    error
    Metadata map[string]any
}
```

> 权限相关错误见 `core/permission.go`：`PermissionBlockedError`、`PermissionDeclinedError`、`PermissionCorrectedError`、`PermissionNotFoundError`。

---

## 13. 流式事件模型

> 全路径 `SessionEvent`（agent 层）尚未移植。当前规范事件是 `llm/schema/LLMEvent`（provider → 调用方，见 §5.3），通过 `iter.Seq2[schema.LLMEvent, error]` 返回。

---

## 14. 构建与测试

```bash
# 测试
go test ./...
go test -race ./...
```

---

## 15. 参考

- [opencode GitHub](https://github.com/anomalyco/opencode)
- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat)
- [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses)
- [Anthropic Messages API](https://docs.anthropic.com/en/api/messages)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [OpenRouter API](https://openrouter.ai/docs)