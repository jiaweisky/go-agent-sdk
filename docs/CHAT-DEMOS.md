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
- **模型目录（in-code manifest）**：`examples/internal/util/catalog.go` 用一份条目清单描述每个模型的
  provider/protocol/baseURL/apiKeyEnv/limits/pricing，派生 `ModelInfoProvider` + `CredentialProvider`
  注入 client。协议取自 `ModelInfo.API.Protocol`、凭据按 providerID，所以**一个 client 可路由多家 provider**；
  每条用各自的 `APIKeyEnv`/`BaseURL`，仅列出 API key 已设置的模型。`chat` 与 `tool` 共用这份 catalog，
  模型经 `util.WithModel` 传给 `util.Client`。
- **持久化上次选择**：`examples/chat/state.go` 把上次选的 model/agent 写入 `{ExampleDir}/.data/state.json`；
  启动优先级 **上次选择 > catalog 默认**（`Default: true`，不可用则第一个可用）——
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

### 2.7 SystemPrompt 模式与 section — 中 ✅ 已实现

- API：`SystemPromptConfig.{Mode,Content,Sections}`、`SectionOverride`（`systemprompt.go:11`、`:50`）
- 实现：
  - `/persona [--replace] <text>`（`examples/chat/persona.go`）：`append`（默认）或 `replace`。
  - `/prompt add|replace|remove <section> [<text>]`（`examples/chat/systemprompt.go`）：`customize` + 单个
    `SectionOverride`；内置 id `agent_system`/`instructions`，其它名称为自定义 section（渲染在 agent_system 与
    instructions 之间，按 id 排序）。
  - 二者共用 `startSystemPromptSession`，均**新建会话**并 `replaceSession`。
- 限制：`SystemPrompt` 仅创建期生效、不持久化（`session.go:27`、`client_session.go:46`），故每次改动都是新会话；
  且 `Sections` 只作用于 agent system 部分，改不到运行时 `systemcontext` baseline。
- 可测性：合成后的 prompt 无公开读接口，示例测试只验证命令接线与新建会话。

### 2.8 协议 / variant — 中 ✅ 已实现（variant 部分不做）

- 协议路由：`examples/internal/util/catalog.go` 的 catalog 里，同一 DeepSeek 模型以三个协议各列一条，基址分别对应该
  协议的 endpoint，请求模型名统一 `deepseek-flash`（`ModelEntry.Model`，`ID` 只作 catalog 选择器）：

  | catalog id | protocol | base URL | 最终 endpoint |
  |---|---|---|---|
  | `deepseek-flash` | `openai-compatible-chat` | `https://api.deepseek.com` | `/chat/completions` |
  | `deepseek-flash-responses` | `openai-responses` | `https://api.deepseek.com` | `/responses` |
  | `deepseek-flash-anthropic` | `anthropic-messages` | `https://api.deepseek.com/anthropic` | `/messages` |

  一个有 `DEEPSEEK_API_KEY` 就能演示三种路由（每条用各自的 `BaseURL`，凭据按 provider `deepseek` 取）。
  `FromCatalogModel`（`core/session/runner/model.go:138`）按 `ModelInfo.API.Protocol` 查协议、按 providerID 取
  凭据；2.4 的 `/model` 即可切换。本项并补**可见性**：`/model` 列表显示 `id (provider · protocol)`。
- variant：**刻意不做**。`schema.ModelVariant` + `ModelRef.Variant` 机制存在（`runnerModelWithVariant`，
  `core/session/runner/model.go:83`），但只改请求 body/header，chat UI 不可见；`temperature` 类演示单薄，
  reasoning 类依赖 provider 支持且易 400。故不在示例中演示，仅此说明。

### 2.9 MCP 远程 server / prompts / resources — 中 ✅ 已实现

- 新增 `examples/notesmcp`：可独立运行的小 MCP server（默认 stdio；`--http <addr>` 走 streamable HTTP）。
  提供 4 个工具（`note_save`/`note_get`/`note_delete`/`note_list`）、2 个 prompt（`notes_digest`/`action_items`）、
  资源 `notes://index`、模板 `notes://note/{id}` 和 instructions；笔记持久化到一个 JSON 文件。
- chat 默认以 **remote(HTTP)** 使用它：`run()` 用 `go run ./examples/notesmcp --http 127.0.0.1:<free>` spawn
  子进程，轮询就绪后注册 `ServerConfig{Type:"remote"}`，退出时按**进程组** kill（`examples/chat/notesmcp.go`
  及 `_unix`/`_windows` 变体）。stdio 由 SDK 的 `CommandTransport` 自行托管、chat 无需管，故暂未接入。
