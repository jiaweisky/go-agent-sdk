# go-agent-sdk A2A 支持设计文档

> 状态：**草案（决策已收敛），待评审**
> 范围：为 go-agent-sdk 增加 A2A（Agent2Agent）协议支持（入向 + 出向）
> 关系：本文是 `docs/DESIGN.md` 的分册，只覆盖 A2A 能力；通用架构、公开 API、移植规则仍以 `docs/DESIGN.md` 与 `AGENTS.md` 为准
> 约定：本文中的 Go 代码片段**仅为示意**，用于表达类型边界与契约，不代表最终签名

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-09-26 | 初稿：映射规格、入向/出向设计、路线图、决策清单 |
| 2026-10-04 | 新增 §0 协议速览（5W1H / 数据模型 / 状态机 / 操作流程 / 与 MCP 的区别） |
| 2026-10-04 | 出向收敛：删除独立「工具形态」，远端 agent 统一为「带 A2A backend 的 agent」，经 `task` 表现为子 agent、可作主 agent（§2.1 / §3.2 / §6.1） |
| 2026-10-04 | 依赖定稿：只支持 `a2a-go/v2` 单版本（D4）；适配层 `a2a/a2ago/`、逃生通道 `WithNativeOptions`；不依赖 adk-go（§3.1） |
| 2026-10-04 | 统一抽象定稿（A1/A2/A7 + A8 方案 C）：`turn` 升为 core 一等 durable 实体（`taskId = turn.id`，本地/远程 subagent 共用）；backend 分派 + 镜像子会话；复用 `task` 权限 action（§3.3 / §4.1 / §5.6） |
| 2026-10-04 | 入向暴露与安全定稿（A3/A4/A5/A6）：显式 `Expose`，一 card = 一 agent、多 agent = 多 card URL；权限可配置、默认对危险工具 `deny`；上下文显式导出（§3.4 / §5.5 / §6.2） |
| 2026-10-04 | SDK 侧改名：`PluginEffect` → `PluginFunc`、`PluginConfig.Effect` → `.Func`（去 Effect 残留） |
| 2026-10-04 | §4 映射与互操作（对照 adk-go v2 / a2a-go）：part 用 `adk_*` 约定、metadata 前缀 `adk_`/`a2a:`；新增 §4.7 互操作清单（身份键、artifact `append`/`lastChunk`、HITL `function_call`、增量同步） |
| 2026-10-04 | 入向 §5 重构：新增 §5.0 职责边界（继承 a2a-go；我们实现 `AgentExecutor` + `taskstore.Store`）；认证继承 / 授权自定义（§5.5）；HITL 唯一设计 = 挂起 + resume（D1），重启降级重建；心跳继承 `WithTransportKeepAlive`（§5.4 / §5.7） |
| 2026-10-04 | 出向 §6 重构：新增 §6.0 职责边界（继承 `a2aclient`）；`GetTask` / `ListTasks` / `SubscribeToTask` 归 a2a-go；HITL 映射为本地 `QuestionRequest`（D2） |
| 2026-10-04 | §7-§10 与附录收敛：Phase 0 = `turn` 实体 / backend 字段 + drain 分派 / 暴露标记；§9 结论「无架构级风险」；§10 全部决策收敛（A1–A8 / D1–D5）；允许直接改 `core/database/migration.go`、不做版本化迁移；附录 A 补 HITL 编码 / task 持久化对照 |

---

## 0. A2A 协议速览（读前必读）

> 本节只讲**协议本身**，不含本 SDK 的任何取舍。不熟悉 A2A 的读者先读这里，再看 §1 的背景判断。

### 0.1 是什么（What）

A2A（Agent2Agent）是一个开放标准，让**互相独立、彼此不透明（opaque）的 AI agent 系统**跨进程 / 跨主机 / 跨厂商互相调用。它复用既有 Web 标准（HTTP、JSON-RPC 2.0、SSE），目标是让 agent 能**发现**彼此能力、协作完成任务，并在**不暴露内部 state / memory / tool** 的前提下安全交换信息。

一句话：**A2A 是「agent 调 agent」的协议；MCP 是「agent 调工具」的协议**（详见 §0.6）。

### 0.2 5W1H

| 维度 | 内容 |
|---|---|
| **Why** | agent 系统异构（语言 / 框架 / 厂商不同），需要一个共同语言来委托任务、交换上下文 |
| **Who** | **A2A Client**（发起方）↔ **A2A Server / Remote Agent**（暴露 A2A endpoint 的一方）；两侧都可以是 agent |
| **What** | 围绕 `Task`（有状态工作单元）交换 `Message` / `Part` / `Artifact` |
| **When** | 同步一问一答、流式实时更新，或长任务异步 + push notification + human-in-the-loop |
| **Where** | HTTP 上的三种绑定：JSON-RPC 2.0、gRPC、HTTP+JSON/REST；端点与能力写在 AgentCard |
| **How** | 见 §0.5：发现 card → 发消息 → 任务状态机推进 → 取结果 / 续跑 / 取消 |

### 0.3 核心概念与数据模型

- **Agent Card**：服务端发布的 JSON 元数据（well-known URI `/.well-known/agent-card.json`），声明身份、接口 URL、传输绑定、`capabilities`（是否支持流式等）、`skills`、`securitySchemes`、`defaultInputModes/OutputModes`、`protocolVersion`。**客户端先取 card，才知道该怎么调**。
- **Message**：一次通信回合，有 `role`（`user` / `agent`）与若干 `Part`。
- **Part**：最小内容单元——文本、文件引用（URL / raw bytes）、结构化 data。
- **Task**：A2A 的核心工作单元，有唯一 `id` 与生命周期状态，产出 `Artifact`。
- **Artifact**：任务输出（文档 / 图片 / 结构化数据），由 `Part` 组成。
- **Context**：可选 id，把若干相关 task / message 逻辑分组（相当于「会话」）。
- **Streaming / Push Notification**：task 更新的两种异步投递方式（SSE 流 / 服务端向客户端 webhook 回调）。
- **Extension**：核心协议之外的可选扩展（`capabilities.extensions[]`，带 `required` 标记）。

标识符语义（本设计的关键落点，详见 §4.1）：

| 标识 | 语义 |
|---|---|
| `contextId` | 会话：多轮的分组 |
| `taskId` | 一次**有终态**的任务（比会话细一级） |
| `messageId` | 单条消息 |

### 0.4 Task 状态机

```
              ┌─────────────────────► canceled（终态）
              │
submitted ─► working ─┬─► completed（终态）
   │            │      ├─► failed（终态）
   │            │      └─► rejected（终态）
   │            │
   │            ├─► input-required ──(客户端再发 message)──► working
   │            └─► auth-required  ──(客户端提供凭据)────► working
   └─（也可能直接返回 Message，不建 Task 的简单交互）
```

- **终态**：`completed` / `failed` / `canceled` / `rejected`——终态 task 不再接受新 message。
- **可中断态**：`input-required`（等用户补充信息）/ `auth-required`（等授权凭据）——这正是 A2A 的 human-in-the-loop 原语，也是本设计最看重的部分（§5.4）。

### 0.5 核心操作与典型流程

绑定无关的核心操作（下表以 JSON-RPC 方法名为例）：

| 操作 | 作用 |
|---|---|
| Send Message | 发消息；返回 `Task`，或对简单交互直接返回 `Message` |
| Send Streaming Message | 同 Send，但以 SSE 流式返回状态 / artifact 事件 |
| Get Task | 按 `taskId` 查任务与历史 |
| List Tasks | 按 `contextId` / 状态列出任务（分页） |
| Cancel Task | 协作式取消 |
| Subscribe to Task | 断连后重订阅某任务的更新流 |
| Push Notification Config（CRUD） | 为任务配置 / 查询 / 删除 webhook 回调 |
| Get Extended Agent Card | 取认证后的扩展 card |

一次典型（流式 + HITL）往返：

```
Client                                      Server (Remote Agent)
  │  GET /.well-known/agent-card.json  ─────► │
  │  ◄────────────── AgentCard ──────────────│   发现
  │                                            │
  │  SendStreamingMessage(message)  ─────────► │
  │  ◄──── Task{state: submitted} ────────────│
  │  ◄──── TaskStatusUpdate{working} ─────────│
  │  ◄──── TaskArtifactUpdate{append} ────────│   流式产出
  │  ◄──── TaskStatusUpdate{input-required} ──│   需要用户输入
  │                                            │
  │  SendMessage(taskId, answer)  ───────────► │   多轮续跑
  │  ◄──── TaskStatusUpdate{working} ─────────│
  │  ◄──── TaskArtifactUpdate{lastChunk} ─────│
  │  ◄──── TaskStatusUpdate{completed} ───────│   终态
```

