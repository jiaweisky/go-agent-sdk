# go-agent-sdk A2A 支持设计文档

> 状态：**草案，待评审**
> 范围：为 go-agent-sdk 增加 A2A（Agent2Agent）协议支持（入向 + 出向）
> 关系：本文是 `docs/DESIGN.md` 的分册，只覆盖 A2A 能力；通用架构、公开 API、移植规则仍以 `docs/DESIGN.md` 与 `AGENTS.md` 为准
> 约定：本文中的 Go 代码片段**仅为示意**，用于表达类型边界与契约，不代表最终签名

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-09-26 | 初稿：映射规格、入向/出向设计、路线图、待决策清单 |

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
2. **入向时如何定义与暴露本地 subagent？** → 答案是把「可暴露」从 `Mode`/`Hidden` 的启发式中独立出来、显式声明，并区分「间接触达」与「直接路由」两个语义层级。

这两个问题定完，出向与入向只是在同一套抽象上各写一个适配器，先后顺序无关紧要。

---

## 2. 目标与非目标

### 2.1 目标

1. **出向**：SDK 的 agent 能以「工具形态」和「子 agent 形态」调用远端 A2A agent；
2. **入向**：SDK 的 agent 能作为 A2A server 被调用，支持流式、多轮、`input-required`、取消、task 重查；
3. **保真**：part / tool-call / HITL 语义在往返中尽量不丢，支持 SDK↔SDK 与 **SDK↔ADK-go 跨实现互操作**；
4. **安全**：远端调用方**不能**隐式获得本机代码执行能力。

### 2.2 非目标

- 不做 opencode CLI 那套 launcher / 多形态入口（与 `AGENTS.md` 的 CLI 基础设施不照搬原则一致）；
- 不做 A2A registry / discovery 服务；
- 不引入 OTel / 遥测（SDK 当前没有 telemetry，不在本次引入）；
- 不做跨厂商 agent 能力协商（capability negotiation 只实现协议最小集）；
- 不移植 opencode 的任何 v1/legacy 层代码。

---

## 3. 包结构与依赖策略

A2A 在 opencode TS 中**没有对应物**，属于 Go 专属能力，应像 `jsonschema/` 那样在 `AGENTS.md` 的包架构表中显式登记「无 TS 对应目录」。

```
a2a/                    协议无关核心：类型、映射、状态机（不依赖 transport）
a2a/server/             入向：executor + card/skills 生成 + http.Handler
a2a/client/             出向：card 解析、消息发送、A2A 事件 → SDK 事件转换
a2a/sdkclient/          可替换的 a2a-go 适配层（默认实现）
```

### 3.1 依赖策略

| 决策 | 内容 | 理由 |
|---|---|---|
| 依赖库 | 只引 `github.com/a2aproject/a2a-go/v2`，**单一版本** | adk-go 当前同时依赖 `a2a-go v0.3.15` 与 `a2a-go/v2 v2.2.1`，双版本共存对依赖图不友好，属于明确要避免的坑 |
| 类型隔离 | 对外 API 使用 SDK 自己的类型 + `iter.Seq2`，不泄漏 a2a-go 类型 | 对齐 `mcp/` 包装官方 SDK 的思路 |
| 适配层 | `a2a/sdkclient/` 定义薄接口（参考 ADK 的 `internal/agent/remoteagent.A2AClient`） | 协议仍在演进，升级只需改一处 |
| 逃生通道 | `a2a.WithSDKOptions(...)` 之类的选项透传原生配置 | 不因封装而失去协议全部能力 |

### 3.2 与既有扩展机制的定位

A2A 能力应当**融入现有机制**，而不是另起一套：

