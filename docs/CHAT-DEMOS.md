# examples/chat demo backlog

> Status: **draft, not yet scheduled**
> Scope: catalogue which go-agent-sdk capabilities `examples/chat` can still demonstrate (topics and entry points only, no implementation)
> Relationship: the public API is defined by `docs/DESIGN.md`; this document is only the demo checklist for `examples/chat`

## Change log

| Date | Change |
|---|---|
| 2026-10-02 | First draft: current-state inventory, candidate demos, SDK interfaces that must be added first, priorities |

---

## 1. Current state (already demonstrated)

Session management `/new` `/sessions` `/delete` (including resume), `/history`, `/compact`, `/persona` (SystemPrompt append),
`/reference` (local), `/tool` (runtime registration), `/skills` `/agents` `/mcps` `/task`,
permission interaction, question interaction, model catalog + token limits, automatic compaction, automatic titles,
usage/cost, reasoning stream, subagent nesting, SessionHooks (see 2.1), `-C`/`--yolo`.

---

## 2. Demonstrable with example-only changes

### 2.1 SessionHooks (lifecycle and tool hooks) — high ✅ implemented

- API: `SessionConfig.Hooks` (`session.go:40`), `SessionHooks` (`hooks.go:12`)
- Implementation: `examples/chat/hooks.go`'s `newSessionHooks` is attached to every newly created/resumed session (inherited by task child sessions). The startup banner explains it;
  `[hook]` lines log the lifecycle; `OnSessionStart` injects the current date; `?<text>` is rewritten into a terse answer; `bash rm -rf` is rejected by `OnPreToolUse`.
  (Original entry points, for reference)
  - `OnSessionStart` → `AdditionalContext` injects the current date
  - `OnUserPromptSubmitted` → expand `!cmd` / `@file` prefixes
  - `OnPreToolUse` → intercept dangerous bash or rewrite arguments
  - `OnPostToolUse` / `OnPostToolUseFailure` → append context
  - `OnToolDefinition` → hide a tool
  - `OnErrorOccurred`
- Value: the broadest coverage; nearly every hook is immediately observable in the REPL.

### 2.2 Steer (mid-run redirection) — high ✅ implemented

- API: `Session.Steer` (`session.go:138`)
- Implementation: text typed while a turn is running shows `steer: <draft>` in the status bar; Enter feeds it into the current turn via `Session.Steer`
  (`watchInterrupt` gained a steer callback and escape-sequence handling, see `examples/chat/turn.go`); Esc/Ctrl+C interruption behavior is unchanged.

### 2.3 Permission Always / persistence — high ✅ implemented

- API: `PermissionDecision.Always`, `PermissionRequest.Save`
- Implementation: `examples/chat/permission.go` has a three-stage interaction — `Allow once / Allow always / Reject`;
  always first shows a confirmation (listing the patterns to be remembered), reject collects an optional reason; aligned with opencode's interaction and wording.
  **`Allow always` is selected by default** (`Choice [2]`, to make the demo easier; upstream defaults to `Allow once`).