变体：

- **非流式**：`SendMessage` 阻塞到终态或中断态才返回（默认行为）；
- **异步**：`SendMessage{returnImmediately:true}` 立即返回 `working`，之后靠 `GetTask` 轮询 / `SubscribeToTask` / push notification 取更新；
- **取消**：客户端 `CancelTask`，服务端转换到 `canceled`。

### 0.6 A2A ≠ MCP

| | A2A | MCP |
|---|---|---|
| 连接 | agent ↔ **agent**（对等，各自有推理循环） | agent ↔ **工具 / 资源**（无自主循环） |
| 抽象 | 有状态的 `Task`、多轮、HITL | 无状态的函数调用 / 资源读取 |
| 典型用途 | 把任务委托给另一个 agent | 让模型调用一个工具 |

本 SDK 出向把远端 agent 当「**agent**」（子 agent / 主 agent）而不是「工具」，正是这一区别的直接后果（§3.3 / §6.1）。

### 0.7 版本与本文档约定

A2A 最新为 **1.0.0**（此前 0.3.0 / 0.2.6 / 0.1.0）。1.0 含**破坏性变更**：移除 `kind` 判别字段、状态枚举改为 `TASK_STATE_*`（0.3 为 `input-required` 这类小写 kebab）、card 的接口描述改为 `supportedInterfaces[]`（含 `protocolBinding` / `protocolVersion`）等。

本 SDK 以 `github.com/a2aproject/a2a-go/v2` 的实现为准（§3.1，已定 D4）。正文采用该库的方法名（`SendMessage` / `SendStreamingMessage` / `GetTask` / `ListTasks` / `CancelTask` / `SubscribeToTask`）；A2A 各版本（0.3 / 1.0）间的字段与枚举拼写差异（如 `TASK_STATE_*` vs `input-required`、`supportedInterfaces`）一律以该库实际绑定的版本为准。

---

## 1. 背景与三个前置判断

A2A 是一套让 Agent 之间跨进程/跨主机互相调用的协议。目标是双向支持：

- **出向（client）**：让 SDK 的 agent 能调用远端 A2A agent；
- **入向（server）**：让 SDK 的 agent 能被 A2A 客户端调用。

在动手前先确立三个判断，它们决定了后面所有设计取舍。

### 判断一：不要照搬 adk-go 的 `adka2a` / `remoteagent`

ADK 的前提是「无状态 agent + SessionService + `runner.Run` 可重入」，因此它用「input-required 结束本轮 → 下一条消息重跑」来换取**无进程内状态**。

go-agent-sdk 的前提完全不同：

- session 是 durable 的（SQLite，`session` / `session_message` 表）；
- 对外是**事件并集**（`agent.Event` + `iter.Seq2`），不是 task 状态机；
- 权限/提问会**在轮内阻塞挂起**（`core.Permission.Ask` / `core.Question.Ask` 阻塞在 channel 上）。

把 ADK 的实现平移到本 SDK，会丢掉 SDK 最有价值的一点（durable session + 可挂起的轮），因此**映射表与事件状态机必须重新设计**。

### 判断二：本 SDK 已有 `input-required` 的现成红利

`core/permission.go` / `core/question.go` 已经是如下形态：

| 已有能力 | 位置 |
|---|---|
| 发布 `permission.asked` / `permission.replied` | `schema/permission.go` + `permissionEvents.PublishAsked` |
| 发布 `question.asked` / `question.replied` / `question.rejected` | `schema/question.go` + `core/question.go` |
| 阻塞等待应答 | `core.Permission.pending` / `core.Question.pending`（`pendingRegistry`） |
| 按 requestID 回复 | `Permission.Reply` / `Question.Reply` / `Question.Reject` |
| 枚举 pending | `Permission.Get/ForSession/List`、`Question.List` |

这正是 A2A `input-required` + 后续 message 恢复所需的全部原语。ADK 需要自己造一套 long-running tool 协议，我们只需要做**桥接**。

### 判断三：真正要解决的是「统一抽象」，而不是出向/入向的先后

方向先后只是排期问题。真正决定架构成败的是两个问题（详见 §3.3 / §3.4）：

1. **远端 subagent 与本地 subagent 如何在设计上兼容、使用上一致？** → 答案是把差异落在「session 的执行后端」上 + 镜像子会话，而不是在 `task` 工具层分叉；
2. **入向时如何定义与暴露本地 subagent？** → 答案是显式声明**哪些 agent 对外提供服务**：暴露的 agent **各自提供一个 card URL**（一 card = 一 agent）。

这两个问题定完，出向与入向只是在同一套抽象上各写一个适配器，先后顺序无关紧要。

---

## 2. 目标与非目标

### 2.1 目标

1. **出向**：SDK 的 agent 能调用远端 A2A agent。唯一机制是「带 A2A 执行后端的 agent」，它经 `task` 工具表现为**子 agent**，并可被选为**主 agent**（§3.3 / §6.1）；不提供独立的「工具形态」；
2. **入向**：SDK 的 agent 能作为 A2A server 被调用，支持流式、多轮、`input-required`、取消、task 重查；
3. **保真**：part / tool-call / HITL 语义在往返中尽量不丢，支持 SDK↔SDK 与 **SDK↔ADK-go 跨实现互操作**；
4. **安全**：远端调用方**不能**隐式获得本机代码执行能力。

### 2.2 非目标

- 不做 opencode CLI 那套 launcher / 多形态入口（与 `AGENTS.md` 的 CLI 基础设施不照搬原则一致）；
- 不做 A2A registry / discovery 服务；
- 不引入 OTel / 遥测（SDK 当前没有 telemetry，不在本次引入）；
- 不自造 AgentCard 声明之外的**额外**能力协商机制（不做双向能力握手 / 降级协商）。仅按 AgentCard 的 `capabilities` / `defaultInputModes` / `defaultOutputModes` / `protocolVersion` 做最小适配：不认识的 non-required 扩展安全忽略，`required` 扩展不支持则拒绝（见 §6.2）；
- 不移植 opencode 的任何 v1/legacy 层代码。

---

## 3. 包结构与依赖策略

A2A 在 opencode TS 中**没有对应物**，属于 Go 专属能力，应像 `jsonschema/` 那样在 `AGENTS.md` 的包架构表中显式登记「无 TS 对应目录」。

```
a2a/                    协议无关核心：类型、映射、状态机（不依赖 transport）
a2a/server/             入向：executor + AgentCard 生成 + http.Handler
a2a/client/             出向：card 解析、消息发送、A2A 事件 → SDK 事件转换；定义薄接口
a2a/a2ago/              出向薄接口的 a2a-go 实现（唯一 import a2a-go 的出向适配层，可替换）
```

### 3.1 依赖策略

| 决策 | 内容 | 理由 |
|---|---|---|
| 依赖库 | **只支持 `github.com/a2aproject/a2a-go/v2`**（v2），单一版本；不兼容 `a2a-go` v0.x，也不同时引入两个版本 | adk-go 当前同时依赖 `a2a-go v0.3.15` 与 `a2a-go/v2 v2.2.1`，双版本共存对依赖图不友好，属于明确要避免的坑 |
| 类型隔离 | 对外 API 使用 SDK 自己的类型 + `iter.Seq2`，不泄漏 a2a-go 类型 | 对齐 `mcp/` 包装官方 SDK 的思路 |
| 适配层 | 薄接口定义在 `a2a/client`（模式参考 ADK 的 `internal/agent/remoteagent.A2AClient`），由 `a2a/a2ago/` 用 a2a-go 实现 | 协议仍在演进，升级只需改一处 |
| 逃生通道 | `a2a.WithNativeOptions(...)` 透传被打包库（a2a-go）的原生配置 | 不因封装而失去协议全部能力；名字不把 `a2a-go` 写进公开 API，契合「类型隔离」 |

> **依赖边界**：本 SDK **不依赖 adk-go**——adk-go 仅在 §1 / 附录 A 作为参照实现。`a2a/a2ago/` 是出向唯一 import `github.com/a2aproject/a2a-go/v2` 的适配层；a2a-go 类型不越过 `a2a/a2ago/` 与 `a2a/server/` 的边界进入 `a2a/` 核心与公开 API（见上「类型隔离」）。

→ **已定（D4，§10.2）：只支持 `a2a-go/v2` 单版本。**

### 3.2 与既有扩展机制的定位

A2A 能力应当**融入现有机制**，而不是另起一套：

