# go-agent-sdk — Coding Agent Reference

## 项目文档

- **`docs/DESIGN.md`** — 架构设计、模块详细设计、MVP 范围、SDK 公开 API（**项目范围与公开 API 以本文档为准**）
- **`docs/PORTING-STRATEGY.md`** — TS/Effect → Go 的移植策略与决策树

## 文档中的 Go 代码

两文档中的 Go 代码仅为示意，函数签名、类型定义、错误处理等细节**以 opencode 的 TypeScript 源码为准**。移植时请对照 opencode 对应文件的完整逻辑。

## 移植规则

### 移植范围：legacy / v1 代码默认不移植

默认只移植 **v2 core**（`packages/core/src/`）。**legacy / v1 代码默认不移植**，除非确认它是 v2 core 的必需依赖——此时**先与用户确认**再动手。

判定为 legacy 的信号（满足其一即可）：
- 位于 `packages/opencode/src/session/`（v1 CLI 编排层）
- 导入 v1 模块：`v1/`、`SessionV1`、`ConfigV1`、`MessageV2`、`SessionProcessor`、`Plugin`、legacy `Provider`、`EventV2Bridge`、`RuntimeFlags`、`InstanceState` 等
- 面向 opencode CLI 的 HTTP handler / `app-runtime`

示例：`packages/core/src/session/compaction.ts`（v2，移植）vs `packages/opencode/src/session/compaction.ts`（v1 legacy，不移植，仅因复用 core 的 `buildPrompt` 而依赖前者）。

> 注意：`packages/opencode/src/` 下的**工具类**（如 `id/`、`tool/json-schema.ts`）不属于 legacy，仍按需移植。

### CLI 基础设施：不照搬，改用 SDK 原生窄输入

opencode 中**面向 CLI 的基础设施模块默认不照搬移植**，例如 `catalog`（models.dev 模型目录 + provider 注册表）、`credential`（`auth.json` 凭据存储）、`integration`（OAuth 连接流程/存储）等。

判定信号（满足其一即可）：
- 职责是「数据源 / 持久化 / OAuth / DI 服务」，而非 agent 循环的纯逻辑
- 依赖 Effect `Context.Service` + `Layer`（DI node）而本身没有实质算法

做法：**只移植其纯转换逻辑**，把数据源换成 SDK 原生窄输入（构造器注入）。
- 例：`session/runner/model.ts` 的 `fromCatalogModel` / `withDefaults` / `withVariant` 只依赖 `schema.ModelInfo` + API key + `llm` 的 route/protocol/Auth。移植时用「`ModelInfo` provider + API key」构造器替代 `Catalog.Service` / `Integration.Service` / `Credential.Value`，不必移植三个服务。
- 参考：docs/DESIGN.md 公开 API 直接传 `Protocol`（协议 id）+ `Credentials`（`schema.Credential`）+ `Model`，SDK 走 `llm/protocols` 注册表 + `llm/route`，不引入 catalog/credential/integration。

若确认某基础设施确实是必需依赖，**先与用户确认**再移植。

### 动手前必须读完整个 TS 源文件

之前犯过错误：`events.ts` 有 618 行，只读了前半部分（类型定义）就开始写，遗漏了后半部分（状态机、LLMResponse namespace）的 15+ 个方法。**每个文件必须完整读完再动手**，包括：
- 类型定义（struct/interface）
- 所有导出函数和方法
- namespace 中的静态方法
- 私有辅助函数（可能影响公共 API 设计）

### 同包符号命名：模块名前缀

TS 每个文件是独立 module（自带命名空间）；Go 一个包只有一个命名空间。当 `packages/<pkg>/src/` 下多个平铺文件进入**同一个 Go 包**时，重名符号会冲突，需用模块名前缀区分：

- 导出符号：**PascalCase 前缀** —— `OpenAIChatProtocol`、`OpenAIResponsesHTTPRoute`、`FileMutationTarget`、`LocationMutationTarget`、`PermissionRule`
- 非导出符号：**camelCase 前缀** —— `openaiChatBody`、`openaiResponsesParserState`、`anthropicMessagesImageMIMEs`
- 已带前缀的符号不重复加（如 `OpenAIChatDefaultBaseURL` 保持）