- Persistence: `Always` rules are written by the SDK to the project's `permission` table (aligned with V2 core's `PermissionSaved`),
  **retained across restarts**; the confirmation wording therefore says "until the rule is removed (saved for this project)".
  (Note: the TS TUI wording "until OpenCode is restarted" corresponds to V1 permissions' **in-memory** implementation — `InstanceState.approved`,
  which differs from Go's DB-backed behavior, so it is not copied here.)

### 2.4 Per-session agent / model switching — high ✅ implemented

- API: new `Session.SetModel` and `Session.SetAgent` (`session.go`), aligned with V2 core's
  `V2Session.switchModel` / `switchAgent` (`packages/core/src/session.ts:402`, `:393`):
  publish the existing `session.next.model.switched` / `session.next.agent.switched`, update the session row, take effect on the next turn.
- Implementation:
  - `/model [<id>]` — with no argument, lists **available** models; `/model <id>` switches the model for the **current session**.
  - `/agent [<id>]` — with no argument, lists switchable primary agents; `/agent <id>` switches the agent for the **current session**.
  Neither **creates a new session**.
- **Model catalog (in-code manifest)**: `examples/internal/util/catalog.go` describes each model's
  provider/protocol/baseURL/apiKeyEnv/limits/pricing in one entry list, and derives a `ModelInfoProvider` + `CredentialProvider`
  injected into the client. The protocol comes from `ModelInfo.API.Protocol`, the credential from the providerID, so **one client can route to several providers**;
  each entry uses its own `APIKeyEnv`/`BaseURL`, and only models whose API key is set are listed. `chat` and `tool` share this catalog,
  and the model is passed to `util.Client` via `util.WithModel`.
- **Persisting the last selection**: `examples/chat/state.go` writes the last selected model/agent to `{ExampleDir}/.data/state.json`;
  startup precedence is **last selection > catalog default** (`Default: true`, or the first available if unavailable) —
  last selection wins, so switching survives restarts (the TUI's `model.json` uses `config.model > recent`; here persistence is deliberately given priority).
- Removed the originally planned `/sandbox` (`SessionConfig.Permissions`) and `/session-tool` (`SessionConfig.Tools`):
  the former is non-mainstream (permissions should be configured by agent/config rules), the latter duplicates `examples/tool`.
- Note: `/persona` already demonstrates `SessionConfig.SystemPrompt`.

### 2.5 Completing event rendering — high ✅ implemented

- Implementation (`examples/chat/render.go`):
  - `TitleUpdated` → `[title] <title>`.
  - `StepStarted / StepEnded / StepFailed` → print a `[step N]` separator for multi-step turns; abnormal finishes (other than `stop`/`tool-calls`)
    are reported separately; failures print `[step N failed] ...`.
  - `Retried` → `[retry N] ...`; `ShellStarted / ShellEnded` → `[shell] $ <cmd>` + output.
- Caveat: `Retried` and `ShellStarted / ShellEnded` have **no producer** in either the Go SDK or V2 core (nowhere is `Publish` called),
  so these render branches currently never fire; adding producers is an SDK-side change and contradicts "align with V2", as noted in the
  `events.go` / `schema/session_event.go` comments.

### 2.6 Registering an agent at runtime — medium ✅ implemented

- Implementation: `/agent add <name> [<description>]` (`examples/chat/agent.go`) calls `Client.RegisterAgent`, registering a
  hidden subagent (`Mode=subagent`, `Hidden`, `Steps=2`, allow-all permissions, generated system prompt).
- Effect: `/agents` lists it immediately (`task.go`'s `SubagentTypes` only filters out primary/denied, not hidden);
  the model can delegate to it with `task`, and `Steps=2` demonstrates the step limit.
- Protection: refuses if the name already exists (`RegisterAgent`'s `draft.Update` **overwrites in place** an existing agent, including the built-in build).
- Note: `AgentConfig.Model` is not exposed by the command (focus on Mode/Hidden/Steps/Description/System).

### 2.7 SystemPrompt mode and sections — medium ✅ implemented

- API: `SystemPromptConfig.{Mode,Content,Sections}`, `SectionOverride` (`systemprompt.go:11`, `:50`)
- Implementation:
  - `/persona [--replace] <text>` (`examples/chat/persona.go`): `append` (default) or `replace`.
  - `/prompt add|replace|remove <section> [<text>]` (`examples/chat/systemprompt.go`): `customize` +
    a single `SectionOverride`; built-in ids `agent_system`/`instructions`, other names are custom sections (rendered between agent_system and
    instructions, sorted by id).
  - Both share `startSystemPromptSession`; both **create a new session** and `replaceSession`.
- Limitation: `SystemPrompt` only takes effect at creation time and is not persisted (`session.go:27`, `client_session.go:46`), so every change is a new session;
  additionally `Sections` only affects the agent system part and cannot touch the runtime `systemcontext` baseline.
- Testability: the composed prompt has no public read interface, so the example tests only verify the command wiring and new-session creation.

### 2.8 Protocol / variant — medium ✅ implemented (variant part not done)

- Protocol routing: in `examples/internal/util/catalog.go`'s catalog, the same DeepSeek model is listed once per protocol, with base URLs matching each
  protocol's endpoint and the request model name uniformly `deepseek-flash` (`ModelEntry.Model`; `ID` is only a catalog selector):

  | catalog id | protocol | base URL | final endpoint |
  |---|---|---|---|
  | `deepseek-flash` | `openai-compatible-chat` | `https://api.deepseek.com` | `/chat/completions` |
  | `deepseek-flash-responses` | `openai-responses` | `https://api.deepseek.com` | `/responses` |
  | `deepseek-flash-anthropic` | `anthropic-messages` | `https://api.deepseek.com/anthropic` | `/messages` |

  With just `DEEPSEEK_API_KEY` you can demonstrate three routes (each uses its own `BaseURL`, credentials are taken by provider `deepseek`).
  `FromCatalogModel` (`core/session/runner/model.go:138`) looks up the protocol from `ModelInfo.API.Protocol` and takes
  credentials by providerID; the `/model` from 2.4 can switch. This item also adds **visibility**: the `/model` list shows `id (provider · protocol)`.
- variant: **deliberately not done**. The `schema.ModelVariant` + `ModelRef.Variant` mechanism exists (`runnerModelWithVariant`,
  `core/session/runner/model.go:83`), but it only changes the request body/header and is invisible in the chat UI; `temperature`-style demos are thin,
  and reasoning-style demos depend on provider support and are prone to 400s. So it is not demonstrated in the example; this note is all.

### 2.9 MCP remote server / prompts / resources — medium ✅ implemented

- New `examples/notesmcp`: a small, independently runnable MCP server (stdio by default; `--http <addr>` uses streamable HTTP).
  It provides 4 tools (`note_save`/`note_get`/`note_delete`/`note_list`), 2 prompts (`notes_digest`/`action_items`),
  the resource `notes://index`, the template `notes://note/{id}` and instructions; notes are persisted to a JSON file.
- chat uses it as **remote (HTTP)** by default: `run()` spawns it with `go run ./examples/notesmcp --http 127.0.0.1:<free>`
  as a child process, polls until ready, then registers `ServerConfig{Type:"remote"}`, and kills it by **process group** on exit (`examples/chat/notesmcp.go`
  plus `_unix`/`_windows` variants). stdio is hosted by the SDK's `CommandTransport` and needs no handling by chat, so it is not wired up yet.
- Commands: `/mcp prompts | prompt <name> [k=v ...] | resources | resource <uri>` to list/read; `/mcps` now also shows
  each server's instructions.
- Note: `ServerConfig` static auth uses `Headers` (no OAuth); "when there is no MCP config, `rt.mcp == nil`" no longer occurs because the notes server
  is present by default.

### 2.10 Reference git source / hidden — medium ✅ implemented (hidden part not done)

- Implementation: `/reference add-git <name> <repo> [<branch>]` (`examples/chat/references.go`) registers a `ReferenceGitSource`;
  the SDK immediately computes the cache path and **shallow-clones in the background** (git `--depth 100`) into `<Root>/repos/...`, and the model can read that directory on the next turn.
- The `/reference` list shows `[git] cloning...` / `[git] ready` for git sources (based on whether the cache directory already exists), and `[local]` for local ones.
- Because `materialize` **silently skips** "non-remote / invalid branch", the command verifies the materialized list after adding and reports `was not accepted` on failure.
- **hidden not done**: `ReferenceInfo.Hidden` is **not consumed** in either Go's or TS's guidance (the visibility criterion is `Description != nil`,
  and `adapters.go` even drops Hidden), so there is no behavior to demonstrate; hence excluded.
- Note: `file://` sources are skipped because `RepositoryIsRemote` treats them as non-remote, so git demos must use a genuine remote repository.

### 2.11 Plugin events / Options / Tools — medium ✅ implemented

- Implementation (`examples/chat/plugin.go`): registers two plugins via `ClientOptions.Plugins` (`util.WithPlugin`):
  - `chat-greeter`: `Options` provides a greeting; `PluginContext.On` subscribes to count `ToolCalled`; `PluginContext.Tools.Register`
    registers the tool `plugin_greet` (reads `Options` and reports the count); cleanup unsubscribes + unregisters.
  - `chat-permission`: migrates the original `addBashAskRule` from the low-level `client.Plugin().Add` to the high-level `Plugins` (`Agents.Transform`).
- Command: `/plugin remove <id>` unloads via `client.Plugin().Remove` and triggers cleanup (`[plugin] ... unloaded`, and the tool disappears immediately).
  Because `core.Plugin` has no query interface and `Remove` does not error on unknown ids, the example keeps its own list of loaded ids (`pluginRegistry`), and unknown or
  already-removed ids report `not active`.
- Note: **event subscription / runtime tools / agent transforms are already demonstrated by other APIs** (`Client.On`, `/tool`→`Client.RegisterTools`,
  the permission plugin); 2.11 adds the **high-level `ClientOptions.Plugins` registration path + `PluginConfig.Options` declarative configuration +
  cleanup lifecycle**.
- Limitation: high-level `Options`/`Tools`/`On` are only available in **startup-time `ClientOptions.Plugins`** (injected by `adaptPlugin`);
  `client.Plugin().Add` is the low-level `core.PluginContext` (no `Options`/`Tools`/`On`), so at runtime you can only Remove, not Add
  another high-level plugin.

### 2.12 Typed tool + permission action — medium ✅ implemented

- Implementation (`examples/chat/convert.go`): `/tool add convert_length` registers the typed tool `convert_length`:
  - Parameters use a **hand-written schema AST** (`jsonschema.Object` + `Required` + `Enum`) rather than reflection, so `from`/`to` are enums
    (mm/cm/m/km/in/ft/yd/mi) and `value` is required; the handler still uses `DefineTool`'s typed struct (overriding `Parameters` at registration).
  - `Permission: "convert_length"`, and `permissionPlugin` adds the rule `{convert_length,*,ask}`; when not `--yolo`, calls go through
    `Allow once/always/reject` interactive approval.
  - Contrast tool `list_length_units`: same action but `SkipPermission: true`, never approved.
- `/tool`'s empty-list hint now **derives** available tool names from the registry (`runtimeToolNames`) instead of hardcoding `now | dice`.
- Note: `convert_length` replaces the originally planned `weather` (chat already has a weather MCP, avoiding naming/functionality conflicts); enums cannot be
  generated by `DefineTool` via reflection, so `Parameters` is overridden (built-in tools are likewise hand-written ASTs; see `core/tool/webfetch.go`'s `jsonschema.Enum`).

### 2.13 Tool output truncation + cleanup — low ✅ implemented

- Truncation: keeps the SDK default (2000 lines / 50KiB); adds the runtime tool `emit_lines{count}` (`examples/chat/tooloutput.go`).
  After `/tool add emit_lines`, use a large count (e.g. 5000) to trigger truncation — over-limit content is spilled to `<Root>/data/tool-output/tool_*`,
  and the model sees a preview of head + `... output truncated; full content saved to <path> ...` + tail. The global limit is not changed, to avoid affecting other tools.
- Cleanup: `/tool-output` shows the managed directory and file count; `/tool-output cleanup` calls `Client.CleanupToolOutput()`. Because that API
  **only deletes files older than 7 days and returns no count**, the example counts directory files itself and reports `scanned N, removed M` (new files are not deleted).
- Note: the command is named `/tool-output` (not a generalized `/cleanup`); the directory path is derived by the example from the `core.Global` layout (the SDK has no accessor).

### 2.14 Config `modes` files / Locale — low ✅ implemented (Locale env only)

- modes: new `examples/chat/.config/mode/planner.md` → a **primary agent** (`{mode,modes}/*.md` is loaded by `defs/config`,
  `Mode` forced to `primary`), listed and switchable under **`/agent`** (not `/agents`); only `{agent,agents}/**/*.md` are subagents and go under `/agents`.
- Locale: `ClientOptions.Locale` is not set, so env is used (`util.LocaleTag` parsed from `LC_ALL`/`LC_COLLATE`/`LANG`). It does locale-aware
  ordering, affecting the model-side guidance (advertised subagent types, skills/reference guidance) and chat's `/agents`, `/mcps`. Locale-aware collation of
  mixed-script subagent names is locked by `TestSubagentLocaleOrdering`.
- Note: `/agent` (the primary list) uses `sort.Strings` and does not use locale; the example does not expose `ClientOptions.Locale` (env only).

### 2.15 File/image attachments — high ✅ implemented (with minor SDK changes)

- SDK: `MessageOptions` gained `Files []schema.PromptFileAttachment` (**message-level** attachments); `Session.run` → `runtime.admit`
  passes `schema.Prompt{Text, Files}` through → the user message's `Files` → `llmMedia` → `MediaPart` (the protocol layer already supports anthropic image /
  openai `image_url` / `input_image`). The low-level pipeline already existed; the gap was only in `MessageOptions` and `admit`.
- Example (`examples/chat/attach.go`): `/attach <path>` reads an image → infers MIME (png/jpeg/gif/webp only) → encodes
  `data:<mime>;base64,...` (the SDK's `ValidateMedia` only accepts base64/data URLs and **does not read paths**) → sent with the **next** message and then cleared;
  `/attach` lists, `/attach clear` clears.
- Limitation: images only; decoded ≤20MB / encoded ≤28MB. `deepseek-flash` supports vision (OpenAI-compatible `image_url` data URL,
  matching the payload produced by the `openai-compatible-chat` protocol); verified in practice.
- Tests: `TestSessionSendAttachesFiles` (file→media part), `TestAttachCommand`/`TestAttachRejectsUnsupportedType`.

### 2.16 Todo list (event) — medium ✅ implemented (with minor SDK changes)

- SDK: new high-level event `agent.TodoUpdated{SessionID, Todos}` (`events.go`), mapping `todo.updated` into it. The publisher of that event
  emits a **struct rather than a map**, so the mapping uses a JSON round-trip to decode. Aligned with v2 core's TUI — the TUI also subscribes to `todo.updated`.
- Example: `render.go` prints `[todo] <status-mark> <content>` on `TodoUpdated` (☐/▶/☑/✕); `/todos` prints the event-cached
  current list.
- Note: only the **event** was added, not a read interface (`Session.Todos()`); `/todos` reads the event-cached copy, empty if `todo.updated` was never received.
- Tests: `TestClientOnReceivesTodoUpdated` (todowrite→event end-to-end), `TestMapTodoEventStructPayload`, `TestTodosCommand`.

### 2.17 Snapshot / diff — medium ✅ implemented (with SDK changes)

- SDK:
  - `Client.Capture() (string, bool)` (current snapshot id; false if snapshots are unsupported/failed) and
    `Client.Diff(from, to string) ([]core.FileDiff, error)` (the runtime retains `*core.Snapshot`; nil for empty id/unsupported).
  - High-level `agent.StepEnded` gained `Files []schema.RelativePath`; `agent.ToolSuccess` gained `Structured any` (structured tool output,
    including `files[].patch`).
- Example (`examples/chat/diff.go`):
  - At the end of every step, print `[files] N changed: ...` (`StepEnded.Files`).
  - On successful `edit`/`apply_patch`, print `[diff] path (+A -D)` with a colored patch (`ToolSuccess.Structured`; `write`'s output has no patch,
    and new files only appear in `/diff`).
  - `/diff` calls `Client.Capture()` once at the **start and end of the current turn**, then `Client.Diff`. It **does not take snapshots from step events**: v2 core does not emit
    `step.ended` when the provider fails/is interrupted (TS has the same guard, `if (stepSettlement && !hasProviderError())`), which would miss this turn's changes.
- Mechanism (consistent with v2 core, not a custom format; underneath it is **git index + tree objects**):
  1. Create a **shadow git repository** at `<global.data>/snapshot/<projectID>/<hash(worktree)>`, and when the repo is created, copy the **source repository's `index`** over;
  2. `capture` runs `git add --all --sparse` over the scope (refreshing the **shadow index**; ignores are filtered with the **source repository's** `git check-ignore --no-index`,
     skipping untracked files >2MiB) and then `git write-tree` to get a content-addressed tree id;
  3. `files`/`diff` are `git diff --name-only` / `git diff` between the two tree ids; `preview`/`restore` use a temporary `GIT_INDEX_FILE` +
     `read-tree`/`update-index`/`checkout-index`.
  Throughout, the user's **working tree/staging area is never modified**; consequently diff only covers changes within the refreshed scope. Go's `core/snapshot.go`/`core/git.go` correspond
  line-by-line to TS's `snapshot.ts`/`git.ts`.
- Limitation: the snapshot scope is the **location directory** (`-C examples/chat` → only covers changes under that directory); non-git projects have no diff.
- Tests: `TestMapStepEndedFiles`, `TestMapToolSuccessStructured`, `TestClientCapture`, `TestClientDiffNoSnapshot`,
  `TestToolDiffFiles`, `TestWriteDiffs`, `TestDiffCommandNoSnapshot`.

### 2.18 revert / undo / redo — medium ✅ implemented (with minor SDK changes)

- SDK: new `Session.RevertStage(messageID) (*schema.RevertState, error)`, `Session.Revert() *schema.RevertState`,
  `Session.RevertClear() error`, `Session.RevertCommit() error`, aligned with v2 core's `Session.Interface.revert.{stage,clear,commit}`.
  Underneath it reuses the existing `core/session/revert.go` (staged rollback based on the `Snapshot{Start,End,Files}` in assistant messages +
  `session.revert.*` events) and `core.Snapshot`.
- Example (`examples/chat/revert.go`): consistent with the TUI, the boundary is **read from session state** (`Session.Revert()`, so it survives resume),
  rather than tracked locally:
  - `/undo` → take the **previous user message before the boundary** as the boundary and call `RevertStage` (or the last user message if there is no boundary), restoring the file changes after it
    to before the edit, printing the diff, and echoing the reverted message's text (the TUI refills it into the input box); **can undo repeatedly**.
  - `/redo` → move the boundary **forward to the next user message** (`RevertStage(next)`); when already at the latest, `RevertClear` restores everything. **Can step forward.**
  - `RevertCommit` is exposed but not wired in the example (consistent with the TUI, which also does not call `commit`).
- Limitation: only **files** are rolled back; reverted messages are not removed from the model context (the TUI hides messages via `session.revert.messageID`).
- Tests: `TestUndoCommandNothingToUndo`, `TestRedoCommandWithoutSession`, `TestRedoCommandNothingToRevert`,
  `TestUserMessageStepping`, `TestRevertDiffFiles` (end-to-end undo/redo verified manually).

### 2.19 rename / change title — medium ✅ implemented (with minor SDK changes)

- SDK: new `Session.SetTitle(title) error`. It publishes a `session.next.title.updated` event (the projector persists `session.title` to the DB),
  using the same mechanism as internal automatic title generation (`core/session/runner/llm.go`'s `publishTitle`); `SessionConfig.Title` can also be used at creation time.
- Example (`examples/chat/rename.go`): `/rename <title>` calls `Session.SetTitle`; the new title is rendered by the `TitleUpdated` event
  (`render.go`'s `writeTitle`, `[title] ...`), and the command itself does not echo it again.
- Tests: `TestRenameCommandSetsTitle` (`ListSessions` reflects the new title), `TestRenameCommandRequiresTitle`,
  `TestSessionSetTitle` (SDK: persists + emits `TitleUpdated`).

### 2.20 fork session — medium ✅ implemented (with SDK changes)

- SDK: new `Client.ForkSession(ctx, sessionID, messageID) (*Session, error)`. Creates a new top-level session (retaining the source session's
  agent/model, title `<original title> (fork #N)`, or N+1 if it already has the suffix), then copies the source session's messages over.
- Underneath, `core/session/fork.go`'s `Fork(db, source, target, messageID)`: reads source messages by `seq`, generates a new id for each with
  `schema.NewMessageID()`, reorders `seq`, and writes into the target session's `session_message` within a transaction; if `messageID` is non-empty it copies only the history
  **before** that message (for branching/retrying), and for an unknown id it copies the whole range. A v2 message is "one row of JSON per message" and has **no `parentID`/
  `tail_start_id` cross-references** (these two v1 fields were removed in v2), so copying needs no reference remapping, only rewriting the embedded
  `sessionID` for synthetic messages.
- Difference from V1: V1 `fork` passes only directory/path/workspace/title/metadata, and the new session's agent/model fall back to defaults; this implementation
  **retains the source session's agent/model** (more useful).
- Example (`examples/chat/fork.go`): `/fork` copies the whole conversation and switches to the new session; `/fork #n` forks **before the n-th user message**
  (discarding that message and everything after), using `Client.ForkSession` + `userMessageIDs` to resolve the boundary.
- Tests: `TestForkedTitle`, `TestForkSessionCopiesHistory` (identical content, brand-new ids, source unchanged, title suffix),
  `TestForkSessionBoundary` (boundary truncation), `TestForkSessionMissing`; `examples/chat`'s `/fork` switching/selector/no-session cases.

### 2.21 Async permission / question (List/Reply/Reject) — medium ✅ implemented (with SDK changes)

- Background: v2 core's `Permission`/`Question` is an **event-driven + pending/Deferred** model (`ask`/`assert`/`reply`/`reject`/
  `list`). Previously the high level only offered synchronous callbacks (`OnPermissionRequest`/`OnQuestion`); the question path did not even go through `core.Question.Ask`
  (`question.go` called the handler directly), so pending and events were completely bypassed.
- SDK:
  - `Client.Permission()` / `Client.Question()` expose the pending surface (`List`/`ForSession`/`Get`/`Reply`/`Reject`).
  - `Client.HoldInteractions(on)` toggles **hold mode**: `permissionHandler.Assert` / `questionHandler.Ask` are changed to call core's
    blocking `Assert`/`Ask`, so requests enter pending and publish events until an external reply/reject resolves them (the handler is retained, used when hold is off).
  - New events: `PermissionAsked`/`PermissionReplied`, `QuestionAsked`/`QuestionReplied`/`QuestionRejected` (the payload is a
    struct or map; a JSON round-trip is used when mapping).
- Example (`examples/chat/async.go`): `/async [on|off]` toggles it; in hold mode events render as `[permission] #n ...` /
  `[question] #n ...`, resolved via `/permissions`, `/allow|/always|/deny <#n>`, `/questions`, `/answer <#n> <text>`,
  `/reject <#n>`. Reply commands typed during a turn are routed by the watcher (they would otherwise be steering); everything else remains steering.
- Tests: the SDK's `TestHoldPermissionRoundTrip`, `TestHoldQuestionRoundTrip`, `TestHoldQuestionReject`; chat's
  `/async` toggle, `/permissions`/`/questions` rendering and selector errors.

### 2.22 User tool progress (ToolContext.Progress) — low ✅ implemented (with minor SDK changes)

- Background: the low-level `core/tool.Context` has long had `Progress(structured, content)`, and the runner publishes `session.next.tool.progress` from it,
  projecting it to the assistant's running tool state and mapping it to `agent.ToolProgress`; the built-in task (subagent) tool already uses it. But the high-level
  `agent.ToolContext` only had `SessionID`/`ToolCallID`, and `Tool.coreTool` dropped `tctx.Progress`, so user tools could not report progress.
- SDK: `ToolContext` gained `Progress func(structured map[string]any, content []tool.Content) error`, and `coreTool` passes it through unchanged.
- Example (`examples/chat/progress.go`): the new user tool `count_to` (`/tool add count_to`) calls `ctx.Progress({current,total})` at each step;
  `handleProgress` was split into `registerTask` (task metadata) and `writeToolProgress` (generic progress `[progress] k=v ...`).
- Tests: `TestUserToolProgressPassthrough` (a scripted LLM drives the tool call and asserts the event), `TestCountToToolEmitsProgress`,
  `TestClampSteps`, `TestFormatProgress`, `TestHandleProgressRendersUserTool`.

---

## 3. SDK interfaces that must be added first (currently not demonstrable)

| Capability | Gap |
|---|---|
| A2A | design draft only in `docs/DESIGN-A2A.md` |

> **Non-goals (not done)**: `share` / `unshare` — session sharing is a hosted service + OAuth account + network sync (V1 `share/`, which depends on
> `Account`/`EventV2Bridge`/`InstanceState`); v2 core only has the data model (`session_share` table, `share_url` column) with no
> service/events, and it is CLI infrastructure outside the SDK's scope (see `docs/DESIGN.md` for the rationale).

---

## 4. Suggested priorities

| Priority | Topics |
|---|---|
| High | SessionHooks, Steer, permission Always, per-session model switching, completing event rendering |
| Medium | Runtime agent registration, SystemPrompt mode, MCP prompts/resources, git reference, typed tool |
| Low | CleanupToolOutput, ToolOutput truncation, modes, Locale, model variants |