| 既有机制 | A2A 如何接入 |
|---|---|
| `Client.On` / `Session.On` | 远端事件镜像进子会话后，`Client.On` 与子会话的 `Session.On` 可观察（父会话的 `Session.On` 看不到子会话，同本地 subagent） |
| `Client.RegisterAgent` / `AgentConfig`（及 `ConfigDirs` 的 agent 定义） | 远端 agent 的**主注册路径**：`AgentConfig` / `schema.AgentInfo` 增加 `A2A` backend 字段后，即可注册为命名 agent（进 `SubagentTypes`、可作主 agent） |
| `PluginFunc` | 插件经 `PluginContext.Agents`（`Transform`）贡献/改写 agent（含远端），用 `On` 观测远端往返；**不单设「远端 agent 工具」注册面** |
| `SessionHooks` | 只对**本地** `task` 调用（委派边界）生效：可在委派前拦截/审计「是否调用远端 agent」。远端 agent **内部**的工具调用不经本地 hooks，只能经 A2A 事件观测（降级为 `ToolProgress`，不伪装成 `ToolCalled`，见 §3.3 / §6.2） |
| `SessionConfig.Permissions` | **仅对入向**有效：外部调用触发的是**本机** agent，执行在本机发生，故按调用方身份注入的会话级规则集约束的是**本机工具**。出向远端 agent 的内部工具不受本地规则约束（§3.3） |
| `core/database` 建表 | 新增 core 一等 **`turn`** 实体（本地/远程 subagent 共用；A2A `taskId` 即 `turn.id`，见 §5.6）。直接改 `migration.go` 的建表清单即可 |
| `task` 工具 + `TaskRunner`（`task_run.go`） | 复用为出向**子 agent 形态**的**入口与本地编排**：`task` 工具、`runTask`、`task_id`、进度、深度、权限派生全部不变。真正的接入点是更下层的 `runner.SessionRunner` **drain 分派**（§3.3），因此 `task.go` / `task_run.go` 均不改；远端专属的 `a2a` 权限断言由 A2A backend 在该分派点追加（A7） |

> **执行侧边界（与 §3.3 一致）**：A2A 不提供控制对端工具/权限的原语（opaque execution）。**权限与 hooks 只对执行侧生效**：入向执行侧是本机 → 本地规则有效；出向执行侧是远端 → 本地只能控制「是否委派」与「发什么上下文」，远端内部工具调用**仅可观测、不可拦截**。

### 3.3 统一抽象：agent 的「执行后端」（backend）

**要回答的问题**：远端 subagent 与本地 subagent 如何在设计上兼容，且使用上完全一致？

**关键发现**：现成的接缝已经存在，**不需要新增 Agent 抽象层**。

`runtime.buildRunner` 把 coordinator 的 drain 注入为一个「按 session 调用」的函数：

```go
r.coord = session.NewCoordinator(func(ctx, id, force) error {
    publishSessionStatus(id, busy); defer publishSessionStatus(id, idle)
    return r.runner.Run(ctx, runner.RunInput{SessionID: id, Force: force})
})
```

而 `r.runner`（`*runner.LLMRunner`）满足 `runner.SessionRunner` 接口（`Run(ctx, RunInput) error`）；runner 又通过 `runner.AgentSelector` 接口的 `Select(sessionID, id)` **按 session 解析要运行的 agent**（runtime 侧的实现是 `sessionAgents`）。

也就是说：**「用哪个 agent」与「怎么跑一个 session」已经是两个正交的注入点。**

#### 设计

把 agent 的差异落在「session 的执行后端」上，**不要**在 `task` 工具或 `runTask` 里分叉：

1. `schema.AgentInfo` 增加可选 backend 字段（如 `A2A *AgentA2ABackend`），声明该 agent 由谁执行：本地 LLM 循环（默认，零值）或远端 A2A（endpoint / card / auth / 映射策略）；公开的 `AgentConfig` 同步增加该字段，由 `applyAgentConfig` 合并（`Client.RegisterAgent` 据此注册远端 agent）；
2. runtime 的 drain 改成分派：**先读 session 行拿到 `session.agent`，再经 `sessionAgents.Select(sessionID, &agentID)` 解析有效 agent**（与 runner 内部解析一致），据其 backend 分派——有远端 backend 走 `A2ARunner`，否则走 `LLMRunner`。两个 Runner 都实现 `runner.SessionRunner`；
3. **远端子 agent 仍然创建一个真实的本地子会话**（复用 `resolveChildSession`），远端事件**镜像写入**该子会话；该子会话的这轮执行记为一条 `turn`（`parent_turn_id` 指向父轮），与本地 subagent 完全同构（§5.6）。

这称为「**镜像子会话（mirrored child session）**」模型。用法一致性不是靠两套并行实现互相「对齐」，而是靠两种后端**共享同一套 session 语义**。

**澄清两个容易误解的点：**

- 镜像子会话**不是**「子会话里再跑一个本地 LLM，由它去调 A2A」。那样会多一层推理与 token 消耗，`task_id` 语义也会变。镜像子会话中**没有本地 LLM 参与**，子会话只是被 `A2ARunner` 驱动的 durable 账本 —— 对话实际发生在远端，本地子会话负责承载它的输入、事件与状态。
- 「建子会话」这件事本来就不由 `task` 工具负责，而是由 `runtime.runTask` 负责（`resolveSubagent` → `resolveChildSession` → `setSessionRegistry` / `inheritSession` / `publishTaskProgress` → `runChildSession`）。本设计只替换**最后一跳**：drain 的分派。因此 `task_id` 复用、存量会话的 `Agent` / `Permission` 恢复等逻辑全部沿用。

#### 两条路径的对照

```
task 工具
  → RunTask
    → resolveSubagent          // 查 registry：本地与远端 agent 都走这里
    → resolveChildSession      // 建 / 复用 session 行：两种 agent 都走这里
    → …（registry / hooks / progress，全部不变）
    → coord.Run(childID)
      → drain(childID, force)
        ├─ agent.Backend == nil   → LLMRunner.Run(childID)   // 本地：LLM 循环读写该 session
        └─ agent.Backend == A2A   → A2ARunner.Run(childID)   // 远端：读 pending input → 发 A2A
                                                             //       远端事件 → 镜像写入该 session
```

唯一新增的分支是最后一行；其余全部复用。

#### 不变量清单

远端 subagent 必须与本地 subagent 逐条一致，全部要进测试：

| 不变量 | 由什么保证 |
|---|---|
| `SubagentTypes()` 同时列出本地与远端 agent，排序与描述来源一致 | 不动 `SubagentTypes`，只让 agent 定义携带 backend |
| `task` 工具入参 / 出参 schema 不变 | 不动 `task.go` |
| `TaskOutput.SessionID` 都是本地子会话 ID | 镜像子会话（`resolveChildSession`） |
| `task_id` 续跑 | 本地子会话复用；远端 `taskId` 即该子会话活动 `turn` 的 id（§5.6） |
| `session.next.tool.progress` 字段一致 | 不动 `publishTaskProgress` |
| `SubagentDepth` 限制 | 不动 `checkSubagentDepth` |
| 权限派生（父规则 → 子会话） | 不动 `DeriveSubagentSessionPermission` |
| hooks 继承 / Abort 传播 | 不动 `inheritSession` / `coord.Interrupt`。**注意**：对 A2A 子会话，hooks **没有实际触发点**——远端内部工具不在本地执行（§3.2），这里保留的只是机制（本地/远程共用同一套 session 语义）；真正生效的是委派边界处对本地 `task` 调用的 hook 与 Abort→`CancelTask` 传播 |
| `Client.On` / `SessionMessages(childID)` 可观察远端对话 | 远端事件镜像落库 |
| 错误形态（统一为 `tool.Failure`） | 由 `A2ARunner` 内转换。注意 `task` 工具的错误路径**只**产出 `tool.Failure`，从不产出 `TaskOutput.State="error"`（`task.go` 仅在成功时置 `State="completed"`） |

#### 必须不同、且要写进用户文档的语义降级

| 项 | 说明 |
|---|---|
| 权限作用域 | 本地 permission 只覆盖「是否允许调用该远端 agent」「是否允许把本地上下文发出去」，**不覆盖远端内部的工具调用** |
| 路径语义 | 远端看不到本地文件系统 → 需要显式的上下文导出策略（§6.2 / A6），不能靠 prompt 里塞本地路径 |
| 取消 | 远端是协作式取消（A2A `CancelTask`），不保证立即停止 |
| usage / 计费 | 远端 usage 可能不可见 → `StepEnded` / `Usage` 事件可能缺失，UI 需容忍 |
| steering | A2A 没有等价语义，映射需单独定义 |

#### 顺带的净收益

同一个后端分派让「**远端 agent 当主 agent**」零成本可用（`CreateSession(Agent: "remote-x")` 即可）—— 这正是 ADK `remoteagent` 的用法，但在这里只是同一接缝的一个推论，不需要额外设计。

### 3.4 入向暴露：哪些 agent 能被外部调用