| 既有机制 | A2A 如何接入 |
|---|---|
| `Client.On` / `Session.On` | A2A 接入后产生的事件天然可通过它们观察（因为就是 session 事件） |
| `PluginEffect` | 插件可用 `PluginContext.Tools` 注册远端 agent 工具；用 `On` 观测远端往返 |
| `SessionHooks` | Pre/PostToolUse 对「远端 agent 工具」同样生效（权限与审计一致） |
| `SessionConfig.Permissions` | 入向按调用方身份注入会话级规则集 |
| `core/database` migrations | 新增 `session_task` 表（见 §5.6） |
| `task` 工具 + `TaskRunner` | 出向子 agent 形态的宿主接口；**注意**：接入点在更下层（§3.3），`task.go` 不需要改动 |

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

而 `r.runner`（`*runner.LLMRunner`）满足 `runner.SessionRunner` 接口（`Run(ctx, RunInput) error`）；runner 又通过 `runner.AgentSelector{Select(sessionID, id)}` **按 session 解析要运行的 agent**（runtime 侧的实现在 `sessionAgents`）。

也就是说：**「用哪个 agent」与「怎么跑一个 session」已经是两个正交的注入点。**

#### 设计

把 agent 的差异落在「session 的执行后端」上，**不要**在 `task` 工具或 `runTask` 里分叉：

1. `schema.AgentInfo` 增加可选 backend 字段（如 `A2A *AgentA2ABackend`），声明该 agent 由谁执行：本地 LLM 循环（默认，零值）或远端 A2A（endpoint / card / auth / 映射策略）；
2. runtime 的 drain 改成分派：查 session 的有效 agent（复用 `sessionAgents`）→ 有远端 backend 走 `A2ARunner`，否则走 `LLMRunner`。两个 Runner 都实现 `runner.SessionRunner`；
3. **远端子 agent 仍然创建一个真实的本地子会话**（复用 `resolveChildSession`），远端事件**镜像写入**该子会话。

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
| `task_id` 续跑 | 本地子会话复用 + 远端 `taskId` 存在子会话的 backend state 中 |
| `session.next.tool.progress` 字段一致 | 不动 `publishTaskProgress` |
| `SubagentDepth` 限制 | 不动 `checkSubagentDepth` |
| 权限派生（父规则 → 子会话） | 不动 `DeriveSubagentSessionPermission` |
| hooks 继承 / Abort 传播 | 不动 `inheritSession` / `coord.Interrupt` |
| `Client.On` / `SessionMessages(childID)` 可观察远端对话 | 远端事件镜像落库 |
| 错误形态（`tool.Failure` / `TaskOutput.State="error"`） | 统一在 `A2ARunner` 内转换 |

#### 必须不同、且要写进用户文档的语义降级

| 项 | 说明 |
|---|---|
| 权限作用域 | 本地 permission 只覆盖「是否允许调用该远端 agent」「是否允许把本地上下文发出去」，**不覆盖远端内部的工具调用** |
| 路径语义 | 远端看不到本地文件系统 → 需要显式的上下文导出策略（§6.4），不能靠 prompt 里塞本地路径 |
| 取消 | 远端是协作式取消（A2A `CancelTask`），不保证立即停止 |
| usage / 计费 | 远端 usage 可能不可见 → `StepEnded` / `Usage` 事件可能缺失，UI 需容忍 |
| steering | A2A 没有等价语义，映射需单独定义 |

#### 顺带的净收益

同一个后端分派让「**远端 agent 当主 agent**」零成本可用（`CreateSession(Agent: "remote-x")` 即可）—— 这正是 ADK `remoteagent` 的用法，但在这里只是同一接缝的一个推论，不需要额外设计。

### 3.4 三个正交概念：默认 agent / 可委派 / 可暴露

A2A 的引入暴露了当前设计的一处局限：`Mode` + `Hidden` 两个字段同时承担了三个正交语义。

