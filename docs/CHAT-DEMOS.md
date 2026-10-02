# examples/chat 演示 backlog

> 状态：**草案，待排期**
> 范围：整理 `examples/chat` 还可以演示哪些 go-agent-sdk 能力（只列选题与切入点，不含实现）
> 关系：公开 API 以 `docs/DESIGN.md` 为准；本文只是 `examples/chat` 的演示清单

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-02 | 初稿：现状盘点、候选演示、需先补的 SDK 接口、优先级 |

---

## 1. 现状（已演示）

会话管理 `/new` `/sessions` `/delete`（含 resume）、`/history`、`/compact`、`/persona`（SystemPrompt append）、
`/reference`（local）、`/tool`（运行时注册）、`/skills` `/agents` `/mcps` `/task`、
权限交互、question 交互、model catalog + token limits、自动压缩、自动标题、
usage/cost、reasoning 流、subagent 嵌套、SessionHooks（见 2.1）、`-C`/`--yolo`。

---

## 2. 只改示例即可演示

### 2.1 SessionHooks（生命周期与工具钩子）— 高 ✅ 已实现

- API：`SessionConfig.Hooks`（`session.go:40`）、`SessionHooks`（`hooks.go:12`）
- 实现：`examples/chat/hooks.go` 的 `newSessionHooks` 挂到每个新建/恢复的会话（task 子会话继承）。启动横幅有说明；
  `[hook]` 行记录生命周期；`OnSessionStart` 注入当天日期；`?<text>` 改写为简洁回答；`bash rm -rf` 被 `OnPreToolUse` 拒绝。
  （原切入点，供参考）
  - `OnSessionStart` → `AdditionalContext` 注入当天日期
  - `OnUserPromptSubmitted` → 展开 `!cmd` / `@file` 前缀
  - `OnPreToolUse` → 拦截危险 bash 或改写参数
  - `OnPostToolUse` / `OnPostToolUseFailure` → 追加 context
  - `OnToolDefinition` → 隐藏某个工具
  - `OnErrorOccurred`
- 价值：覆盖面最大，几乎每个 hook 都能在 REPL 里立刻看到效果。

### 2.2 Steer（运行中改道）— 高 ✅ 已实现

- API：`Session.Steer`（`session.go:138`）
- 实现：turn 进行中键入文本会在状态栏显示 `steer: <draft>`，Enter 通过 `Session.Steer` 送进当前轮
  （`watchInterrupt` 新增 steer 回调与转义序列处理，见 `examples/chat/turn.go`）；Esc/Ctrl+C 中断行为不变。

### 2.3 权限 Always / 持久化 — 高 ✅ 已实现

- API：`PermissionDecision.Always`、`PermissionRequest.Save`
- 实现：`examples/chat/permission.go` 三阶段交互——`Allow once / Allow always / Reject`，
  always 先弹确认（列出将记住的 patterns），reject 收集可选原因；对齐 opencode 的交互与文案。
  **默认选中 `Allow always`**（`Choice [2]`，便于演示；上游默认是 `Allow once`）。
- 持久化：`Always` 规则由 SDK 写入 project 的 `permission` 表（对齐 V2 core 的 `PermissionSaved`），
  **跨重启保留**；确认文案据此写成「until the rule is removed (saved for this project)」。
  （注：TS 的 TUI 文案「until OpenCode is restarted」对应的是 V1 权限的**内存**实现——`InstanceState.approved`，
  与 Go 走 DB 的行为不同，故此处不照搬。）

### 2.4 会话级 agent / 模型切换 — 高 ✅ 已实现

- API：新增 `Session.SetModel` 与 `Session.SetAgent`（`session.go`），对齐 V2 core 的
  `V2Session.switchModel` / `switchAgent`（`packages/core/src/session.ts:402`、`:393`）：
  发布已有的 `session.next.model.switched` / `session.next.agent.switched`，更新 session 行，下一轮生效。