**要解决的问题**：入向当 A2A server 时，**哪些本地 agent 对外提供服务**（各自一张 card）。

**A2A 的模型**：一张 AgentCard 描述**一个** agent；要暴露多个 agent，就提供**多个 card URL**（各自一个端点）。card 的 `skills` 列这个 agent 能做的事——它的工具、能力、子 agent 都可以列，**列哪些、要不要列，都是定义 card 时的选择**。

**为什么需要显式声明**：不能靠任何「默认/可见性」之类的启发式去推断「哪些 agent 对外提供服务」。至少这些诉求它都表达不了：

1. 「可暴露但不可内部委派」；
2. 对外专用的描述 / 安全策略（脱敏）；
3. 外部身份 → 权限映射（card 的 `securitySchemes` / 入向授权）；
4. 明确**不可暴露**的项——靠启发式迟早会漏。

**方案**：`AgentConfig` / `AgentInfo` 增加显式暴露声明（如 `Expose` / `A2A` 子结构），决定某 agent 是否、以及以什么描述与安全要求**对外提供一张 card**；未声明的 agent 不提供 card。

#### 多个 agent = 多个 card URL

A2A 里一张 card 只描述**一个** agent。一个 server 要对外暴露 N 个 agent，就提供 **N 个 card URL**（各自一个端点，可在不同路径）。所以「入向暴露」= 决定本地哪些 agent 各自拥有一个 card/端点；客户端通过**选哪个 card/URL** 来选 agent。

每个被外部直接调用的 agent 跑在**根会话**（无父），权限派生与本地 `task` 委派不同：

| 维度 | 本地委派（task 工具） | 外部直接调用暴露 agent |
|---|---|---|
| 会话结构 | 有 `parent_id` 的子会话 | 无父的根会话 |
| 权限来源 | `DeriveSubagentSessionPermission(父规则, 子 agent)` | 该 agent 自身、由 server 定义的规则集（**无父可派生**）；多租户时再由调用方身份收敛 |
| 闸门 | `PermissionEvaluate(task, agentID, …)` | 暴露声明本身即闸门（可选：per-agent 的外部调用权限） |
| 深度限制 | 生效 | 不适用（无父），但要防止「外部 → 内部委派」链式放大 |
| 取消 | 父取消 → 子中断 | 由 A2A `cancel` 驱动 |

→ 见 §10.1 A3（暴露模型）/ A4（暴露 agent 的权限）。

---

## 4. 核心映射规格

> 本节是整个方案的地基，实现前必须先评审通过。

### 4.1 身份映射

| A2A | SDK | 说明 |
|---|---|---|
| `contextId` | `sessionID` | SDK session 本身 durable、可 resume，天然承载多轮；与 ADK 的选择一致 |
| `taskId` | `turn.id`（core 新增的一等 durable 实体，见 §5.6） | A2A task 有终态；SDK session 永续、一轮 turn 有终态，二者同构 |
| `messageId` | `session_message.id` | 便于 `GetTask` 与审计对齐 |
| 调用方身份（`CallContext.User`） | session 的 agent + `Permissions` 规则集 + 工作区 | 安全关键，见 §5.5 |
| task 处于 `input-required` | turn 的 `awaiting_input` → core 的 pending permission / question request | 见 §5.4 / §5.6 |

补充事实（已有数据模型支撑，无需新造概念）：

- session 行已带 `ProjectID`（`insertSessionRow`）；
- `schema.LocationInfo{Directory, WorkspaceID, Project}`、`schema.ProjectInfo{Worktree, Sandboxes, ...}` 已存在；
- `core/project/directories.go` 已支持一个 project 挂多个目录。

### 4.2 Part 映射

A2A 原生只有 **text / file / data** 三类 part，并不定义「工具调用 / 思考」的语义——这些靠 **data part + metadata** 表达。为与 ADK-go 互操作（目标 3），共享语义**直接采用 `a2a-go` / ADK 的既有约定**（`adk_` 前缀、`function_call` / `function_response` 类型）；SDK 专属语义用 `sdk_` 前缀。依据：`server/adka2a/v2/parts.go`。

| SDK 侧 | A2A 侧 | 备注 |
|---|---|---|
| `TextDelta` / `TextEnded` | Text part | 流式由 artifact 的 `append` / `lastChunk` 表达（§4.5） |
| `ReasoningDelta` / `ReasoningEnded` | Text part + meta `adk_thought=true` | 与 ADK 一致 |
| `ToolCalled` | Data part，meta `adk_type="function_call"`（长任务再加 `adk_is_long_running=true`） | **必须能在对端还原**，coding agent 可观测性命脉 |
| `ToolSuccess` / `ToolFailed` | Data part，meta `adk_type="function_response"` | 失败信息放进 response 载荷 |
| `ToolProgress` | Data part，meta `sdk_type="tool_progress"`；或仅发 `working` 心跳 | SDK 专属（§5.7） |
| `Usage` / `StepEnded` | artifact metadata `adk_usage_metadata` | 成本可观测 |
| `FileDiffInfo{File, Patch, Additions, Deletions, Status}` | Data part，meta `sdk_type="file_diff"` | SDK 专属；代码改动本身是可消费 artifact |
| 入向非文本 part（图片 / raw / 文件 URL） | File **URL part**（`NewFileURLPart`）或 **raw part**（`NewRawPart`） | coding agent 落盘为路径（§6.2） |
| 其他结构化 data | Data part；ADK 对无法识别的 data 会包成 `<a2a_datapart_json>…</a2a_datapart_json>` 的 text/plain | 保证往返不丢 |

### 4.3 Metadata 命名约定

A2A 没有标准 metadata 键空间。为互操作，**共享键沿用 ADK 的键名**；SDK 专属键用自有前缀。**禁止各处手写字符串**，统一走常量。依据：`server/adka2a/v2/metadata.go`。

| 方向 | 前缀 | 常量（对齐 ADK） |
|---|---|---|
| SDK → A2A（写进 A2A 对象 metadata） | `adk_`（共享）/ `sdk_`（SDK 专属） | `a2a.ToA2AMetaKey(key)`（ADK 同为 `adk_` 前缀） |
| A2A → SDK（写进 session event custom metadata） | `a2a:` | `a2a.ToSDKMetaKey(key)`（ADK 为 `a2a:`） |

- **共享键（对齐 ADK）**：`type`、`thought`、`partial`、`is_long_running`、`usage_metadata`、`error_code`、`session_id`、`invocation_id`、`author`、`branch`、`app_name`、`user_id`、`task_id`、`context_id`。
- **SDK 专属键**：`file_diff`、`tool_progress` 等（`sdk_` 前缀）。

### 4.4 事件 → task 状态机

| 时机 | 发出 |
|---|---|
| 无既有 turn（新 taskId） | 新建 `turn` + `Task{state=submitted}` |
| 开始 `Send` 之前 | `TaskStatusUpdateEvent{working}` |
| text / reasoning / tool delta | `TaskArtifactUpdateEvent{append=true, lastChunk=false}` |
| `permission.asked` / `question.asked` | `TaskStatusUpdateEvent{input-required}`；status message 里带 `adk_type=function_call` 的 data part（ADK 长任务约定，见 §4.7） |
| `Idle` | 补 `lastChunk=true` 封口 artifact → `completed` |
| `ErrorEvent` / `StepFailed` | `failed`（message 带错误文本） |
| A2A `cancel` | `Session.Abort()` → `canceled` |

### 4.5 Artifact 粒度

**建议只提供一种粒度**：per-turn 流式追加，turn 结束时发 `lastChunk=true`。

理由：ADK 的 `OutputArtifactPerRun` / `OutputArtifactPerEvent` 双模式是历史包袱（partial artifact 需要在结束时用空 data part「擦除」），对端聚合逻辑极易重复发布。本 SDK 的 delta 事件天然适合「追加 + 封口」一种模型。若后续确实需要第二种，只增加 per-message，**不要**引入 partial 擦除语义。

### 4.6 明确不保证的部分（语义降级清单）

任何「无损双向映射」的企图都会失败。以下为**显式降级**，必须写进用户文档：

- SDK 的 `steer` 与 A2A 的「向进行中 task 追加消息」不是同一语义 → 只在受控场景映射（见 §5.3）；
- SDK 的 `CompactionStarted/Ended` 没有 A2A 对应物 → 只出现在 metadata；
- SDK 的 `Retried` 无对应物 → metadata；
- SDK 的一次 turn 可能包含多次 LLM 调用（step），A2A 只有一个 task 生命周期 → 中间 step 边界降级为 metadata；
- A2A 的 `relatedTasks` / task 图能力本次不使用；
- **HITL 编码不同**：本 SDK 的 `input-required` 桥接的是 permission / question；ADK 用的是 **long-running function call**（`adk_is_long_running` + `function_call`/`function_response`，ADK 侧由 `HandleInputRequired` 按 function call ID 匹配）。跨实现时要按 §4.2 约定互转。