| 概念 | 现在的表达 | 用途 |
|---|---|---|
| (a) 能否作为默认 agent | `agentSelectable`：`Mode != subagent && !Hidden` | `Client.Default()` / `CreateSession` |
| (b) 能否被内部委派 | `SubagentTypes`：`Mode != primary` 且 `task` 权限不为 deny | `task` 工具 |
| (c) **能否被外部调用（A2A 暴露）** | **无显式表达**（目前只能靠 (a) 推断） | A2A card skills |

建议把 (c) 显式化，而不是继续用启发式推断。理由：

1. 无法表达「可被外部调用但不可被内部委派」（例如专供外部客户的 agent）；
2. 无法为外部调用提供**不同的描述与输入输出模式**（对外描述常常需要脱敏）；
3. 无法配置**外部身份的权限映射**（安全关键）；
4. 有些 agent 明确不该被暴露（内部编排 agent），靠启发式迟早会漏。

形式上：`AgentConfig` / `AgentInfo` 增加显式的暴露声明（如 `Expose` 或 `A2A` 子结构）；**入向只广告声明过的 agent**。

补充建议：**不要把本地工具广告成 skill**。ADK 的 `BuildAgentSkills` 会把每个 tool 变成一个 skill，对 coding agent 而言既吵闹又泄密（等于把 `bash` / `edit` / `write` 的能力清单告诉外部）。card 的 skills 应当只来自显式暴露的 agent。

#### 暴露的两个语义层级

远端「使用子 agent」有两条截然不同的路径，必须区分。

**层级 A：间接触达（默认）** —— card 只广告主 agent；远端若要用子 agent，由主 agent 的模型自己调 `task` 工具委派。语义与 opencode 完全一致，权限复用 `DeriveSubagentSessionPermission`。代价：远端无法确定性地选择 agent。

**层级 B：直接路由（可选，需显式声明）** —— 声明了暴露的 agent 直接作为 card skill 广告（skill id = agent id），请求带 skillId / metadata → server **以该 agent 为根**建 session。

层级 B **不是**「远端父 agent 调子 agent」，语义差异必须写清：

| 维度 | 本地委派（task 工具） | A2A 直接路由（层级 B） |
|---|---|---|
| 会话结构 | 有 `parent_id` 的子会话 | 无父的根会话（或挂在 contextId 对应的会话上） |
| 权限来源 | `DeriveSubagentSessionPermission(父规则, 子 agent)` | **需新定义**：调用方身份规则 ∩ agent 自身规则 ∩ 暴露策略 |
| 闸门 | `PermissionEvaluate(task, agentID, ...)` | 暴露声明本身即闸门（可选：per-agent 的外部调用权限） |
| 深度限制 | 生效 | 不适用（无父），但要防止「外部 → 内部委派」链式放大 |
| 取消 | 父取消 → 子中断 | 由 A2A `cancel` 驱动 |

结论：层级 B 的代价是引入一套**独立的入向权限派生（inbound permission derivation）**。D7 / D9 其实是这个问题的子问题，应合并进来一起定（见 §10.1 A3 / A4）。

---

## 4. 核心映射规格

> 本节是整个方案的地基，实现前必须先评审通过。

### 4.1 身份映射

| A2A | SDK | 说明 |
|---|---|---|
| `contextId` | `sessionID` | SDK session 本身 durable、可 resume，天然承载多轮；与 ADK 的选择一致 |
| `taskId` | **一轮 turn**（比 session 细一级） | A2A task 有终态，SDK session 是永续的 |
| `messageId` | `session_message.id` | 便于 `tasks/get` 与审计对齐 |
| 调用方身份（`CallContext.User`） | session 的 agent + `Permissions` 规则集 + 工作区 | 安全关键，见 §5.5 |
| task 处于 `input-required` | core 的 pending permission / question request | 见 §5.4 |

补充事实（已有数据模型支撑，无需新造概念）：

- session 行已带 `ProjectID`（`insertSessionRow`）；
- `schema.LocationInfo{Directory, WorkspaceID, Project}`、`schema.ProjectInfo{Worktree, Sandboxes, ...}` 已存在；
- `core/project/directories.go` 已支持一个 project 挂多个目录。

