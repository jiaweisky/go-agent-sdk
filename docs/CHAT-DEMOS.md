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

### 2.13 工具输出限长 + 清理 — 低 ✅ 已实现

- 限长：保持 SDK 默认（2000 行 / 50KiB）；新增运行时工具 `emit_lines{count}`（`examples/chat/tooloutput.go`）。
  `/tool add emit_lines` 后用大 count（如 5000）触发截断——超限内容落盘到 `<Root>/data/tool-output/tool_*`，
  模型看到 head + `... output truncated; full content saved to <path> ...` + tail 的预览。不改全局限，避免影响其它工具。
- 清理：`/tool-output` 显示托管目录与文件数；`/tool-output cleanup` 调 `Client.CleanupToolOutput()`。因该 API
  **只删 7 天前的文件且不返回数量**，示例自行统计目录文件数，报告 `scanned N, removed M`（新文件不会被删）。
- 说明：命令名为 `/tool-output`（不是泛化的 `/cleanup`）；目录路径由示例按 `core.Global` 布局推导（SDK 无 accessor）。

### 2.14 配置 modes 文件 / Locale — 低 ✅ 已实现（Locale 仅 env）

- modes：新增 `examples/chat/.config/mode/planner.md` → **primary agent**（`{mode,modes}/*.md` 由 `defs/config` 加载，
  `Mode` 强制为 `primary`），在 **`/agent`**（不是 `/agents`）列出并可切换；`{agent,agents}/**/*.md` 才是 subagent、进 `/agents`。
- Locale：不设 `ClientOptions.Locale`，沿用 env（`util.LocaleTag` 由 `LC_ALL`/`LC_COLLATE`/`LANG` 解析）。它做 locale 感知
  排序，影响模型侧引导（advertised subagent types、skills/reference guidance）与 chat 的 `/agents`、`/mcps`。另加两个中文名
  subagent（`.config/agent/翻译.md`、`摘要.md`）：`LC_ALL=zh_CN.UTF-8` 按拼音、`en_US.UTF-8` 按码点，`/agents` 顺序不同
  （`TestSubagentLocaleOrdering` 锁定）。
- 说明：`/agent`（primary 列表）用 `sort.Strings`，不吃 locale；示例未暴露 `ClientOptions.Locale`（仅 env）。

### 2.15 文件/图片附件 — 高 ✅ 已实现（含小幅 SDK 改动）

- SDK：`MessageOptions` 新增 `Files []schema.PromptFileAttachment`（**消息级**附件）；`Session.run` → `runtime.admit`
  透传 `schema.Prompt{Text, Files}` → user 消息的 `Files` → `llmMedia` → `MediaPart`（协议层已支持 anthropic image /
  openai `image_url` / `input_image`）。底层管线原本就有，缺口只在 `MessageOptions` 与 `admit` 两处。
- 示例（`examples/chat/attach.go`）：`/attach <path>` 读图 → 推 MIME（仅 png/jpeg/gif/webp）→ 编
  `data:<mime>;base64,...`（SDK 的 `ValidateMedia` 只吃 base64/data URL、**不读路径**）→ 随**下一句**发送后清空；
  `/attach` 列出、`/attach clear` 清空。
- 限制：仅图片；decoded ≤20MB / encoded ≤28MB。`deepseek-flash` 即支持视觉（OpenAI 兼容 `image_url` 的 data URL，
  与 `openai-compatible-chat` 协议产出的载荷一致），实测通过。
- 测试：`TestSessionSendAttachesFiles`（文件→媒体 part）、`TestAttachCommand`/`TestAttachRejectsUnsupportedType`。

### 2.16 Todo 列表（事件）— 中 ✅ 已实现（含小幅 SDK 改动）

- SDK：新增高层事件 `agent.TodoUpdated{SessionID, Todos}`（`events.go`），把 `todo.updated` 映射进来。该事件的发布方
  发的是**结构体而非 map**，故映射时用 JSON 往返解码。对齐 v2 core 的 TUI —— TUI 也是订阅 `todo.updated`。
- 示例：`render.go` 收到 `TodoUpdated` 时打印 `[todo] <status-mark> <content>`（☐/▶/☑/✕）；`/todos` 打印事件缓存的
  当前列表。
- 说明：只加**事件**、未加读接口（`Session.Todos()`）；`/todos` 读的是事件缓存的副本，未收到过 `todo.updated` 时为空。
- 测试：`TestClientOnReceivesTodoUpdated`（todowrite→事件全链路）、`TestMapTodoEventStructPayload`、`TestTodosCommand`。

### 2.17 快照 / diff — 中 ✅ 已实现（含 SDK 改动）

- SDK：
  - `Client.Capture() (string, bool)`（当前快照 id；无快照支持/失败返回 false）与
    `Client.Diff(from, to string) ([]core.FileDiff, error)`（runtime 保留 `*core.Snapshot`；空 id/无支持返回 nil）。
  - 高层 `agent.StepEnded` 加 `Files []schema.RelativePath`；`agent.ToolSuccess` 加 `Structured any`（工具结构化输出，
    含 `files[].patch`）。