### 4.7 与 ADK-go 互操作清单（wire-level）

目标是 SDK↔ADK-go **双向**互操作（§2.1 目标 3）。以下线上约定必须对齐，均取自 `a2a-go/v2` + adka2a v2（`server/adka2a/v2/*`、`agent/remoteagent/v2/*`）：

1. **传输 / 发现**：`a2a-go/v2` + AgentCard 的 `supportedInterfaces`（JSON-RPC / gRPC / HTTP+JSON）；well-known `/.well-known/agent-card.json` 返回**单个** AgentCard。
2. **身份**：`contextId ↔ sessionID`（ADK 侧 `sessionID = ContextID`）。ADK 客户端把 `taskId` / `contextId` 记在 session event 的 custom metadata 里（键 `a2a:task_id` / `a2a:context_id`，`GetA2ATaskInfo` 读回）——我们作为客户端时也要这样做以续跑。
3. **Part**：§4.2（text + `adk_thought`；data + `adk_type=function_call/function_response`、`adk_is_long_running`；file → URL/raw）。未知 data 在 ADK 侧会退化为 `<a2a_datapart_json>` 内联文本，不会报错。
4. **Artifact**：用 `TaskArtifactUpdateEvent` 的 `append` / `lastChunk`。**不要**设 `adk_partial`（那是 ADK 的 legacy 模式：客户端会跳过自己的聚合，期望服务端已聚合）。ADK 客户端在非 legacy 下按 `ArtifactID` 聚合，服务端发 `lastChunk=true` 收口。
5. **状态**：`submitted → working →` 终态（`completed`/`failed`/`canceled`）；`input-required` 表达 HITL。**非终态的 status message 会被 ADK 客户端当成 thought（推理）显示**；`failed` 且仅一段文本时会被当作 error message。
6. **HITL**：ADK 用 **long-running function call** 做 HITL——`input-required` 的 status message 里放 `adk_type=function_call` 的 data part（`adk_is_long_running=true`）；客户端下一轮把用户应答作为 `function_response` 发回（带 `taskId` / `contextId`），服务端按 call ID 匹配（ADK: `HandleInputRequired`）。要与 ADK 客户端互通，我们承载 permission/question 时**必须用这套 function_call/response 编码**。
7. **客户端增量同步**：ADK 客户端只发「自上一个远端响应以来」的事件，并把本地其它 agent 的事件渲染成 user 文本（`presentAsUserMessage`），不重发全量；我们出向时同样只发增量。
8. **简单交互**：A2A 允许对简单请求直接返回 `Message`（无 `Task`）；ADK 客户端把 `Message` 视为 `TurnComplete=true` 的一轮。

---

## 5. 入向（Server）设计

### 5.0 职责边界：a2a-go 提供什么 / 我们实现什么

我们**基于 `a2a-go/v2/a2asrv` 组装，不重复造轮子**。协议与传输层已由它提供，我们只做「SDK ⇄ A2A」的适配：

| 能力 | 归属 | 挂载点 |
|---|---|---|
| JSON-RPC / REST(HTTP+JSON) 绑定、SSE 流、keep-alive、panic 处理 | **a2a-go**（继承） | `NewJSONRPCHandler` / `NewRESTHandler` / `WithTransportKeepAlive` |
| 请求管线：caps 校验、并发、inactivity、集群、拦截器、push、`GetTask`·`ListTasks`·`SubscribeToTask` | **a2a-go**（配置） | `NewHandler(executor, opts…)` + `WithTaskStore` / `WithPushNotifications` / … |
| 认证（`securitySchemes` / `CallContext`） | **a2a-go**（继承） | `a2asrv/auth.go` |
| AgentCard 类型 + well-known / 动态 card handler | **a2a-go**（继承） | `NewStaticAgentCardHandler` / `NewAgentCardHandler` |
| A2A 类型（Task / Message / Part / Artifact / Event） | **a2a-go** | |
| **Executor**（Execute / Cancel / Cleanup） | **我们实现** | 实现 `a2asrv.AgentExecutor`：把 session 事件流映射为 A2A 事件 |
| **TaskStore 持久化** | **a2a-go 定接口，我们实现** | 用 DB 实现 `a2asrv/taskstore.Store`（映射到 core `turn`，§5.6） |
| **Card 生成（agent → AgentCard）** | **我们** | `CardBuilder`（§5.1） |
| **类型 / 事件映射（§4）** | **我们** | 核心工作量 |
| **HITL 桥接、backend 分派、`turn`、暴露声明、授权策略** | **我们** | §5.4 / §3.3 / §5.6 / §3.4 / §5.5 |

本节之后只写「我们」的部分；表中标 **a2a-go** 的只说明「如何配置 / 实现其接口」，不再展开设计。

### 5.1 我们实现的组件

| 组件 | 职责 |
|---|---|
| `CardBuilder` | 为**每个显式暴露的 agent** 生成一张 AgentCard（**一 card = 一 agent**，各自 URL/端点）；其 `skills` 描述该 agent 的能力（条目可为工具 / subagent / 其它，默认不铺工具）；`capabilities` 据实声明（§3.4 / §6.2） |
| `Executor` | 实现 `a2asrv.AgentExecutor`（Execute / Cancel / Cleanup）：把 `session.Send` 的事件流映射为 A2A 事件（§4） |
| `TaskStore` | 实现 `a2asrv/taskstore.Store`（Create / Update / Get / List），映射到 core `turn`（§5.6） |
| `PendingRegistry` | 挂起的 turn 迭代器 + 待回复的 requestID |
| `WorkspaceResolver` | 调用方身份 → 工作区 / Root / 权限规则集 |
| `Handler()` | 返回 `http.Handler`：用标准库 `http.ServeMux` 组合 `a2a-go/v2/a2asrv` 的 `NewStaticAgentCardHandler`（well-known card）与 `NewJSONRPCHandler`（JSON-RPC 绑定）；多暴露 agent 时按各自 card URL/端点分别提供 |

### 5.2 接线方式与 HTTP 依赖

**返回 `http.Handler`，由宿主自己挂 mux。**

- **HTTP 依赖只用标准库 `net/http`**；协议绑定用 `a2a-go/v2/a2asrv` 提供的 handler（`NewStaticAgentCardHandler` / `NewJSONRPCHandler`，本身就是 `http.Handler`）。**不引入第三方 router / server 框架**（adk-go 的 `gorilla/mux` 只是它 launcher 的组合选择）。
- SDK **不自己启动 server、不绑定端口、不读 env**：宿主拿到 `http.Handler` 挂到自己的 `http.Server` 上；测试用 `httptest`。
- 不做 adk-go 那种 `launcher` 把 server 与启动方式强绑定。理由：符合 SDK 一贯的「窄输入、不读 env、不选默认位置」哲学（`Root` 必填、`ConfigDirs` 显式声明），也让嵌入既有 HTTP 服务变得简单。

### 5.3 一轮的生命周期与 message 语义

执行流程：

1. 解析 card / `taskId` / `contextId` → 决定「新 session」「resume session」还是「续一个 input-required 的 task」；
2. message parts → prompt + 附件落盘 + 路径注入；
3. `session.Send(ctx, MessageOptions{Prompt, Mode})` → 事件流 → A2A 事件流（§4.4）；
4. 终止：`Idle` → completed；`ErrorEvent` → failed；`asked` → input-required。

A2A 的消息语义比 SDK 丰富，需要明确映射：

| 到达的消息 | 映射 |
|---|---|
| 无 `taskId`，新 `contextId` | 新 session + 新 turn |
| 有 `taskId`，task 处于 `input-required` | 解析为 reply（`Permission.Reply` / `Question.Reply`），**续挂起的 turn** |
| 有 `taskId`，task 处于 `working` | `SessionDeliverySteer` |
| 有 `taskId`，task 已终态 | 拒绝（`UnsupportedOperationError`）→ **D3 已定** |

**并发模型**：SDK 一个 session 同时只能跑一轮（queue/steer）；A2A 同一 context 的 task 串行、不同 context 并发。`contextId → session` 的一一映射天然保证了串行性。

### 5.4 HITL：最关键的设计决策

远端用户应答权限/提问的架构其实**只有一个选择：挂起 + resume**。**与 ADK 客户端互通时，`input-required` 的载荷必须按 §4.7 编码为 `function_call`（应答为 `function_response`）**，否则 ADK 侧无法应答。

#### 设计：挂起 + resume