### 4.2 Part 映射

| SDK 侧 | A2A 侧 | 备注 |
|---|---|---|
| `TextDelta` / `TextEnded` | Text part | 流式 → `append=true` |
| `ReasoningDelta` / `ReasoningEnded` | Text part + `sdk.thought=true` | 对齐 ADK 的 `adk_thought` |
| `ToolCalled` | Data part + `sdk.type=tool_call` | **必须能在对端还原**，是 coding agent 可观测性的命脉 |
| `ToolSuccess` / `ToolFailed` | Data part + `sdk.type=tool_result` | 带 `is_error` 标记 |
| `ToolProgress` | Data part + `sdk.type=tool_progress`，或作为心跳 | 见 §5.7 |
| `Usage` / `StepEnded` | artifact metadata（不单独成事件） | 成本可观测 |
| `FileDiffInfo{File, Patch, Additions, Deletions, Status}` | Data part + `sdk.type=file_diff` | **差异化亮点**：代码改动本身就是可消费的 artifact |
| 入向非文本 part（图片 / raw / 文件 URL） | 落盘到工作区 → prompt 里给路径 | coding agent 的 read/write 按路径工作，塞进 message 无意义 |

### 4.3 Metadata 命名约定

沿用 ADK 的教训，**禁止各处手写字符串**：

| 方向 | 前缀 | 常量 |
|---|---|---|
| SDK → A2A（写进 A2A 对象 metadata） | `sdk.` | `a2a.ToA2AMetaKey(key)` |
| A2A → SDK（写进 `session.Event.CustomMetadata`） | `a2a.` | `a2a.ToSDKMetaKey(key)` |

需要覆盖的键至少包括：`task_id`、`context_id`、`thought`、`partial`、`type`、`is_long_running`、`usage_metadata`、`file_diff`、`session_id`、`agent`、`invocation_id`。

### 4.4 事件 → task 状态机

| 时机 | 发出 |
|---|---|
| 无 StoredTask | `Task{state=submitted}` |
| 开始 `Send` 之前 | `TaskStatusUpdateEvent{working}` |
| text / reasoning / tool delta | `TaskArtifactUpdateEvent{append=true, lastChunk=false}` |
| `permission.asked` / `question.asked` | `TaskStatusUpdateEvent{input-required}` + 结构化 question payload |
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
- A2A 的 `relatedTasks` / task 图能力本次不使用。

---

## 5. 入向（Server）设计

### 5.1 组件划分

| 组件 | 职责 |
|---|---|
| `CardBuilder` | 由已注册 agent 生成 AgentCard：`mode != subagent && !hidden` 的 agent 作为 skills（复用 `SubagentTypes` 的过滤逻辑）；工具列表走 registry materialize |
| `Executor` | 等价于 `a2asrv.AgentExecutor` 的三件事：Execute / Cancel / Cleanup |
| `TaskStore` | task ↔ session/turn 绑定、状态、artifact 边界、pending requestID |
| `PendingRegistry` | 挂起的 turn 迭代器 + 待回复的 requestID |
| `WorkspaceResolver` | 调用方身份 → 工作区 / Root / 权限规则集 |
| `Handler()` | 返回 `http.Handler`（well-known card path + JSONRPC / HTTP+JSON path） |

### 5.2 接线方式

**返回 `http.Handler`，由宿主自己挂 mux。**

不做 adk-go 那种 `launcher` 把 server 与启动方式强绑定。理由：符合 SDK 一贯的「窄输入、不读 env、不选默认位置」哲学（`Root` 必填、`ConfigDirs` 显式声明），也让嵌入既有 HTTP 服务变得简单。

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
| 有 `taskId`，task 已终态 | 拒绝（或按新 turn 处理）→ **待决策 D6** |