- 实现：
  - `/model [<id>]` —— 无参列出**可用**模型；`/model <id>` 在**当前会话**切换模型。
  - `/agent [<id>]` —— 无参列出可切换的主 agent；`/agent <id>` 在**当前会话**切换 agent。
  两者都**不新建会话**。
- **模型目录（in-code manifest）**：`examples/chat/model.go` 用一份条目清单描述每个模型的
  provider/protocol/baseURL/apiKeyEnv/limits/pricing，派生 `ModelInfoProvider` + `CredentialProvider`
  注入 client。协议取自 `ModelInfo.API.Protocol`、凭据按 providerID，所以**一个 client 可路由多家 provider**；
  仅列出 API key 已设置的模型。匹配 `AGENT_PROVIDER`/`AGENT_PROVIDER_ID` 的条目用 `AGENT_API_KEY`/`AGENT_BASE_URL`
  （与原有 env 配置兼容），其余条目用各自的 provider env。`util.Client` 的默认模型现在可由 option 提供。
- **持久化上次选择**：`examples/chat/state.go` 把上次选的 model/agent 写入 `{ExampleDir}/.data/state.json`；
  启动优先级 **上次选择 > `AGENT_MODEL` > catalog 默认**（`Default: true`，不可用则第一个可用）——
  上次选择优先，这样切换会跨重启保留（TUI 的 `model.json` 是 `config.model > recent`，此处刻意让持久化优先）。
- 已删除原拟的 `/sandbox`（`SessionConfig.Permissions`）与 `/session-tool`（`SessionConfig.Tools`）：
  前者非主流（权限应由 agent/config 规则配置），后者与 `examples/tool` 重复。
- 备注：`/persona` 已演示 `SessionConfig.SystemPrompt`。

### 2.5 事件渲染补齐 — 高 ✅ 已实现

- 实现（`examples/chat/render.go`）：
  - `TitleUpdated` → `[title] <title>`。
  - `StepStarted / StepEnded / StepFailed` → 多步回合打印 `[step N]` 分隔；异常 finish（非 `stop`/`tool-calls`）
    单独提示；失败打印 `[step N failed] ...`。
  - `Retried` → `[retry N] ...`；`ShellStarted / ShellEnded` → `[shell] $ <cmd>` + 输出。
- 注意：`Retried` 与 `ShellStarted / ShellEnded` 在 Go SDK 与 V2 core 里**都没有生产者**（无处 `Publish`），
  所以这几条渲染分支目前不会触发；补生产者属 SDK 侧改动、且与"对齐 V2"相悖，已在
  `events.go` / `schema/session_event.go` 注释说明。

### 2.6 运行时注册 agent — 中 ✅ 已实现

- 实现：`/agent add <name> [<description>]`（`examples/chat/agent.go`）调用 `Client.RegisterAgent`，注册一个
  hidden subagent（`Mode=subagent`、`Hidden`、`Steps=2`、allow-all 权限、生成的 system prompt）。
- 效果：`/agents` 立即列出它（`task.go` 的 `SubagentTypes` 只过滤 primary/被 deny，不过滤 hidden）；
  模型可用 `task` 委派给它，`Steps=2` 演示步数上限。
- 保护：若名字已存在则拒绝（`RegisterAgent` 的 `draft.Update` 会**原地覆盖**已有 agent，含内置 build）。
- 备注：`AgentConfig.Model` 未在命令里暴露（聚焦 Mode/Hidden/Steps/Description/System）。

### 2.7 SystemPrompt 模式与 section — 中

- API：`SystemPromptConfig.Mode`（Replace/Customize）、`SectionOverride`（`systemprompt.go:11`、`:50`）
- 现状：只演示了 append。
- 切入点：`/persona --replace`、`/prompt add|replace|remove <section> <text>`。

### 2.8 协议 / variant — 中