`asked` 事件到达时，A2A 层把这个 turn 的迭代器 **park 住**（`session.Send` 的 goroutine 本来就阻塞在 `Ask` 的 channel 上），发 `input-required` 并把 requestID 写进 turn 状态；下一条 A2A message 回来后调用 `Reply`，被阻塞的 goroutine 自然继续，原迭代器继续 yield。

它同时满足：

- **语义保真**：模型上下文、工具侧状态、已完成的 step 都不丢；
- **不重复调用 LLM、不重复计费**。

代价：挂起状态在**进程内**（受限于 sticky session）。

#### 唯一的降级路径：重建（不是并列选项）

当 parked turn 不可达时（进程崩溃/重启，或非 sticky 的水平扩展），进程内状态已丢，只能**降级重建**：turn 状态已落库（§5.6），重启后若发现活动 turn 处于 `awaiting_input`，就把「问题 + 待批准的工具调用」重新作为一轮继续。

- 代价：模型要重新推理；工具可能被重放（对 `bash` / `write` 危险）。

结论：**在线路径恒为「挂起 + resume」；「重建」只是 parked turn 不可达时的降级恢复**，不是与它并列、需要二选一的设计。ADK 只有后者（本轮结束、下轮重跑）；我们两者兼有，正是本次投入最值得的部分。**决策见 §10.2 D1（已定）。**

**参考实现（ADK）**：ADK 的整套入向就是这条「重建」路径——`Executor.Execute` 先调 `HandleInputRequired`（`server/adka2a/v2/input_required.go:152`）校验应答是否覆盖 status message 里全部 pending `function_call`，缺失则回 `input-required` + 校验错误，齐了则在 durable session 上重跑（`executor.go:197/215/238`）；ADK 无进程内 park。我们的 `turn.awaiting_input` + `pending_request_id` 等价于它的 `StoredTask`。

#### 配套的 SDK 侧改动

1. **已具备**：`agent.Event` 已携带 permission/question 事件——`PermissionAsked` / `PermissionReplied` / `QuestionAsked` / `QuestionReplied` / `QuestionRejected`，`events.go` 的 `mapEvents` 已映射，并经 `Session.Send` 与 `On` 透出。入向可直接感知 `asked`，无需再改；
2. **待做**：把「等待输入」表达在 **turn** 上（`awaiting_input`，§5.6），而非 session 上；session 的 busy/idle 由活动 turn 派生。挂起期间 session 仍是 busy 会阻塞同 session 的其他 `Send`，语义上需要由 turn 状态区分。

### 5.5 认证与授权

agent 能做什么（工具 / 子 agent / 技能）是**定义 agent 时定的**，card 的 `skills` 只是对外**描述**——协议不参与、调用方也改不了；本节只谈**由外部调用触发的那次 session 的信任边界**。

**认证（authentication）**：A2A 协议已定义，**直接继承 `a2a-go/v2/a2asrv`**，不自行决策——card 的 `securitySchemes` 声明方案，服务端据此校验调用方请求头；我们从 `CallContext.User` 取身份。

**授权（authorization）**：协议**明确留给实现**（spec §7.5：授权「based on the authenticated identity and its own policies」，是 implementation-specific，MAY 参考 requested skills / actions attempted / data access policies）。a2a-go 只给身份、不给策略，因此这部分仍要我们定：

| 规则 | 内容 |
|---|---|
| 暴露范围 | 调用只能落在**已暴露的 card/agent 集合**内（§3.4） |
| 权限策略 | 外部触发的 session 用**显式传入**的权限规则集（**可配置**：程序生成 / 配置文件 / 用户输入皆可），默认对危险工具 `deny`，**不用** client 级 `ApproveAll`；`ask` 走 §5.4 的 HITL（**已定，见 A4**） |
| 工作区隔离 | `WorkspaceResolver` 把 (调用方身份, contextId) 映射到工作区；复用既有 `Project` / `Directory` / `Location` / `Sandboxes` |
| 禁止隐式提权 | A2A metadata **不允许**覆盖 agent / model / permission |

### 5.6 Turn：A2A task 的持久化载体（core 一等实体）

`GetTask` / `ListTasks` / `SubscribeToTask` 的协议逻辑由 **a2a-go 的 handler 负责**；我们要做的是**实现 `a2asrv/taskstore.Store`**，把它落到 core 的 `turn`（+ 事件）。所以本节不设计「任务查询」，只设计「A2A task ↔ 我们的持久化」的映射。

**问题定位**：A2A `taskId` 对应「一轮 turn」，而 turn 在 SDK 里原本没有独立的 durable 行。本地 subagent 从不需要这样的记录，是因为它的 `task_id` 恰好**就是**子会话 id（`resolveChildSession` 直接 `store.Get(TaskID)`）——task 落在了一个已有主键上。远程把 **turn** 暴露成可寻址 id，才多出这份状态。

**方案 C（已定）**：把 `turn` 提升为 **core 的一等 durable 实体**，本地与远程 subagent 共用，而不是为 A2A 加一张协议专属的 `session_task` 表。

```text
turn
  id                 text PRIMARY KEY   -- 入向时即 A2A taskId
  session_id         text NOT NULL      -- 即 contextId
  parent_turn_id     text               -- 本地/远程 subagent 子轮 → 父轮
  input_id           text               -- 触发本轮的 session_input（锚点）
  state              text NOT NULL      -- running | awaiting_input | completed | failed | canceled | rejected
  pending_request_id text               -- awaiting_input 时的 permission/question request
  agent / model      text
  anchor_seq         integer            -- 起始事件序号（历史/artifact 边界）
  end_seq            integer            -- 终止事件序号（未终态为 NULL）
  metadata           text               -- 协议附加（远端 taskId / contextId 映射）
  time_created / time_updated integer
  索引：(session_id, time_created)、(state)
```

要点：

- **映射**：`contextId ↔ session_id`、`taskId ↔ turn.id`；
- **一套实体、两种用法**：本地 subagent 的每一轮、A2A 入向的每个 task、A2A 出向的镜像子轮，都落同一条 `turn`；`parent_turn_id` 表达委派层级，从而消除「本地不需要、远程需要」的不对称；
- **持久化**：`turn` 是 core 概念（`schema` + `core/database` 建表），由 coordinator 在轮开始 / 挂起 / 终止时维护；`GetTask`·`ListTasks`·`SubscribeToTask` 由 a2a-go handler 负责，我们实现 `a2asrv/taskstore.Store` 读写它（可作事件的投影，对齐既有 `session_message`）；
- **`input-required` 的落点**是 turn 的 `awaiting_input`（见 §5.4），不是 session 状态；
- **命名注意**：`task` 工具的 `task_id` 仍是子会话 id（≈ context），**不是** turn；A2A 的 `taskId` 才对应 `turn.id`。二者不可混用；
- **不引入协议专属的 `session_task`**：那既与 subagent `task` 工具撞名，又把「turn」这件事错误地记成「session 的 task」。

> **关于迁移**：SDK 尚未发布，**不做版本化迁移**。直接在 `core/database/migration.go` 增删/修改建表语句；本地与 examples 下的旧开发库（`.data`）删除重建即可。

→ **决策见 §10.1 A8（已定：方案 C）。**

### 5.7 长任务与心跳

SDK 的一轮可能跑很久（多 step 工具循环），客户端/网关会有超时。分三层，**尽量继承 `a2a-go`，不自造**：

1. **传输层 keep-alive（a2a-go 已有）**：`a2asrv.WithTransportKeepAlive(interval)` 打开 SSE 心跳——JSON-RPC（`jsonrpc.go`）与 HTTP+JSON（`rest.go`）的流式响应会起 `time.Ticker`，每 interval 调 `SSEWriter.WriteKeepAlive`，防止 API 网关断开空闲连接。**默认关闭（0）**。我们只需把宿主配置透传到这个 option（或走 `a2a.WithNativeOptions`），**不自己实现心跳**。
2. **语义层进度**：把 `StepStarted` / `ToolProgress` 映射为 `TaskStatusUpdateEvent{working}`，让客户端/应用看到「还在跑」。这是**可选**的进度表达，与传输层 keep-alive 是两回事。**Progress 只更新 status，不写 artifact**（不污染输出）。
3. **断连 / 超长任务（a2a-go 已有）**：`a2asrv.WithPushNotifications(store, sender)` 提供 webhook 推送（`a2asrv/push`，默认 in-memory store）；客户端可断连后由服务端推送 task 更新。本期不作为依赖（§2.2），但机制现成。

> 另有 `a2asrv/workqueue` 的 `Heartbeater`（`HeartbeatInterval` / `Heartbeat`）——那是**集群模式下 work item 租约**的心跳，与客户端 keep-alive 无关，仅做分布式 executor 时相关。

---

## 6. 出向（Client）设计

### 6.0 职责边界：a2aclient 提供什么 / 我们实现什么

出向**基于 `a2a-go/v2/a2aclient`**，同样不重复造轮子：