典型场景：`llm/protocols/`（5 个协议）、`core/`（file-mutation / location-mutation / permission / ...）、`schema/`（event / location / permission / project / ...）。

### 移植工作流

移植某个 TS 文件时：

1. **完整读完**目标 TS 文件（见上）
2. **分析依赖**：列出它 import 的所有 TS 模块（含传递依赖）
3. **逐个与 Go 实现对比**：
   - 已实现 → **逐行**对照 TS 源码核对逻辑一致性（不只是签名），发现差异就修复
   - 未实现 → 记为待移植
4. **按依赖顺序移植**：先移植未实现的依赖（依赖根/叶子优先），最后移植目标文件
5. **用 todo 列表跟踪**所有 review（对比）和移植任务；每完成一项就更新状态

**不要**：跳过依赖直接写目标文件；只对比签名而忽略实现逻辑。

## Go 版本

Go 1.27.1，以下特性可放心使用：
- `iter.Seq` / `iter.Seq2` — 替代 Effect Stream
- 泛型方法 — `func (List[E]) Map[R any](...)`
- 泛型类型别名 — `type StringMap[V any] = map[string]V`
- `weak` 包 — 弱引用缓存
- `os.Root` — 安全目录访问
- `testing/synctest` — 测试并发行为
- `new(expression)` — `new(ReadParams{Name: "foo"})`

## 关键移植决策速查

| TS/Effect 模式 | Go 方案 |
|---|---|
| `Effect<T, E>` / `yield*` | `(T, error)` + 显式传参 |
| `Stream<T, E>` | `iter.Seq2[T, error]` |
| `Schema.Struct({...})` + `Schema.optional` | Go struct + json tag + `*T` |
| `type E = A \| B \| C` (≤4, 结构差异大) | marker interface + type switch |
| `type E = A \| B \| C` (5-10, 字段重叠) | tagged struct + 构造器 |
| `type E = A \| B \| C` (>10, JSON 持久化) | `json.RawMessage` + 延迟分发 |
| `Context.Tag` + `Layer` | 构造器注入 |
| 同目录多个 TS module 平铺进一个 Go 包 | 模块名前缀区分符号（`OpenAIChatProtocol` / `openaiChatBody`） |
| `Route<Body, Prepared>` | `llm/route/` struct + `any` 类型擦除 |
| `WeakMap<AnyTool, Runtime>` | 泛型 `Def[Input, Output, Structured]` + `Tool` 接口 |
| `Schema.Codec` | `santhosh-tekuri/jsonschema/v6` 验证 |
| 工具 schema（`Schema.Struct` 等） | `jsonschema.Schema` 类型化 AST：`Model()` 发给模型 + `Validation()` 校验 |
| MCP（`@modelcontextprotocol/sdk`） | 官方 Go SDK `github.com/modelcontextprotocol/go-sdk` |
| CLI 的 env/XDG 默认（`global.ts`、`flag.ts`） | **刻意偏离**：库不读 `XDG_*`/`OPENCODE_*` 等宿主 env（只保留 `$HOME`/`COMSPEC`/`LC_*` 这类宿主事实）；状态目录由**必填**的 `ClientOptions.Root` 派生（`core.NewGlobal(root, home)` 纯函数），配置由 `ConfigDirs` 显式声明 |

## 包架构

Go 目录**直接镜像 opencode 的 TS monorepo 包结构**，不使用 `pkg/` 或 `internal/`。每个顶层目录对应 TS 的一个 package：

