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

### 2.3 权限 Always / 持久化 — 高

- API：`PermissionDecision.Always`、`PermissionRequest.Save`（`permission.go:27`）
- 现状：交互只返回 `Allow`。
- 切入点：`[y]es/[a]lways/[n]o`，`Always` 后同一 action 不再询问，演示记住规则。

### 2.4 会话级配置 — 高

- API：`SessionConfig.{Permissions,Tools,OnPermissionRequest,OnQuestion,Model}`（`session.go:17`）
- 切入点：
  - `/sandbox` — 新建一个 deny `bash`/`edit` 的会话（session 级权限）
  - `/model <id>` — 新建会话并切换模型
  - `/session-tool` — 只在当前会话注册一个工具
- 备注：`/persona` 已演示 `SystemPrompt`。

### 2.5 事件渲染补齐 — 高（成本最低）

- 现状：`Retried` / `StepStarted` / `StepEnded` / `StepFailed` / `ShellStarted` / `ShellEnded`
  已在 `events.go` 映射，但 `render.go` 不渲染。
- 切入点：重试提示、step 分隔、bash 命令块与输出。纯 UI。

### 2.6 运行时注册 agent — 中

- API：`Client.RegisterAgent`（`client.go:214`）、`AgentConfig{Mode,Hidden,Steps,Model,Permissions}`（`client.go:172`）
- 切入点：`/agent add` 注册一个 hidden 子 agent，`/agents` 立即出现；用 `Steps` 演示步数上限。

### 2.7 SystemPrompt 模式与 section — 中

- API：`SystemPromptConfig.Mode`（Replace/Customize）、`SectionOverride`（`systemprompt.go:11`、`:50`）
- 现状：只演示了 append。
- 切入点：`/persona --replace`、`/prompt add|replace|remove <section> <text>`。

### 2.8 多模型 / 协议 / variant — 中

- API：`SessionConfig.Model`、`ModelInfoProvider`（`util.Client` 的 `ModelInfo`）、`schema.ModelVariant`（`schema/model.go:73`）
- 切入点：`/model` 列出/切换，演示不同 provider/protocol 路由；给 catalog 加 variant 后用 `/variant` 切换。

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
| 高 | SessionHooks、Steer、权限 Always、会话级配置、事件渲染补齐 |
| 中 | 运行时注册 agent、SystemPrompt 模式、MCP prompts/resources、git reference、类型化工具 |
| 低 | CleanupToolOutput、ToolOutput 限长、modes、Locale、model variants |