**并发模型**：SDK 一个 session 同时只能跑一轮（queue/steer）；A2A 同一 context 的 task 串行、不同 context 并发。`contextId → session` 的一一映射天然保证了串行性。

### 5.4 HITL：最关键的设计决策

远端用户应答权限/提问有两条路线。

#### 路线 A：挂起 + resume（建议作为默认）

`asked` 事件到达时，A2A 层把这个 turn 的迭代器 **park 住**（`session.Send` 的 goroutine 本来就阻塞在 `Ask` 的 channel 上），发 `input-required` 并把 requestID 写进 task 状态；下一条 A2A message 回来后调用 `Reply`，被阻塞的 goroutine 自然继续，原迭代器继续 yield。

| 优点 | 代价 |
|---|---|
| 语义完全保真（模型上下文、工具侧状态、已完成的 step 不丢） | 进程内状态，崩溃/重启丢失挂起 |
| 不重复调用 LLM、不重复计费 | 水平扩展受限于 sticky session |

#### 路线 B：结束本轮 + 重放

`asked` 时直接结束本轮，把「问题 + 待批准的工具调用」持久化进 session，下一条 message 作为新 turn 继续。

| 优点 | 代价 |
|---|---|
| 无进程内状态，可水平扩展，崩溃可恢复 | 模型要重新推理 |
| | 工具可能被重放（对 `bash` / `write` 是危险的） |

#### 建议：混合策略

- **A 做默认**（在线路径，保真且省 token）；
- **B 做降级恢复**：task 状态落库（§5.6），进程重启后发现 task 处于 `input-required`，则按 B 重建一个 turn。

混合策略是 ADK 给不了的（它只有 B），也是本次投入最值得的部分。**待决策 D2。**

#### 配套需要改 SDK 的两个点

1. **`agent.Event` 并集补 permission/question 事件**：目前 `permission.asked` 只在 core 事件总线上，`events.go` 的 `mapEvents` **完全不映射**，宿主拿不到。需要新增 `PermissionRequested` / `PermissionReplied` / `QuestionAsked` / `QuestionAnswered`；
2. **新增 session「等待外部输入」状态**：挂起期间 session 仍是 busy，会阻塞同 session 的其他 `Send`，语义上是错的。可以只在 A2A 层维护，但更干净的是让 `SessionStatus` 多一个值。

### 5.5 安全模型

远端 A2A 调用方可以让本机 agent 执行 `bash` / `write` / `edit` —— 这是 ADK 那类问答 agent 不会有的风险面。强制要求：

| 规则 | 内容 |
|---|---|
| 默认不信任 | A2A 引入的 session 使用**显式传入**的权限规则集（默认对危险工具 ask 或直接 deny），不使用 client 级的 `ApproveAll` |
| 工作区隔离 | `WorkspaceResolver` 把 (调用方身份, contextId) 映射到工作区；复用既有 `Project` / `Directory` / `Location` / `Sandboxes` |
| 凭据 | 远端认证（bearer / header）走 SDK 的 `Credentials` / headers 窄输入，不读 env |
| 禁止隐式提权 | A2A metadata **不允许**覆盖 agent / model / permission；按 skill 选 agent 只能落在 card 已广告的范围内 |
| 默认开关 | 入向 server 默认不开启危险工具，由宿主显式打开 → **待决策 D7** |

### 5.6 Task 状态持久化

A2A 要求 task 可重查（`tasks/get`、断连后 `tasks/resubscribe`），因此必须 durable。

| 方案 | 评价 |
|---|---|
| **新增 `session_task` 表**（走既有 `core/database` migration）：`id` / `project_id` / `session_id` / `turn_anchor` / `state` / `last_message_id` / artifact 边界 / `pending_request_id` / `time` | **建议**。从 `session_message` 反推 task 状态对 artifact 边界是有损的 |
| 塞进 session 行的 JSON 列（`Agent` / `Model` / `Permission` 已是这个模式） | 省一张表，但查询与清理麻烦 |