```
go-agent-sdk/                       opencode TS
├── llm/          ← packages/llm/src/        (@opencode-ai/llm)
│   ├── schema/     LLMRequest, LLMEvent, LLMError, ID 类型
│   ├── route/      Route, Protocol, Endpoint, Auth, Framing, Executor, Transport
│   ├── protocols/  各 provider 线协议（平铺：openai_chat.go, openai_responses.go, ...）
│   │   └── utils/   协议通用工具
│   ├── provider/   Provider facades (openai, anthropic, openrouter, ...)
│   └── utils/      通用工具 (record)
├── core/         ← packages/core/src/        (@opencode-ai/core)
│   ├── database/   SQLite + migrations
│   ├── effect/     Effect 运行时工具 (keyed-mutex)
│   ├── project/    Project schema + directories
│   ├── tool/       工具框架 (tool, registry, write, ...)
│   └── util/       通用工具 (wildcard, hash)
├── mcp/          ← packages/opencode/src/mcp/  (MCP client + 适配)
├── agent/        ← 高层公开 API（对标 copilot-sdk/go 形态，无 TS 对应目录）
│   Client / Session / Send / On / DefineTool / Hooks / Permission
├── opencode/     ← packages/opencode/src/    (opencode CLI)
│   ├── id/         带前缀的单调 ID 生成
│   └── tool/       opencode 工具层 (schema, json-schema)
└── schema/       ← packages/schema/src/      (@opencode-ai/schema)
```

- `jsonschema/` 是 Go 专属的类型化 JSON Schema AST（对标 Effect `Schema`），供 `core/tool` 定义工具 schema，无 TS 对应目录
- 全部为公开包，无需 `internal/`
- `packages/<pkg>/src/` 下的平铺文件进入 `<pkg>/` 包根目录；子目录成为子包
- Effect 的 `Context.Tag` + `Layer`（DI）不移植，改用构造器注入

## 目录/文件名对齐 opencode

规则：
- `packages/<pkg>/src/<path>/<name>.ts` → `<pkg>/<path>/<name>.go`，**kebab-case → snake_case**，目录层级不变
- 目录下的 `index.ts` 若需移植（含实现）→ `<pkg>/<path>/<目录名>.go`；纯 barrel（只 re-export）不移植，因为 Go 没有 re-export

| opencode TS | Go |
|---|---|
| `packages/llm/src/route/client.ts` | `llm/route/client.go` |
| `packages/llm/src/schema/messages.ts` | `llm/schema/messages.go` |
| `packages/llm/src/protocols/openai-chat.ts` | `llm/protocols/openai_chat.go`（同包平铺，符号加协议前缀） |
| `packages/llm/src/protocols/utils/tool-stream.ts` | `llm/protocols/utils/tool_stream.go` |
| `packages/core/src/file-mutation.ts` | `core/file_mutation.go` |
| `packages/core/src/location-mutation.ts` | `core/location_mutation.go` |
| `packages/core/src/tool/tool.ts` | `core/tool/tool.go` |
| `packages/core/src/tool/write.ts` | `core/tool/write.go` |
| `packages/core/src/effect/keyed-mutex.ts` | `core/effect/keyed_mutex.go` |
| `packages/core/src/util/wildcard.ts` | `core/util/wildcard.go` |
| `packages/core/src/project/directories.ts` | `core/project/directories.go` |
| `packages/schema/src/identifier.ts` | `schema/identifier.go` |
| `packages/opencode/src/id/id.ts` | `opencode/id/id.go` |

**易错点：**
- 平铺文件不要建同名子包：`core/src/file-mutation.ts` → `core/file_mutation.go`（package `core`），**不是** `core/filemutation/filemutation.go`
- 同一 Go 包内类型名会冲突，用模块名前缀区分（如 `FileMutationTarget` vs `LocationMutationTarget`、`OpenAIChatProtocol` vs `OpenAIResponsesProtocol`）
- `packages/opencode/src/tool/json-schema.ts` **不移植**：它只服务 CLI 工具 schema 的 `$ref` 内联，Go 侧没有消费者；曾移植的 `opencode/tool` 包已作为死代码删除。工具框架只有 `core/tool`
- `packages/core/src/effect/app-node.ts`、`layer-node.ts` 是 Effect DI 基础设施，不移植
- Go 无循环依赖；TS module 循环（如 `protocols/` ↔ `protocols/utils/`）需重构打破（如把 `utils` 反向依赖的 `ValidateMedia` 移入 `utils`）
- `packages/llm/src/route/transport/index.ts` 的 `Transport`/`TransportRuntime`/`TransportPrepareInput` 放在 `llm/route/transport.go`（package `route`），因为 `transport` 包反向依赖 `route`（`Framing`），放进 `transport/` 会循环