- 示例（`examples/chat/diff.go`）：
  - 每步结束打印 `[files] N changed: ...`（`StepEnded.Files`）。
  - `edit`/`apply_patch` 工具成功时打印 `[diff] path (+A -D)` 与上色 patch（`ToolSuccess.Structured`；`write` 的输出无 patch，
    新建文件只在 `/diff` 里出现）。
  - `/diff` 在**本轮开始/结束各 `Client.Capture()` 一次**再调 `Client.Diff`。**不从 step 事件取快照**：v2 core 在 provider
    失败/中断时不发 `step.ended`（TS 同样有 `if (stepSettlement && !hasProviderError())` 守卫），那样会漏掉本轮改动。
- 机制（与 v2 core 一致，不是自造格式，底层就是 **git index + tree 对象**）：
  1. 在 `<global.data>/snapshot/<projectID>/<hash(worktree)>` 建**影子 git 仓库**，`repo.create` 时把**源仓库的 `index`
     复制过去**；
  2. `capture` 对 scope 执行 `git add --all --sparse`（刷新**影子 index**；忽略项用**源仓库** `git check-ignore --no-index`
     过滤，>2MiB 的 untracked 跳过）后 `git write-tree` 得到内容寻址的 tree id；
  3. `files`/`diff` 即 `git diff --name-only` / `git diff` 两个 tree id；`preview`/`restore` 用临时 `GIT_INDEX_FILE` +
     `read-tree`/`update-index`/`checkout-index`。
  全程**不改用户的工作区/暂存区**；因此 diff 只覆盖被 refresh 的 scope 内的改动。Go 的 `core/snapshot.go`/`core/git.go` 与
  TS 的 `snapshot.ts`/`git.ts` 逐行对应。
- 局限：快照 scope 是 **location 目录**（`-C examples/chat` → 只覆盖该目录下的改动）；非 git 项目无 diff。
- 测试：`TestMapStepEndedFiles`、`TestMapToolSuccessStructured`、`TestClientCapture`、`TestClientDiffNoSnapshot`、
  `TestToolDiffFiles`、`TestWriteDiffs`、`TestDiffCommandNoSnapshot`。

### 2.18 revert / undo / redo — 中 ✅ 已实现（含小幅 SDK 改动）

- SDK：新增 `Session.RevertStage(messageID) (*schema.RevertState, error)`、`Session.Revert() *schema.RevertState`、
  `Session.RevertClear() error`、`Session.RevertCommit() error`，对齐 v2 core 的 `Session.Interface.revert.{stage,clear,commit}`。
  底层复用已有的 `core/session/revert.go`（按 assistant 消息里的 `Snapshot{Start,End,Files}` 做分阶段回滚 +
  `session.revert.*` 事件）与 `core.Snapshot`。
- 示例（`examples/chat/revert.go`）：与 TUI 一致，边界**从会话状态读取**（`Session.Revert()`，因此跨 resume 保留），
  而非本地跟踪：
  - `/undo` → 取**边界之前的上一个 user 消息**为界调 `RevertStage`（无边界时取最后一个 user 消息），把其后的文件改动
    恢复到编辑前、打印 diff，并回显被回退消息的文本（TUI 是回填到输入框）；**可连续回退**。
  - `/redo` → 边界**前移到下一个 user 消息**（`RevertStage(next)`）；已是最新时 `RevertClear` 整体复原。**可逐步前进**。
  - `RevertCommit` 已暴露但示例未接线（与 TUI 一致，TUI 也不调 `commit`）。
- 局限：只回滚**文件**；不会从模型上下文剔除已 revert 的消息（TUI 靠 `session.revert.messageID` 隐藏消息）。
- 测试：`TestUndoCommandNothingToUndo`、`TestRedoCommandWithoutSession`、`TestRedoCommandNothingToRevert`、
  `TestUserMessageStepping`、`TestRevertDiffFiles`（端到端 undo/redo 临时验证通过）。

### 2.19 rename / 改标题 — 中 ✅ 已实现（含小幅 SDK 改动）

- SDK：新增 `Session.SetTitle(title) error`。实现发布 `session.next.title.updated` 事件（投影器把 `session.title` 落库），
  与内部自动标题生成（`core/session/runner/llm.go` 的 `publishTitle`）走同一机制；创建时另可用 `SessionConfig.Title`。
- 示例（`examples/chat/rename.go`）：`/rename <title>` 调 `Session.SetTitle`；新标题由 `TitleUpdated` 事件渲染
  （`render.go` 的 `writeTitle`，`[title] ...`），命令本身不重复回显。