- API：`ModelInfoProvider`（`util.Client` 的 `ModelInfo`）、`schema.ModelVariant`（`schema/model.go:73`）
- 备注：`/model` 切换已在 2.4 实现。此处补充：给 catalog 加 variant 后用 `/variant` 切换；catalog 含多 provider 时演示不同 protocol 路由。

### 2.9 MCP 远程 server / prompts / resources — 中

- API：`ServerConfig.Type="remote"` + `Headers`（`mcp/config.go:28`）、
  `MCP().{Connect,Disconnect,Prompts,Resources,ResourceTemplates,ServerInfo}`（`mcp/client.go:347` 起）
- 现状：只配了本地 stdio weather，`/mcps` 只显示状态。
- 注意：无 MCP 配置时 `rt.mcp == nil`（`runtime.go:730`），需固定配一个远程 MCP（或占位 config）才能演示。
- 切入点：`/mcp prompts`、`/mcp resources`、`/mcps` 展示 instructions。

### 2.10 Reference git 源 / hidden — 中

- API：`core.ReferenceGitSource`（`schema/reference.go:14`）、`ReferenceInfo.Hidden`
- 现状：只演示 local。
- 切入点：`/reference add-git <name> <repo> [branch]`，演示 clone 缓存预热。

### 2.11 Plugin 的事件 / Options / Tools — 中

- API：`PluginContext.On`、`Options`、`Tools`（`plugin.go:12`）、`ClientOptions.Plugins`（`client.go:75`）
- 现状：只用插件做 permission 的 `Agents.Transform`。
- 切入点：一个订阅事件并运行时注册工具的插件，配置里带 `Options`。

### 2.12 类型化工具 + 权限动作 — 中

- API：`DefineTool` struct 参数与 `jsonschema` tag（`tool.go:44`）、`Tool.Permission` / `SkipPermission`（`tool.go:22`）
- 现状：`/tool now|dice` 太简单。
- 切入点：加一个带枚举/必填字段的 `weather` 工具；给某工具自定义 permission action 触发交互审批。

### 2.13 工具输出限长 + 清理 — 低

- API：`ClientOptions.ToolOutput`（`client.go:120`）、`Client.CleanupToolOutput`（`client.go:431`）
- 切入点：设小 MaxLines/MaxBytes 演示截断提示；`/cleanup` 清理并报告数量。

### 2.14 配置 modes 文件 / Locale — 低

- API：`{mode,modes}/*.md`（`defs/config/agent.go:32`）、`ClientOptions.Locale`（`client.go:96`）
- 切入点：加一个 `.config/mode/*.md` 并在 `/agents` 展示；用中英混合名对比 locale 排序。

---

## 3. 需先补 SDK 接口（当前无法演示）

| 能力 | 缺口 |
|---|---|
| 文件/图片附件 | `MessageOptions` 只有 `Prompt`/`Mode`，无 Files（`session.go:44`） |
| Todo 列表 | 无公开读接口，只有内置 `todowrite` 工具 |
| 快照/diff | 高层 `StepEnded` 不带 files（底层 `session.next.step.ended` 有） |
| revert / undo / redo | 无公开 API |
| rename（SetTitle） | `DeleteSession` 之外没有改 title 的接口 |
| share / unshare | 无公开 API |
| fork session | 无公开 API |
| 异步权限/问题（List/Reply/Reject） | `Client` 没有 `Permission()` / `Question()` getter |
| 用户工具进度 | `ToolContext` 只有 `SessionID`/`ToolCallID`，无 Progress（`tool.go:16`） |
| A2A | 仅 `docs/DESIGN-A2A.md` 设计稿 |

---

## 4. 优先级建议

| 优先级 | 选题 |
|---|---|
| 高 | SessionHooks、Steer、权限 Always、会话级模型切换、事件渲染补齐 |
| 中 | 运行时注册 agent、SystemPrompt 模式、MCP prompts/resources、git reference、类型化工具 |
| 低 | CleanupToolOutput、ToolOutput 限长、modes、Locale、model variants |