→ **待决策 D5。**

### 5.7 长任务与心跳

SDK 的一轮可能跑很久（多 step 工具循环），A2A 客户端通常有超时。建议：

- 服务端支持可配置心跳间隔；
- 把 `StepStarted` / `ToolProgress` 映射为 `working` 状态更新以保活；
- 心跳**不写 artifact**，避免污染输出。

---

## 6. 出向（Client）设计

### 6.1 四种接入形态

| 形态 | 实现成本 | 能力 | 建议 |
|---|---|---|---|
| **① Tool**（`a2a.NewRemoteTool`） | 极低：注册进 `Client.Tools` / `Session.Tools` / plugin `Tools`，不改核心 | 模型可见远端卡描述；流式只能通过 `tool.Context.Progress` 粗粒度透出；不能反向提问 | Phase 1，跑通协议与保真度 |
| **② Subagent**（`SubagentTypes` + `task` 委派） | 中：新增 `A2ARunner`（实现 `runner.SessionRunner`）+ drain 分派 + agent backend 字段；**不改** `task.go` / `runTask` / `SubagentTypes` | 与本地 subagent 用法完全一致（见 §3.3 不变量清单）；`task_id` 续跑天然可用 | 长期主形态 |
| **③ 独立 Delegate 原语** | 高 | 语义最干净，但绕开了 session 语义 | 不做 |
| **④ 主 agent 形态** | **零额外成本** | `CreateSession(Agent: <远端 agent id>)` 即用；因为 backend 是 session 的属性（§3.3） | 免费获得 |

形态 ② 的实现要点见 §3.3：**不要**在 `runTask` 里分叉，而是在 coordinator 的 drain 上按 agent 的 backend 分派，让远端 agent 拥有真实的镜像子会话。选择这个接入点的直接收益是 `task` 工具、`SubagentTypes`、进度事件、深度限制、权限派生、hooks 继承全部零改动。

→ **待决策 A1 / A2（§10.1）。**

### 6.2 映射细节

**发什么**

- Tool 形态：只发本次调用的参数（一个 message）；
- Subagent 形态：发 task 的 prompt；
- 续跑同一 task 时必须带 `taskId` / `contextId`；
- SDK 有 durable session，**不需要**像 ADK 那样计算「远端缺哪些事件」，但本地历史的增量不要重发。

**收回什么**

- 远端 artifact → `TextDelta`（保证 UI 实时）；
- 远端 tool call data part → 降级为 `ToolProgress` 或结构化日志，**不要伪装成本地 `ToolCalled`**（否则本地权限 / 审计 / hooks 会把远端工具调用误判成自己的）；
- `thought` metadata → `ReasoningDelta`。

**远端 input-required → 本地**

- 方案一：映射为本地 `QuestionRequest`（走既有 `OnQuestion` 通路）；
- 方案二（Tool 形态的保守做法）：变成一次失败 + 提示文本，让本地模型自己决定是否用 `question` 工具问用户。

→ **待决策 D4。**

**取消与清理**

- 本地 `Session.Abort()` / ctx 取消 → 远端 `CancelTask`。ADK 的 `cleanupRemoteTask` 有完整参考：仅在「最后一个事件非终态」且不是 input-required 时取消，超时 5 秒；
- 本地 `Session.Close()` → 检查并收敛所有未终态的远端 task；
- 远端 task 处于 `input-required` 且本地正常结束 → 不取消（保持可续跑）。

**Card 解析的二次利用**

拿到 card 后除了连客户端，还要榨出信息：

| Card 字段 | 用途 |
|---|---|
| `description` | 工具 / subagent 的描述 |
| `skills` | 生成子 agent 列表 |
| `capabilities.streaming` | 决定 `SendMessage` 还是 `SendMessageStream` |
| `defaultInputModes/OutputModes` | 决定是否需要附件落盘 / 文本兜底 |

