# go-agent-sdk 设计文档

> ⚠️ **本文档中的 Go 代码仅为示意**，用于表达架构设计和模式，不代表最终实现。每个函数签名、类型定义、错误处理的细节都以 opencode 的 TypeScript 源码为准。移植时请对照 opencode 对应文件的完整逻辑。

## 1. 项目概述

go-agent-sdk 是 [opencode](https://opencode.ai) 的 Go 移植版，目标是提供一套完整可嵌入的 Agent SDK。核心能力包括：

- **LLM 协议层** — 支持 OpenAI Chat / OpenAI Responses / Anthropic Messages / Gemini / Bedrock Converse 等协议（另有 openai-compatible chat），
  调用方按**协议 id** 选择（`ClientOptions.Protocol`），六种协议均可经 `llm/protocols` 注册表到达；`llm/provider/` 的 facade 只是预置默认值的便捷层（见 §5）。
- **Agent 循环** — 自动化的 tool-calling agent 循环，支持步骤管理、上下文溢出恢复、steer/queue 输入。
- **Agent 定义** — 可配置的 agent 元信息、system prompt、权限规则、模型覆盖。
- **Skill** — 从 markdown 文件加载专业知识，注入系统上下文。
- **MCP 集成** — 原生 Model Context Protocol 客户端，支持本地和远程 MCP 服务器。
- **内置工具** — read, write, edit, apply_patch, bash, glob, grep, webfetch, question, skill, todowrite（11 个，见 §7.4）；另有 `task`（subagent 委托，见 §6.3）。
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
| 内置工具 | 11 个（`core/tool/`，见 §7.4） | read/write/edit/apply_patch/bash/glob/grep/webfetch/question/skill/todowrite |
| `core/permission.go` | 规则引擎（allow / deny / ask） | 权限评估 |
| `core/` | file-mutation / location-mutation / git / database / process ... | 支撑模块 |
| `agent/` | `NewClient` / `CreateSession` / `Send` / `On` / `RegisterAgent` | 公开 API（已实现，见 §11） |
| `schema/` | 基础类型 | JSON Schema、ID 生成 |

### 不包含（推迟到后续版本）

| 模块 | 推迟原因 |
|---|---|
| `lsp` 工具 | 非核心路径，后续追加 |
| Provider facade（OpenRouter / xAI / Azure / Cloudflare / GitHub Copilot / Google / Bedrock 的预置 base URL） | 协议与 facade 已实现；`agent` 层改用 `Protocol` + `ModelInfo`，facade 仅供直接使用 `llm` 包的调用方（见 §5.4） |
| `bash` 高级特性（tree-sitter、arity、后台任务、progress 流） | 见 `core/tool/bash.go` 的 TODO |
| `edit` 高级特性（fuzzy match、formatter、watcher、snapshot、LSP） | 见 `core/tool/edit.go` 的 TODO |
| `apply_patch` 的 move 与原子回滚 | 见 `core/tool/apply_patch.go` 的 TODO |
| credential / catalog / integration 服务 | CLI 基础设施，SDK 改用窄输入（见 AGENTS.md） |
| `weak` 包、`os.Root` 等安全特性 | 后续加固 |

> 已实现（早期文档曾列为"推迟"，现已落地）：MCP 集成（§8）、Skill 系统（§9）、Config frontmatter 加载（`defs/config`）、上下文压缩（§6）、Steer/Queue 输入、Plugin（窄化，§7.3）、Reference 服务、`task` 子 agent（§6.3）。

### MVP 用户代码示例

```go
client, err := agent.NewClient(&agent.ClientOptions{
    Model:       "gpt-4o",
    Protocol:    "openai-responses",           // 协议 id
    Credentials: agent.APIKey(os.Getenv("OPENAI_API_KEY")),
    // 可选：catalog 元数据（endpoint/limits/headers）。不设则用协议路由默认值。
    ModelInfo: func(id string) (schema.ModelInfo, error) {
        return agent.NewModelInfo("openai-responses", id, "https://api.openai.com/v1",
            schema.ModelLimit{Context: 128_000, Output: 8_192}) // 启用自动压缩
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
│ write, ...   │ │ (已实现) │ │ (已实现)   │ │                  │
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
- `mcp/` 包装官方 Go SDK `github.com/modelcontextprotocol/go-sdk`（local stdio / remote StreamableHTTP→SSE 回退），不涉及 LLM 概念；`mcp/tool.go` 的适配层将 MCP Tool 转为 LLM 可消费的 ToolDefinition

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
│   ├── provider_error.go            provider-error.ts（re-export llm/schema 的实现）
│   ├── schema/                      LLM 规范类型 (schema/)
│   │   ├── ids.go                   ProviderID, ModelID, ProtocolID, FinishReason, MessageRole
│   │   ├── events.go                LLMEvent (events.ts)
│   │   ├── messages.go              LLMRequest, LLMResponse, Message (messages.ts)
│   │   ├── options.go               GenerationOptions (options.ts)
│   │   ├── errors.go                LLMError + ProviderError (errors.ts)
│   │   └── provider_error.go        IsContextOverflow 规范实现（llm 与 llm/route 共用）
│   ├── route/                       Route 层 (route/)
│   │   ├── client.go                Route + LLMClient (route/client.ts)
│   │   ├── transport.go             Transport / TransportRuntime / TransportPrepareInput
│   │   ├── protocol.go              Protocol (route/protocol.ts)
│   │   ├── endpoint.go              Endpoint (route/endpoint.ts)
│   │   ├── auth.go                  Auth (route/auth.ts)
│   │   ├── auth_options.go          AuthOptions (route/auth-options.ts)
│   │   ├── framing.go               Framing SSE (route/framing.ts)
│   │   ├── executor.go              RequestExecutor (route/executor.ts)
│   │   └── transport/               HttpTransport (route/transport/)
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
│   ├── effect/                      Effect 运行时工具 (effect/keyed-mutex.ts, ...)
│   ├── project/                     project/schema.ts, project/directories.ts
│   ├── tool/                        工具框架 (tool/)
│   │   ├── tool.go                  tool.ts
│   │   ├── registry.go              registry.ts
│   │   ├── read.go / read_filesystem.go  read.ts / read-filesystem.ts
│   │   ├── write.go                 write.ts
│   │   ├── edit.go                  edit.ts
│   │   └── bash.go                  bash.ts
│   └── util/                        通用工具 (util/wildcard.ts, util/hash.ts, util/markdown.ts)
│       └── markdown.go              ConfigMarkdown（frontmatter 解析，config/markdown.ts）
├── opencode/       ← packages/opencode/src/      (opencode CLI)
│   ├── config/                      内容型配置加载（agent/mode markdown）
│   │   ├── agent.go                 config/agent.ts（LoadAgentEntries）
│   │   ├── agent_info.go            config/agent.ts 的 AgentInfo 类型
│   │   ├── entry_name.go            config/entry-name.ts
│   │   └── agent_apply.go           config/plugin/agent.ts 的应用逻辑
│   ├── skill/                       技能发现 (skill/index.ts 的 discoverSkills)
│   │   └── skill.go                 DiscoverSkills（仅扫描给定目录）
│   └── id/                          带前缀的单调 ID 生成 (id/id.ts)
├── agent/          ← 无 TS 对应目录（高层公开 API，对标 copilot-sdk/go）
│   ├── client.go / session.go / runtime.go    Client/Session/runtime 装配
│   ├── task.go / question.go                  task（subagent）与 question 工具
│   ├── events.go / hooks.go / permission.go   事件/钩子/权限
│   └── ...
└── schema/         ← packages/schema/src/        (@opencode-ai/schema)
    ├── schema.go                    schema.ts
    ├── identifier.go                identifier.ts
    ├── event.go                     event.ts
    ├── session_event.go             session-event.ts（含 durable/live-only 清单）
    ├── session_message*.go          session-message.ts + JSON 解码
    ├── agent.go / permission.go / skill.go / location.go / project.go
    ├── model.go / provider.go / question.go / prompt.go / ...
    └── v1/                          legacy SessionV1 事件形状
```

> 上面只是**代表性**目录，非完整清单（`schema/` 约 42 个文件、`core/` 模块更多）。

> 已实现：高层 `agent/` 公开 API、core/session 主循环、12 个内置工具 + `task`、Skill、MCP、Plugin（窄化）、Reference 服务。尚未移植：`lsp` 工具。

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
    StreamPrepared   func(ctx context.Context, prepared any, request *schema.LLMRequest, runtime TransportRuntime) iter.Seq2[any, error]
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
    // 协议 body 契约（jsonschema AST）；路由构造时编译，compile() 校验每个 body。
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

> **body 校验**：每个协议在 `llm/protocols/body_schema.go` 定义 `Schema`（对齐 TS 的 Effect body schema）；`MakeFromTransport` 用 `jsonschema.CompileValidation` 编译 `Validation()` 投影，`llm.Client.compile` 在 `Body.From` 之后校验 marshal 出来的 body，不合法则返回 `InvalidRequestReason`（对齐 TS `ProviderShared.validateWith`）。这层校验抓到了几处 lowering 偏差（如 `messages: null` 而非 `[]`、`parameters: null`）。

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

> SDK 的 `agent` 层**不直接使用**这些 facade：它按 `ModelInfo.API.Protocol` 查 `llm/protocols` 注册表（协议 id → route + 认证方案）并自行套 `schema.Credential`。facade 供直接使用 `llm` 包的调用方。

```go
// llm/provider/openai.go —— 传入预配置的 route（见 llm/protocols/）
openai := provider.NewOpenAI(responsesRoute, chatRoute, provider.OpenAIConfig{
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

> **已实现。** 对应 opencode `packages/core/src/agent*`、`packages/opencode/src/session/`；实现在 `core/session/`（`run_coordinator.go`、`runner/`、`compaction.go`、`context_epoch.go` 等）与 `agent/runtime.go`。

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
            9. Steer 事件 → 在下一轮边界提升（不打断进行中的 provider 流）
      → 检查队列输入 → 继续外层循环
```

> 上下文压缩依赖模型的 `Limits`（`ModelInfo.Limit` → route defaults → `compactionModelLimits`）。**未设置 `Limits` 时自动压缩被禁用**，超长请求会直接以 provider 400 失败。SDK 不内置模型目录（catalog），由调用方按需提供 `Context`/`Output` 限额。

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
    Steps       int             // max turn steps; 0 = 不限制（与 TS 一致，无默认值）
    Temperature float64
    TopP        float64
}
```

### 6.3 Subagent (TaskTool) — 已实现

对应 opencode `packages/opencode/src/tool/task.ts`。`task` 是一个内置工具：主 agent 调用它后，在**独立子会话**里运行另一个 agent，并把结果文本返回。实现在 `agent/task.go`（工具定义 + `DeriveSubagentSessionPermission`）与 `agent/runtime.go`（`runtime.runTask`）。

**工具参数**：`description`、`prompt`、`subagent_type`、`task_id`（续跑同一子会话）、`command`、`background`（暂不支持，返回失败）。

**执行流程**：
1. 深度检查：沿 `parent_id` 数子会话深度，超过 `ClientOptions.SubagentDepth`（默认 1）报错。
2. 权限检查：action `task`、resource `subagent_type`。
3. 解析 agent：`subagent_type` 必须是已注册 agent，否则报 `Unknown agent type`。
4. 派生权限：`DeriveSubagentSessionPermission(父会话权限, 子 agent)` —— 继承父的 deny 规则 + `external_directory`，并默认追加 `todowrite` / `task` 的 `deny`（除非子 agent 自己已声明该 action）。写入子会话的 `session.permission` 列。
5. 创建子会话：`parent_id = 当前会话`、`agent = subagent_type`、`permission = 派生规则`；给了 `task_id` 且会话存在则复用（复用其已存储的 agent 与 permission）。
6. `Admit` + `coordinator.Run(childID)` 跑一轮；从 `session_message` 取最后一条 assistant 消息的最后一段 text 返回。工具上下文被取消时 `coordinator.Interrupt(childID)`。
7. 结束后移除子会话的 dispatcher / 权限登记（保留 session 行以便 `task_id` 续跑）。

**权限生效**：`core.PermissionSessionInfo.Permissions` 让 `configured` 返回 `merge(agent.permissions, session.permissions)`；runner 的 `MaterializeFor` 也合并 session 规则（工具可见性）。均对齐 TS 的 `Permission.merge(agent.permission, session.permission)`。

**工具描述**：由 `SubagentTypes(agents, 调用方权限)` 动态注入 `mode != "primary"` 且 `task` 权限未 deny 的 agent 列表，对齐 TS `registry.describeTask`。

**可观测性**：`runTask` 通过工具的 progress 回调在父会话上发布 **durable** 的 `session.next.tool.progress` 事件（`structured` 携带 `sessionId` / `parentSessionId` / `subagentType` / `description` / `model`），对齐 v2 的 tool invocation progress（`Tool.Context.Progress`）。用 `Client.On` 全局订阅即可观察子会话的 reasoning/text/tool 事件（`Session.On` 按 session 过滤，看不到子会话）。

**与 `mode` 的关系**：`mode: subagent`（或 `all`）的 agent 会出现在 `subagent_type` 列表；`primary` 只当主 agent。

**已知偏差**：`background`（实验性后台子 agent）未实现，请求时返回失败（对齐 TS 在 flag 关闭时的行为）。

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
    // 发布 running-tool 状态（durable session.next.tool.progress）
    Progress           func(structured map[string]any, content []Content) error
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
    Execute            func(execCtx context.Context, input Input, ctx Context) (Output, error)
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
        Execute: func(execCtx context.Context, input WriteInput, ctx Context) (WriteOutput, error) { /* ... */ },
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

> MCP 已实现（见 §8，`mcp/` 包）。Plugin 已实现（窄化：`core/plugin.go` 的 lifecycle 管理器 + `agent` 接线，见 `PluginContext`）；TS 的动态工具（外部 JSON Schema）未移植，MCP 工具即通过 `jsonschema.Raw` 走这条路径。

> **工具输出限额**：`core.ToolOutputStore`（`MAX_LINES=2000` / `MAX_BYTES=50KB`）会把超限的 tool result 落盘为 `tool-output/tool_*`，只把 head/tail 预览发给模型（`core/tool/output_store.go` 适配 `tool.OutputStore`，由 `agent/runtime.go` 通过 `tool.WithOutputStore` 注入）。**未注入时 registry 的 `store == nil`，限额被跳过**，一次大 `grep`/`read` 就能撑爆上下文窗口。
>
> 落盘目录是 `{Global.Data}/tool-output`，即 `{ClientOptions.Root}/data/tool-output`；`ClientOptions.Root` 必填。`ToolOutputStore.Cleanup` 按文件 mtime 清理超过 7 天（`ToolOutputStoreRetention`）的文件：`agent` 层在 `newRuntime` 时起一个 goroutine，启动时跑一次、之后每小时一次，`Client.Close()` 时随 runtime 的 lifetime context 退出；也可用 `Client.CleanupToolOutput()` 手动触发（对齐 v2 `tool-output-store.ts` 的 `cleanupLayer` + `cleanupNode`）。

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
| Task | `task` | ✅ 已移植（`agent/task.go`；子会话 + 权限派生，见 §6.3） |
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

发现路径（`ClientOptions.ConfigDirs` 的每个目录，按优先级）:
1. `{skill,skills}/**/SKILL.md`
2. `.agents/skills/**/SKILL.md`
3. `.claude/skills/**/SKILL.md`

> 仅扫描显式给出的目录，不做环境变量/全局目录/向上查找（见 §11.1）。

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
    // 位置
    WorkingDirectory string                    // 项目/worktree 根；空则用进程 cwd
    Root             string                    // SDK 状态根（data/cache/state/tmp）；**必填**，库不选默认位置
    ConfigDirs       []string                  // 内容配置目录（agent/skill markdown，见「配置模型」）
    Home             string                    // ~ 展开用；空则用 OS home

    // 模型与 provider
    Model            string                    // 默认模型
    Protocol         schema.ProtocolID         // 协议 id（"openai-responses"|"anthropic-messages"|"gemini"|...）
    ModelInfo        ModelInfoProvider         // 可选：catalog 元数据（protocol/endpoint/limits/headers）
    Credentials      CredentialProvider        // provider → 秘密（APIKey / AWS）
    LLM              route.LLMClient           // 可选：注入自定义 LLM client

    // 工具与扩展
    Tools            []Tool                    // 应用级工具
    MCP              []MCPConfig               // MCP servers（工具注册为应用级）
    Plugins          []PluginConfig            // 启动时加载的插件
    References       map[string]core.ReferenceSource // 命名 reference 源
    ToolOutput       *ToolOutputConfig         // 工具输出落盘上限（MaxLines / MaxBytes）

    // 运行行为
    Permission       PermissionHandlerFunc     // 默认权限回调（默认 ApproveAll）
    Question         QuestionHandlerFunc       // 默认 question 回调（默认 RejectQuestions）
    SubagentDepth    int                       // task 子会话最大深度（默认 1）
    Shell            string                    // bash 工具使用的 shell（默认 sh）
    Locale           string                    // 列表排序 locale；空则用 LC_ALL/LC_COLLATE/LANG
    Compaction       *CompactionConfig         // 上下文压缩（Auto / PreserveRecentTokens / Reserved）
}
// 用 NewModelInfo(protocol, modelID, baseURL, limit) 构造受支持的 ModelInfo。
// ModelInfo 是 models.dev catalog 的窄输入替代：调用方提供 limits/endpoint/headers/variants，
// SDK 走 runner.FromCatalogModel 建 route（Protocol 决定鉴权与路由）。
type ModelInfoProvider func(modelID string) (schema.ModelInfo, error)
func NewClient(*ClientOptions) (*Client, error)
func (c *Client) RegisterAgent(name string, cfg AgentConfig) error
func (c *Client) CreateSession(ctx, *SessionConfig) (*Session, error)
func (c *Client) ResumeSession(ctx, sessionID string, *SessionConfig) (*Session, error)
func (c *Client) ListSessions(ctx) ([]SessionInfo, error)
func (c *Client) DeleteSession(ctx, sessionID string) error
func (c *Client) On(func(Event)) (unsubscribe func())   // 全局订阅（含子 agent 会话）
func (c *Client) SessionMessages(sessionID string) ([]schema.SessionMessage, error) // 读任意会话（含子会话）的消息
func (c *Client) Close() error

// ─── Session ────────────────────────────────────────────
type SessionConfig struct {
    Agent        string
    Model        string
    Title        string
    SystemPrompt *SystemPromptConfig           // append | replace | customize（内置 agent_system/instructions + 自定义 section）
    Permissions  schema.PermissionRuleset      // 本会话规则，合并覆盖 agent 的权限（session 规则优先）
    Tools        []Tool
    OnPermissionRequest PermissionHandlerFunc  // 覆盖 client 级权限回调
    OnQuestion   QuestionHandlerFunc           // 覆盖 client 级 question 回调
    Hooks        *SessionHooks
}
type MessageOptions struct { Prompt string; Mode string } // Mode: "queue"(默认)|"steer"

func (s *Session) ID() string
func (s *Session) Messages() ([]schema.SessionMessage, error)       // 历史读取
func (s *Session) Send(ctx, MessageOptions) iter.Seq2[Event, error] // 本轮流，结尾 Idle
func (s *Session) On(func(Event)) (unsubscribe func())
func (s *Session) Abort()
func (s *Session) Close() error

// Usage 是单轮事件，携带 provider 的**原始 usage 对象**（放在 ProviderMetadata，按 provider
// 名索引，如 {"openai": {...}}，含 DeepSeek 的 prompt_cache_hit_tokens 等扩展字段）。
// SDK 不合计、不算 cost；app 自己订阅、按 provider 语义理解与计费。
// （原始 usage 经 LLMEvent.Usage.ProviderMetadata → LLMEventTokens.ProviderMetadata → step.ended 透传，
//   各 protocol mapper 都设 providerMetadata: {<provider>: usage}，与 TS 一致。）

// ─── 事件（类型化并集） ──────────────────────────────────
type Event interface{ eventKind() string }
// TextDelta / TextEnded / ReasoningDelta / ReasoningStarted / ReasoningEnded /
// ToolInputStarted / ToolCalled / ToolProgress / ToolSuccess / ToolFailed /
// StepStarted / StepEnded / StepFailed / Retried / SessionStatus / Usage /
// CompactionStarted / CompactionEnded / ShellStarted / ShellEnded /
// ErrorEvent / Idle

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

> 已接入：MCP 工具（`connectMCP`）、Plugin（窄化）、Reference 服务。system prompt sections 粒度比 copilot 粗。取消语义（对齐 TS `run-coordinator.ts` 的 `uninterruptibleMask`）：**只有 `Abort()`** 经 `coordinator.Interrupt` 取消 drain ctx 并中断进行中的 LLM 流与工具执行；取消 `Send(ctx)` 的 ctx 只停止等待，不中断当前轮。`context` 已贯穿 `SessionRunner.Run` → `Transport.Frames` → `http.NewRequestWithContext`，所以 `Abort()` 能真正打断 provider 请求。

### 11.1 配置模型（code config + 内容目录）

SDK **不使用 `opencode.json`**，也不做目录发现（无 `OPENCODE_CONFIG_DIR` / `XDG_*` / 向上查找 `.opencode`）。所有**设置**都由代码通过 `ClientOptions` 传入（见上）。

### 11.2 状态目录（一个 Root，不读 XDG/OPENCODE env）

TS 的 `global.ts` 是 CLI 模块：加载时读 `XDG_*` / `OPENCODE_CONFIG_DIR` 等 env。**SDK 刻意偏离**——可嵌入的库不该读宿主进程的 env 来定位自己的状态，只保留「宿主事实」类输入（`$HOME`、Windows 的 `COMSPEC`、`LC_*`/`LANG` 作为 locale 默认）。全部状态由 `ClientOptions.Root` 派生（`core.NewGlobal(root, home)` 是纯函数，不读 env）：

```
{Root}/data/    agent.db、snapshot、reference clone、plans、tool-output、log
{Root}/cache/   下载的二进制（ripgrep）、skill 内容缓存
{Root}/state/   运行期状态（保留，对齐 TS 的 flock）
{Root}/tmp/     临时文件
```

`Root` **必填**：库不选默认位置，否则会往宿主没指定的目录（例如 opencode CLI 的 `~/.local/share/opencode`）读写，且行为会被机器上已有的 DB / clone / snapshot 影响。想沿用 CLI 约定的宿主自己算（`$XDG_DATA_HOME/opencode` 或 `~/.local/share/opencode`，示例里就是 `dataRoot()`；`~` 来自 `os.UserHomeDir`，是宿主事实）。除 `$HOME` / `COMSPEC` / locale 默认外，SDK 不读任何 `XDG_*` / `OPENCODE_*`；每个实例指向自己的 `Root` 即可完全隔离（测试、沙箱、多租户），`agent` / `examples` 的测试因此不需要再污染 `XDG_*`。

同一原则也适用于仓库：`core/repository.go` 不再读 `OPENCODE_REPO_CLONE_GITHUB_BASE_URL`，改为显式输入：

- github 镜像：`core.WithGitHubBaseURL(base)` 传给 `RepositoryParse` / `NewRepositoryCache`（TS 对**所有** github.com host 都走 `githubRemote`，显式 URL 同样被改写，以保证 origin 与 clone target 一致）。
- 排序 locale：`util.CompareLocale(left, right, tag)` 显式接收 tag，`ClientOptions.Locale` 一路传到 read 工具的目录列表、skill/reference guidance 与 task 的 subagent 列表；`util.ResolveLocale(name)` 负责解析，空值回退到 env 推断出的 `util.LocaleTag`（对齐 JS `localeCompare` 的默认行为）。
- `core/flag` **不移植**：`flag.ts` 是 CLI 的 env 模块（import 时把 ~25 个 `OPENCODE_*` / `OTEL_*` 快照进 `Flag` 表），SDK 没有消费者，唯一用到它的 websearch 工具已按下面的理由删除。
- **`websearch` 工具不移植**：它硬编码 Exa / Parallel 两家商业后端的 MCP 端点，且必须靠宿主 env 传 provider key —— TS 自己注明这是「retained in V2 core for launch parity」的妥协。宿主想要搜索能力可以用 `agent.DefineTool` 自行接入；通用的 `webfetch`（HTTP + HTML→markdown）保留。

文件只承载**内容**（agent / mode / skill 的 markdown），且来源必须**显式**声明：`ClientOptions.ConfigDirs`。每个目录按以下模式扫描：

- `{agent,agents}/**/*.md` —— 子 agent
- `{mode,modes}/*.md` —— primary agent（旧称 mode）
- `{skill,skills}/**/SKILL.md` —— 技能

优先级：内置 agent（代码）< `ConfigDirs`（文件）< `RegisterAgent`（代码）。

与 TS 的差异：TS 从 `.opencode` 向上查找并合并 `opencode.json` + 环境变量 + 全局目录；Go SDK 只读取调用方给出的目录，避免隐式 I/O 与全局状态。`core/config.go`（Config.Info/Document 发现）、`core/config/`、`core/v1/config/`（legacy ConfigV1）**已删除**。

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

> **已实现。** 规范事件是 `schema/session_event.go` 的 `SessionEvent*`（core session 事件，durable 落库 + live-only delta）；runner 通过 `LLMEventPublisher` 把 `llm/schema/LLMEvent` 翻译成 session 事件。agent 层再把 session 事件映射为高层 `agent.Event` 并集（`agent/events.go`），`Session.Send` 返回 `iter.Seq2[agent.Event, error]`（以 `Idle` 结束）；`Client.On` 可全局订阅（含子 agent 会话）。provider → 调用方的原始流仍是 `iter.Seq2[schema.LLMEvent, error]`（见 §5.3）。

> **会话运行状态**：SDK 在 drain 生命周期发 `session.status`（`busy`/`idle`，live-only），agent 层映射为 `agent.SessionStatus`。订阅者据此判断某会话是否在运行，从而只在运行时渲染 spinner/心跳（对齐 TS `session.status`；TS 的发布者在 v1 CLI 层，Go 改为在 `runtime` 的 coordinator 闭包里发布）。

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