- 测试：`TestRenameCommandSetsTitle`（`ListSessions` 反映新标题）、`TestRenameCommandRequiresTitle`、
  `TestSessionSetTitle`（SDK：落库 + 发 `TitleUpdated`）。

### 2.20 fork session — 中 ✅ 已实现（含 SDK 改动）

- SDK：新增 `Client.ForkSession(ctx, sessionID, messageID) (*Session, error)`。新建一个顶层会话（保留源会话的
  agent/model，标题 `<原标题> (fork #N)`，已有后缀则 N+1），再把源会话的消息复制过去。
- 底层 `core/session/fork.go` 的 `Fork(db, source, target, messageID)`：按 `seq` 读源消息，逐个用
  `schema.NewMessageID()` 生成新 id、`seq` 重排，事务内写进目标会话的 `session_message`；`messageID` 非空时只复制
  该消息**之前**的历史（分支/重试用），未知 id 则整段复制。v2 消息是「一条消息一行 JSON」且**无 `parentID`/
  `tail_start_id` 交叉引用**（v1 的这两个字段在 v2 已移除），故复制无需重映射引用，只需对 synthetic 消息改写内嵌
  `sessionID`。
- 与 V1 的差异：V1 `fork` 只传 directory/path/workspace/title/metadata，新会话的 agent/model 落回默认；本实现
  **保留源会话的 agent/model**（更实用）。
- 示例（`examples/chat/fork.go`）：`/fork` 整段复制并切到新会话；`/fork #n` 在**第 n 条 user 消息之前**分叉
  （丢弃该消息及其后），用 `Client.ForkSession` + `userMessageIDs` 解析边界。
- 测试：`TestForkedTitle`、`TestForkSessionCopiesHistory`（内容一致、id 全新、源不变、标题后缀）、
  `TestForkSessionBoundary`（边界截断）、`TestForkSessionMissing`；`examples/chat` 的 `/fork` 切换/选择器/无会话。

### 2.21 异步权限 / 问题（List/Reply/Reject）— 中 ✅ 已实现（含 SDK 改动）

- 背景：v2 core 的 `Permission`/`Question` 是**事件驱动 + pending/Deferred** 模型（`ask`/`assert`/`reply`/`reject`/
  `list`）。此前高层只提供同步回调（`OnPermissionRequest`/`OnQuestion`）；问题路径甚至**没走** `core.Question.Ask`
  （`question.go` 直接调 handler），pending 与事件被完全旁路。
- SDK：
  - `Client.Permission()` / `Client.Question()` 暴露 pending 面（`List`/`ForSession`/`Get`/`Reply`/`Reject`）。
  - `Client.HoldInteractions(on)` 切换 **hold 模式**：`permissionHandler.Assert` / `questionHandler.Ask` 改为调用 core 的
    阻塞 `Assert`/`Ask`，请求进入 pending 并发布事件，直到外部 reply/reject 才解除（handler 仍保留，用于关掉 hold 时）。
  - 新事件：`PermissionAsked`/`PermissionReplied`、`QuestionAsked`/`QuestionReplied`/`QuestionRejected`（payload 是
    结构体或 map，映射时 JSON 往返）。
- 示例（`examples/chat/async.go`）：`/async [on|off]` 开关；hold 模式下事件渲染为 `[permission] #n ...` /
  `[question] #n ...`，用 `/permissions`、`/allow|/always|/deny <#n>`、`/questions`、`/answer <#n> <text>`、
  `/reject <#n>` 解除。turn 中键入的回复命令由 watcher 路由（本会当 steering），其余仍为 steering。
- 测试：SDK 的 `TestHoldPermissionRoundTrip`、`TestHoldQuestionRoundTrip`、`TestHoldQuestionReject`；chat 的
  `/async` 开关、`/permissions`/`/questions` 渲染与选择器错误。

---

## 3. 需先补 SDK 接口（当前无法演示）

| 能力 | 缺口 |
|---|---|
| 用户工具进度 | `ToolContext` 只有 `SessionID`/`ToolCallID`，无 Progress（`tool.go:16`） |
| A2A | 仅 `docs/DESIGN-A2A.md` 设计稿 |

> **非目标（不做）**：`share` / `unshare` —— 会话分享是托管服务 + OAuth 账号 + 网络同步（V1 `share/`，依赖
> `Account`/`EventV2Bridge`/`InstanceState`），v2 core 只有数据模型（`session_share` 表、`share_url` 列），无
> service / 事件，属 CLI 基础设施，不在 SDK 范围（理由见 `docs/DESIGN.md`）。

---

## 4. 优先级建议

| 优先级 | 选题 |
|---|---|
| 高 | SessionHooks、Steer、权限 Always、会话级模型切换、事件渲染补齐 |
| 中 | 运行时注册 agent、SystemPrompt 模式、MCP prompts/resources、git reference、类型化工具 |
| 低 | CleanupToolOutput、ToolOutput 限长、modes、Locale、model variants |