---

## 7. Phase 0：需要先动的 SDK 侧前置改造

这些改造都是**通用能力**，不是为 A2A 打的补丁，因此值得先行。

| 改动 | 位置 | 为什么必须 |
|---|---|---|
| `agent.Event` 补 permission / question 事件映射 | `events.go` | 否则入向无法感知 `asked`，出向无法把远端提问浮上来 |
| session「等待外部输入」状态 | `session.go` / `SessionStatus` | 挂起期间不能表现为正常 busy，否则并发 `Send` 语义会乱 |
| agent 运行时扩展点（`AgentRunner` / agent kind） | `runtime.go` / `task_run.go` / `schema.AgentInfo` | 出向形态 ② 的前提 |
| session / task 绑定存储（`session_task` + migration） | `core/database` | 入向的 task 可重查、断连恢复 |
| 「是否可被 A2A 调用」标记与 card skills 过滤 | `CardBuilder` + agent 元信息 | 避免把内部子 agent 广告出去 |

---

## 8. 路线图与验收

| Phase | 内容 | 验收 |
|---|---|---|
| 0 | §7 的五项前置改造 | 现有测试全绿；新增事件可见性测试 |
| 1 | `a2a/` 类型 + `a2a/client` + Tool 形态出向（card 解析、SendMessage/Stream、取消） | 远端 agent 能被本地模型当工具调用；超时 / 取消 / 错误路径全测 |
| 2 | `a2a/server` 入向 MVP（executor + card + `http.Handler`，输入输出全文本） | SDK↔SDK 端到端：A2A 客户端发消息拿到 completed task + artifact |
| 3 | HITL 闭环（挂起 + resume）、多轮 context、task 落库 | permission / question 往返测试；重启后降级恢复测试 |
| 4 | Subagent 形态 + part 级保真（tool trace、thought、usage、file diff） | **与 ADK-go 对打**的互操作测试（跨实现验证协议保真度） |
| 5 | 生产化：`WorkspaceResolver`、认证、心跳、限流、artifact 外置钩子 | 多租户隔离测试；`go test -race` 全绿 |

**测试矩阵**参考 adk-go 的 `a2a_e2e_test.go` 场景命名，一个都不能少：

`input-required`、`multi-hop input-required`、`input-required cancellation`、`cleanup propagation`、`single-hop final response`、`streaming success`、`streaming error`、`session reuse`、`callbacks`、`converters`、`custom part converter`、`error on card resolution`、`error on server error`。

---

## 9. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | 挂起 + 写库 + 事件分发三者的并发正确性。SDK 事件分发是同步无损的，park 一个 turn 会长时间占用协调器条目 | `go test -race` + `testing/synctest` 覆盖；Phase 0 先做状态建模 |
| 2 | **安全**：远端可触发本机代码执行。这是事故面，不是技术债 | §5.5 强制规则；入向默认不开启危险工具 |
| 3 | 语义损失：SDK 的 delta 事件流与 A2A 的 task/artifact 不同构 | 策略为「多轮往返保真，边界处显式降级并写文档」（§4.6） |
| 4 | a2a-go 成熟度与协议演进 | `a2a/sdkclient/` 薄适配层是唯一防线；吸取 adk-go 双版本窘境的教训 |
| 5 | 文档一致性 | `AGENTS.md` 包架构表 + `DESIGN.md` 必须同步加 `a2a/`，并注明「无 TS 对应物，属 Go 专属能力」，否则后续维护者会去 opencode 里找对应文件 |
| 6 | `docs/DESIGN.md` 的「不包含」清单需要更新（A2A 从非目标变为目标） | 实现落地时同步更新 |

---

## 10. 待决策清单

### 10.1 架构级决策（A 系列，优先收敛）