| 能力 | 归属 | 挂载点 |
|---|---|---|
| card 解析 / 传输选择（JSON-RPC / REST）/ 凭据 | **a2a-go**（继承） | `NewFromCard` / `NewFromEndpoints` / `Factory` + `auth` |
| 协议操作：`SendMessage` / `SendStreamingMessage` / `GetTask` / `ListTasks` / `CancelTask` / `SubscribeToTask` / push config / `GetExtendedAgentCard` | **a2a-go**（继承） | `a2aclient.Client` 的方法 |
| 拦截器 / 日志 | **a2a-go**（配置） | `CallInterceptor` |
| **SDK 事件 ⇄ A2A 事件转换** | **我们** | 核心工作量（§4） |
| **backend 集成**（`A2ARunner`、镜像子会话、`turn`） | **我们** | §3.3 / §6.1 |
| **上下文导出、HITL 桥接、取消策略** | **我们** | §6.2 |

### 6.1 唯一机制：带 A2A backend 的 agent

出向**不设独立的接入形态**。远端 A2A agent 在语义上就是一个 agent（有 `contextId`/多轮/HITL/流式事件），因此与本地 agent 走同一机制：`schema.AgentInfo` 携带 `backend = A2A`，由 runner 按 backend 分派（§3.3）。它自然获得两种表象——**默认（自然）用法是子 agent**，**把它当主 agent 是扩展用法**：

| 表象 | 如何产生 | 成本 |
|---|---|---|
| **子 agent** | 出现在 `task` 的 `subagent_type` 列表（`SubagentTypes` 按 `Mode != primary` 过滤），由 `task` 委派 | 零改动：`task.go` / `runTask` / `SubagentTypes` 全不动 |
| **主 agent** | `CreateSession(Agent: <远端 agent id>)` | 零成本：backend 是 agent 的属性 |

实现要点见 §3.3：**不要**在 `runTask` 里分叉，而是在 coordinator 的 drain 上按 agent 的 backend 分派，让远端 agent 拥有真实的镜像子会话。直接收益是 `task` 工具、`SubagentTypes`、进度事件、深度限制、权限派生、hooks 继承全部零改动。

**为什么不做「工具形态」**（曾经的 `a2a.NewRemoteTool`，已废弃）：

| 维度 | Tool | 远端 agent |
|---|---|---|
| 多轮 / 上下文 | 无（每次独立） | `contextId ↔ sessionID` 原生 |
| HITL（`input-required`） | 无，只能退化为失败或提示 | 与 `permission.asked` / `question.asked` 同构，可 park/resume |
| 流式保真 | 只有 `ToolContext.Progress` 一个粗通道 | 完整事件流镜像进子会话 |
| `task_id` 续跑 | 无 | 天然复用 |
| 深度 / 权限派生 / hooks / abort | 全部另造 | 全部复用 |
| 实现 | 与 `A2ARunner` 重复一套 card/协议/转换逻辑 | 单一 backend，`a2a/client` 作为内部依赖 |

把有状态的对等 agent 循环压进同步函数调用，必然丢失多轮、HITL 与事件保真度，并形成一条默认就会被选中的**有损路径**，与 §3.3「用法一致」的目标相悖。唯一「像 tool」的窄场景（纯无状态单问单答）用**单轮子会话**表达即可，无需单列形态。

→ **A1 / A2 / A7 均已定（§10.1）。**

### 6.2 映射细节

**发什么**（= A6）

- 委派（子 agent 形态）：发 `task` 的 prompt；主 agent 形态：发首条 message；
- 需要文件 / 结构化输入时**显式作为 part 带上**（text / data / file），**绝不**隐式把本地路径塞进文本；远端 file part 由它的入向适配落盘（§4.2）；
- 续跑同一 task 时带 `taskId` / `contextId`（§4.7）；
- SDK 有 durable（镜像）子会话，能精确知道已发过什么，只发**新增**输入；不必像 ADK 那样回推「远端缺哪些事件」（§4.7）。

**收回什么**

- 远端 artifact → `TextDelta`（保证 UI 实时）；
- 远端 tool call data part → 降级为 `ToolProgress` 或结构化日志，**不要伪装成本地 `ToolCalled`**（否则本地权限 / 审计 / hooks 会把远端工具调用误判成自己的）；
- `thought` metadata → `ReasoningDelta`。

**远端 input-required → 本地**

- 默认：映射为本地 `QuestionRequest`（走既有 `OnQuestion` 通路），挂起的 turn park 住，用户应答后经 `Reply` 续发（镜像子会话 §3.3 + 挂起 §5.4）；
- 若远端在纯单向委派中提出而本地无人应答：退化为一次失败 + 提示文本，让本地模型自己决定是否用 `question` 工具问用户；
- **与 ADK 服务端互通**：远端若用 long-running `function_call` 表达 input-required，本地应答必须以 `function_response`（带 `taskId` / `contextId`）回发（§4.7）。

→ **D2 已定。**

**取消与清理**

- 本地 `Session.Abort()` / ctx 取消 → 远端 `CancelTask`。ADK 的 `cleanupRemoteTask` 有完整参考：仅在「最后一个事件非终态」且不是 input-required 时取消，超时 5 秒；
- 本地 `Session.Close()` → 检查并收敛所有未终态的远端 task；
- 远端 task 处于 `input-required` 且本地正常结束 → 不取消（保持可续跑）。

**Card 解析的二次利用**

拿到 card 后除了连客户端，还要榨出信息。这与 §2.2 的「最小能力适配」是一体的：A2A 的能力是**声明 + 发现**（服务端在 AgentCard 单方面声明，客户端读取后适配），没有双向协商回环。两端都应按此最小集实现。

| Card 字段 | 用途 |
|---|---|
| `description` | 子 agent / 主 agent 的描述 |
| `skills` | 远端 agent 的能力描述；**不据此拆出多个子 agent**（一 card = 一 agent，§3.4） |
| `capabilities.streaming` | 远端是否支持流式；据此选 `SendStreamingMessage`，不支持则退化为 `SendMessage`（见下方注） |
| `defaultInputModes/OutputModes` | 决定是否需要附件落盘 / 文本兜底 |
| `capabilities.pushNotifications` | 是否可配置推送（本期不作为依赖） |
| `capabilities.extensions[]` | `required=true` 且不支持 → 拒绝；否则安全忽略 |
| `protocolVersion` | 兼容判断；跨实现互操作（SDK↔ADK-go）靠它 |
| `securitySchemes` / `securityRequirements` | 从中选用本 SDK 支持的认证方案 |

> **传输方式的选择（与 ADK v2 的有意偏离）**：ADK v2 由**本地运行模式**（`StreamingMode`）决定发 `SendMessage` 还是 `SendStreamingMessage`，**不读取 card 的 `capabilities.streaming`**（`agent/remoteagent/v2/a2a_agent.go:292`；其测试 card 虽声明 `Streaming: true`，分支只看本地模式）。本 SDK 没有等价的本地模式开关，改为**以 card 能力为准**：声明支持流式则用 `SendStreamingMessage`，否则退化为 `SendMessage`；若本地请求流式而服务端声明不支持，按 A2A 的 `UnsupportedOperationError` 处理（§3.3.4 Capability Validation）。这是有意选择，不是照搬 ADK。

服务端（入向）同样必须发布**准确**的 `capabilities`：支持流式则 `streaming=true`，未实现推送则 `pushNotifications=false`，并给出 `defaultInputModes/OutputModes` 与 `protocolVersion`（§5.1 `CardBuilder`）。

---

## 7. Phase 0：需要先动的 SDK 侧前置改造

A2A 落地前必须先做的 **SDK 侧改造**（`turn`、backend 分派偏通用；暴露标记是 A2A 特有）：

| 改动 | 位置 | 为什么必须 |
|---|---|---|
| core 一等 `turn` 实体 + 生命周期记录（含 `awaiting_input`） | `schema` / `core/database` 建表 / coordinator | 本地/远程 subagent 共用；A2A `taskId` = `turn.id`，支撑 `taskstore.Store`（供 a2a-go 的 `GetTask`）与重启恢复；session busy/idle 由活动 turn 派生 |
| agent **backend** 字段 + drain 分派点 | `schema.AgentInfo` / `client.go`（`AgentConfig`）/ `runtime.go`（`buildRunner`） | 出向（子 agent 形态）的前提；**复用现有 `runner.SessionRunner`，不新增 `AgentRunner` 抽象**；`task.go` / `task_run.go` 不改（§3.3） |
| 「是否可被 A2A 调用」标记与 AgentCard 生成 | `CardBuilder` + agent 元信息（`Expose`） | 决定哪些 agent 各提供一个 card URL；避免把内部 agent 暴露出去（§3.4） |