- 命令：`/mcp prompts | prompt <name> [k=v ...] | resources | resource <uri>` 列出/读取；`/mcps` 现在也显示
  各 server 的 instructions。
- 说明：`ServerConfig` 的静态鉴权用 `Headers`（无 OAuth）；「无 MCP 配置时 `rt.mcp == nil`」因 notes server
  默认在而不再出现。

### 2.10 Reference git 源 / hidden — 中 ✅ 已实现（hidden 部分不做）

- 实现：`/reference add-git <name> <repo> [<branch>]`（`examples/chat/references.go`）注册 `ReferenceGitSource`；
  SDK 立即算出缓存路径并**后台 shallow clone**（git `--depth 100`）到 `<Root>/repos/...`，模型下一轮即可读该目录。
- `/reference` 列表对 git 源显示 `[git] cloning...` / `[git] ready`（按缓存目录是否已存在判断），local 显示 `[local]`。
- 因 `materialize` 对「非 remote / branch 非法」**静默跳过**，命令加完会核对物化列表，失败时报 `was not accepted`。
- **hidden 不做**：`ReferenceInfo.Hidden` 在 Go 与 TS 的 guidance 里都**未被消费**（可见性判据是 `Description != nil`，
  `adapters.go` 还把 Hidden 丢了），无行为可演示，故不纳入。
- 备注：`file://` 源被 `RepositoryIsRemote` 判为非 remote 而跳过，所以 git 演示必须是真正的远程仓库。

### 2.11 Plugin 的事件 / Options / Tools — 中 ✅ 已实现

- 实现（`examples/chat/plugin.go`）：通过 `ClientOptions.Plugins`（`util.WithPlugin`）注册两个插件：
  - `chat-greeter`：`Options` 提供 greeting；`PluginContext.On` 订阅 `ToolCalled` 计数；`PluginContext.Tools.Register`
    注册工具 `plugin_greet`（读 `Options` 并汇报计数）；cleanup 退订 + 注销。
  - `chat-permission`：把原 `addBashAskRule` 从底层 `client.Plugin().Add` 迁到高层 `Plugins`（`Agents.Transform`）。
- 命令：`/plugin remove <id>` 用 `client.Plugin().Remove` 卸载并触发 cleanup（`[plugin] ... unloaded`，工具随即消失）。
  因 `core.Plugin` 无查询接口、`Remove` 对未知 id 也不报错，示例自持一份已加载 id（`pluginRegistry`），未知或已移除的
  id 报 `not active`。
- 说明：**事件订阅 / 运行时工具 / agent 变换本身已被其它 API 演示**（`Client.On`、`/tool`→`Client.RegisterTools`、
  permission 插件）；2.11 补的是**高层 `ClientOptions.Plugins` 注册路径 + `PluginConfig.Options` 声明式配置 +
  cleanup 生命周期**。
- 限制：高层 `Options`/`Tools`/`On` 仅在**启动期 `ClientOptions.Plugins`** 可用（由 `adaptPlugin` 注入）；
  `client.Plugin().Add` 是低层 `core.PluginContext`（无 `Options`/`Tools`/`On`），故运行时只能 Remove，不能再 Add
  一个高层插件。

### 2.12 类型化工具 + 权限动作 — 中 ✅ 已实现

- 实现（`examples/chat/convert.go`）：`/tool add convert_length` 注册类型化工具 `convert_length`：
  - 参数用**手写 schema AST**（`jsonschema.Object` + `Required` + `Enum`）而非反射，故 `from`/`to` 为枚举
    （mm/cm/m/km/in/ft/yd/mi）、`value` 必填；handler 仍用 `DefineTool` 的类型化结构体（注册时覆盖 `Parameters`）。
  - `Permission: "convert_length"`，`permissionPlugin` 加规则 `{convert_length,*,ask}`，非 `--yolo` 时调用走
    `Allow once/always/reject` 交互审批。
  - 对照工具 `list_length_units`：同一 action 但 `SkipPermission: true`，永不经审批。
- `/tool` 的空列表提示改为**从注册表派生**可用工具名（`runtimeToolNames`），不再写死 `now | dice`。
- 备注：`convert_length` 取代原拟的 `weather`（chat 已有一个天气 MCP，避免命名/功能冲突）；enum 无法由 `DefineTool`
  反射生成，故覆盖 `Parameters`（内置工具同样是手写 AST，参考 `core/tool/webfetch.go` 的 `jsonschema.Enum`）。

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