| ID | 问题 | 建议 | 状态 |
|---|---|---|---|
| A1 | 远端 agent 的实现位 | 落在「session 执行后端」：新增 `A2ARunner` 实现 `runner.SessionRunner`，coordinator drain 按 agent backend 分派；**不**在 `runTask` / `task` 工具层分叉（§3.3） | 待定 |
| A2 | 远端子 agent 是否创建本地镜像子会话 | 是。一致性来自「两种后端共享同一套 session 语义」，而非两套实现互相对齐（§3.3） | 待定 |
| A3 | 暴露模型 | 显式 expose 声明替代 `Mode` / `Hidden` 启发式；层级 A（间接触达）为默认，层级 B（直接路由）需显式开启（§3.4） | 待定 |
| A4 | 层级 B 的入向权限派生 | 需新定义：调用方身份规则 ∩ agent 自身规则 ∩ 暴露策略；`DeriveSubagentSessionPermission` 不适用（§3.4） | 待定 |
| A5 | 远端 usage / model 在事件与 progress 中如何表达 | `publishTaskProgress` 的 `model` 字段允许为空或填 card 声明的值；`Usage` / `StepEnded` 可缺失，UI 需容忍 | 待定 |
| A6 | 本地上下文如何导出给远端 | 显式上下文导出策略（附件落远端工作区 / 只发文本摘要），禁止隐式把本地路径交给远端 | 待定 |

### 10.2 实现级决策（D 系列，A 系列定完再收敛）

| ID | 问题 | 建议 | 状态 |
|---|---|---|---|
| D2 | HITL 路线 | A（挂起 + resume）为默认，B（重放）为重启降级 | 待定 |
| D4 | 远端 input-required 如何本地化 | 映射为本地 `QuestionRequest`（Tool 形态可退化为失败 + 提示） | 待定 |
| D5 | task 状态存储 | 新增 `session_task` 表 | 待定 |
| D6 | 已终态 task 收到新消息 | 拒绝（返回任务已结束错误） | 待定 |
| D7 | 入向安全默认值 | 默认 deny 危险工具，宿主显式开启（与 A4 合并细化） | 待定 |
| D8 | 依赖策略 | 只引 a2a-go/v2 单版本 + 薄适配层 | 待定 |
| D9 | card 粒度 | 一个 Client 一个 card；skills 只来自显式暴露的 agent，**不广告工具**（与 A3 合并细化） | 待定 |
| D10 | artifact 粒度 | 只做 per-turn 流式追加，放弃 ADK 双模式 | 待定 |

> 原 D1（出向/入向优先级）与 D3（出向形态）已被 A1–A4 取代：前者是排期问题，后者的答案随 A1 而定。

---

## 附录 A：与 adk-go `adka2a` 的对照

| 维度 | adk-go | go-agent-sdk（本文） |
|---|---|---|
| session 语义 | 无状态 runner + `SessionService`，`contextId = sessionID` | durable SQLite session，`contextId = sessionID` |
| `input-required` | 专门造 long-running tool 协议；本轮结束，下轮重跑 | 直接桥接既有 `permission.asked` / `question.asked` + `Reply` |
| 挂起能力 | 无（只有重放） | 有（轮内阻塞 + park/resume） |
| artifact 粒度 | 双模式（per-run / per-event），partial 需擦除 | 单一 per-turn 追加 + 封口 |
| 出向形态 | 只有 `remoteagent.NewA2A`（一等 agent） | 先 Tool，后 Subagent |
| 版本策略 | v0.3.15 + v2.2.1 双版本共存 | 只引 v2 单版本 |
| server 接线 | 与 launcher 强绑定 | 返回 `http.Handler`，宿主自挂 |
| 安全模型 | `userID` 来自 `CallContext`，未强调工作区隔离 | 权限规则集 + 工作区隔离 + 默认不信任 |

可复用的思路（值得参考但不照抄）：part 类型标记的 data part 编码方式、`cleanupRemoteTask` 的取消判定、card skills 自动生成、e2e 测试矩阵。