---

## 8. 路线图与验收

| Phase | 内容 | 验收 |
|---|---|---|
| 0 | §7 的三项前置改造（permission/question 事件映射已就绪，不在其中） | 现有测试全绿；新增 turn 状态测试 |
| 1 | `a2a/` 类型 + `a2a/client`（card 解析、`SendMessage` / `SendStreamingMessage`、取消）+ `A2ARunner` + drain 分派（子 agent 形态） | 远端 agent 能经 `task` 被本地模型委派；超时 / 取消 / 错误路径全测 |
| 2 | `a2a/server` 入向 MVP（`Executor` + `taskstore.Store` + card + `http.Handler`，输入输出全文本） | SDK↔SDK 端到端：A2A 客户端发消息拿到 completed task + artifact |
| 3 | HITL 闭环（挂起 + resume）、多轮 context、重启恢复（降级重建，§5.4） | permission / question 往返测试；重启后降级恢复测试 |
| 4 | part 级保真（tool trace、thought、usage、file diff） | **与 ADK-go 对打**的互操作测试（跨实现验证协议保真度） |
| 5 | 生产化：`WorkspaceResolver`、认证（配置 a2a-go）、keep-alive（配置 a2a-go）、限流（配置 a2a-go 的 limiter）、artifact 外置钩子 | 多租户隔离测试；`go test -race` 全绿 |

**测试矩阵**参考 adk-go 的 A2A 测试（`a2a_e2e_test.go` + `*_compat_test.go` 场景命名），覆盖：

`input-required`、`multi-hop input-required`、`input-required cancellation`、`cleanup propagation`、`single-hop final response`、`streaming success`、`streaming error`、`session reuse`、`callbacks`、`converters`、`custom part converter`、`error on card resolution`、`error on server error`。

---

## 9. 风险

**无架构级风险。** A2A 是标准协议，互操作由协议正确性保证；安全由 §5.5 消解；语义损失是 §4.6 的已知降级；文档更新是 TODO；`turn` 一等化是已定成本（A8）。

唯一要靠测试兜底的是**并发正确性**（挂起 + 落库 + 事件分发）——这属于**实现质量**问题（任何并发代码都存在），不是设计风险：用 `go test -race` + `testing/synctest` 覆盖，Phase 0 先建 turn 状态模型，恢复路径参考 ADK `HandleInputRequired`。

其余项不是风险（说明，避免过度担忧）：

- **互操作**：由协议正确性保证。§4.2 采用 `adk_*` 约定只是**保真增强**（让 tool trace / thought 在对端可还原），不在协议内；即便漂移，未知 data part 会被 ADK 优雅降级为文本，**不影响核心互操作**。
- **安全**（远端触发本机代码执行）：由 §5.5 消解——认证继承 a2a-go、授权默认对危险工具 `deny`、工作区隔离、禁止 metadata 提权。是**配置面**。
- **语义损失**：§4.6 的**已知降级**。
- **文档一致性 / `DESIGN.md` 清单更新**：落地 **TODO**。
- **`turn` 一等化**：已定方案 C 的**成本**（A8）。

---

## 10. 决策清单（已全部收敛）

### 10.1 架构级决策（A 系列）

| ID | 问题 | 决策 |
|---|---|---|
| A1 | 远端 agent 的实现位 | 落在「session 执行后端」——新增 `A2ARunner` 实现 `runner.SessionRunner`，coordinator drain 按 agent backend 分派；**不**在 `runTask` / `task` 工具层分叉（§3.3）；**出向不设独立「工具形态」**（§6.1） |
| A2 | 远端子 agent 是否创建本地镜像子会话 | 是。一致性来自「两种后端共享同一套 session 语义」，而非两套实现互相对齐（§3.3） |
| A3 | 暴露模型 | 显式 expose 声明决定哪些 agent 各自提供一个 card URL（**一 card = 一 agent**）（§3.4） |
| A4 | 被外部直接调用的暴露 agent 的权限 | 用该 agent 自身、由 server 定义的规则集（无父可派生）；多租户时按调用方身份收敛。**权限只需「可配置」**——程序生成 / 配置文件 / 用户输入皆可，SDK 不关心来源；**默认对危险工具 `deny`**，宿主显式放宽。`DeriveSubagentSessionPermission` 不适用（§3.4 / §5.5） |
| A5 | 远端 usage / model 在事件与 progress 中如何表达 | `publishTaskProgress` 的 `model` 允许为空或取 card 声明的值；`Usage` / `StepEnded` 可缺失，UI 需容忍 |
| A6 | 本地上下文如何导出给远端 | **调用方显式给定**——`task` 的 prompt / 首条 message，文件作为 part 显式带上；我们转成 A2A text/data/file part，**绝不**隐式塞本地路径。远端 file part 由**它的**入向适配落盘（§4.2）；增量沿用 ADK `toMissingRemoteSessionParts` 思路（只发上次远端响应后的事件） |
| A7 | 远端调用的权限 action | **复用 `task` action**（子 agent 形态与本地一致；resource = 远端 agent id，可精细控制）。主 agent 形态不经 `task`，由宿主建会话负责（§3.3 / §6.1） |
| A8 | A2A task 的映射与持久化 | **方案 C**：`contextId ↔ sessionID`、`taskId ↔ turn.id`；`turn` 升为 **core 一等 durable 实体**，本地/远程 subagent 共用，不设协议专属 `session_task` 表（§4.1 / §5.6） |

### 10.2 实现级决策（D 系列）

| ID | 问题 | 决策 |
|---|---|---|
| D1 | HITL 路线 | 唯一设计 = 挂起 + resume（在线）；parked turn 不可达（重启 / 非 sticky）时降级为重建一轮——不是二选一（§5.4） |
| D2 | 远端 input-required 如何本地化 | 映射为本地 `QuestionRequest`（走既有 `OnQuestion`，park/resume）；纯单向委派无人应答时退化为失败 + 提示；与 ADK 互通时按 §4.7 回 `function_response`。**不**映射为本地 `permission`（本地权限不覆盖远端内部调用） |
| D3 | 已终态 task 收到新消息 | 拒绝——A2A spec 规定终态 task 不再接受消息（`UnsupportedOperationError`）（§5.3） |
| D4 | 依赖策略 | 只支持 `github.com/a2aproject/a2a-go/v2` 单版本（不兼容 v0.x），经 `a2a/a2ago/` 薄适配层隔离 |
| D5 | artifact 粒度 | 只做 per-turn 流式追加 + `lastChunk`，放弃 ADK 双模式（§4.5） |

---

## 附录 A：与 adk-go `adka2a` 的对照

| 维度 | adk-go | go-agent-sdk（本文） |
|---|---|---|
| session 语义 | 无状态 runner + `SessionService`，`contextId = sessionID` | durable SQLite session，`contextId = sessionID` |
| `input-required` | 专门造 long-running tool 协议；本轮结束，下轮重跑 | 直接桥接既有 `permission.asked` / `question.asked` + `Reply` |
| 挂起能力 | 无（只有重放） | 有（轮内阻塞 + park/resume） |
| artifact 粒度 | 双模式（per-run / per-event），partial 需擦除 | 单一 per-turn 追加 + 封口 |
| 出向形态 | 只有 `remoteagent.NewA2A`（一等 agent） | 同为「带 A2A backend 的 agent」，经 `task` 表现为子 agent，并可作主 agent（无独立 tool 形态） |
| 流式选择 | 由本地 `StreamingMode` 决定，不读 card `capabilities.streaming`（`a2a_agent.go:292`） | 以 card `capabilities.streaming` 为准，不支持则退化为非流式（§6.2） |
| 版本策略 | v0.3.15 + v2.2.1 双版本共存 | 只引 v2 单版本 |
| HITL 编码 | long-running `function_call` / `function_response`（`adk_is_long_running`） | 桥接 `permission.asked` / `question.asked`；对 ADK 客户端按 §4.7 用同款 `function_call` 编码 |
| task 持久化 | a2a task 存 a2a-go 的 task store，session 存 `SessionService` | core 一等 `turn`（`taskId = turn.id`），实现 a2a-go 的 `taskstore.Store`（A8） |
| server 接线 | 由 launcher 提供 A2A server（`adka2a` Executor） | 返回 `http.Handler`，宿主自挂 |
| 安全模型 | `userID` 来自 `CallContext`，未强调工作区隔离 | 权限规则集 + 工作区隔离 + 默认不信任 |

可复用的思路（值得参考但不照抄）：part 类型标记的 data part 编码方式、`cleanupRemoteTask` 的取消判定、card skills 的生成方式（ADK 把工具 / 子 agent 都列为 skill，协议上成立；是否启用是策略，见 §3.4）、e2e 测试矩阵。
