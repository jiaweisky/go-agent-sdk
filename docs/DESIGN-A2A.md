# go-agent-sdk A2A support design

> Status: **implemented (Phases 0–5 all landed; includes cross-implementation interop tests with ADK-go)**
> Scope: add A2A (Agent2Agent) protocol support to go-agent-sdk (inbound + outbound)
> Relationship: this document is a sub-volume of `docs/DESIGN.md`, covering only A2A; the general architecture, public API and porting rules are still defined by `docs/DESIGN.md` and `AGENTS.md`
> Convention: the Go code snippets in this document are **illustrative only**; they express type boundaries and contracts, not final signatures

## Change log

| Date | Change |
|---|---|
| 2026-09-26 | First draft: mapping spec, inbound/outbound design, roadmap, decision list |
| 2026-10-04 | Added §0 protocol primer (5W1H / data model / state machine / operation flow / differences from MCP) |
| 2026-10-04 | Outbound convergence: removed the separate "tool form"; a remote agent is uniformly "an agent with an A2A backend", surfacing via `task` as a subagent and usable as a primary agent (§2.1 / §3.2 / §6.1) |
| 2026-10-04 | Dependency finalized: support only the single version `a2a-go/v2` (D4); adapter layer `a2a/a2ago/`, escape hatch `WithNativeOptions`; no dependency on adk-go (§3.1) |
| 2026-10-04 | Unified abstraction finalized (A1/A2/A7 + A8 option C): `turn` is promoted to a core first-class durable entity (`taskId = turn.id`, shared by local/remote subagents); backend dispatch + mirrored child session; reuse the `task` permission action (§3.3 / §4.1 / §5.6) |
| 2026-10-04 | Inbound exposure and security finalized (A3/A4/A5/A6): explicit `Expose`, one card = one agent, multiple agents = multiple card URLs; permissions configurable, default `deny` for dangerous tools; explicit context export (§3.4 / §5.5 / §6.2) |
| 2026-10-04 | SDK-side rename: `PluginEffect` → `PluginFunc`, `PluginConfig.Effect` → `.Func` (removing the last Effect remnants) |
| 2026-10-04 | §4 mapping and interoperability (against adk-go v2 / a2a-go): parts use the `adk_*` convention, metadata prefixes `adk_`/`a2a:`; added §4.7 interop checklist (identity keys, artifact `append`/`lastChunk`, HITL `function_call`, incremental sync) |
| 2026-10-04 | Inbound §5 rework: added §5.0 responsibility boundary (inherit a2a-go; we implement `AgentExecutor` + `taskstore.Store`); auth inherited / authorization custom (§5.5); the single HITL design = park + resume (D1), with rebuild as a fallback after restart; heartbeat inherits `WithTransportKeepAlive` (§5.4 / §5.7) |
| 2026-10-04 | Outbound §6 rework: added §6.0 responsibility boundary (inherit `a2aclient`); `GetTask` / `ListTasks` / `SubscribeToTask` belong to a2a-go; HITL maps to a local `QuestionRequest` (D2) |
| 2026-10-04 | §7–§10 and appendix convergence: Phase 0 = `turn` entity / backend field + drain dispatch / exposure marker; §9 conclusion "no architectural-level risk"; §10 all decisions converged (A1–A8 / D1–D5); allowed to directly edit `core/database/migration.go` with no versioned migrations; appendix A adds HITL encoding / task persistence comparison |
| 2026-10-04 | Implementation landed (Go): `turn` entity/store, backend dispatch (reusing `runner.SessionRunner` + provider-executed tools), outbound `a2ago` + HITL loop + ADK `function_response` encoding, inbound `Executor`/`turnTaskStore`/card/handler, park+resume and restart fallback rebuild, usage/file_diff/tool trace; Phase 5: `WorkspaceResolver`, `ArtifactExternalizer`, `Options.NativeOptions` pass-through. §5.1 added an "implementation" column, §5.5 added auth/workspace landing, §8 added status (§5.1 / §5.5 / §8) |
| 2026-10-04 | Cross-implementation interop test: `examples/adka2a` (an adk-go server, a separate Go module; the SDK does not depend on adk-go) + `examples/chat`'s `TestAdkA2AInterop` (SDK client ↔ ADK server); chat registers a remote `echo` agent (the name describes its role; "accessed over A2A" is recorded by the backend in the agent definition), via `/agent echo` or `task` delegation (§8 Phase 4) |
| 2026-10-04 | Closed out previously unlanded items: inbound attachment spooling (raw/URL part → workspace + prompt path, §4.2/§5.3); outbound cancel/cleanup (Abort → remote `CancelTask`, only non-terminal and not input-required, §6.2); `turn` completion (local turns also persist a turn; `parent_turn_id`/`anchor_seq`/`end_seq`/`input_id` are all written; A2A inbound turns are exclusively owned by the task store, §5.6/A8); inbound steer (append a message to a working task → `SessionDeliverySteer`, §5.3); outbound remote `taskId`/`contextId` persisted to turn metadata and resumed on the next turn (§4.7 item 2) |
| 2026-10-05 | Media and security landed: outbound user media now sent as A2A file parts (raw/URL, §4.2/§6.2); inbound raw parts spool with an extension derived from `MediaType`; the AgentCard declares truthful `defaultInputModes`/`defaultOutputModes`; new `server.Options.Security` enforces a bearer token on the RPC endpoints and declares `securitySchemes` (§5.1/§5.5); new `cmd/a2aserve` command serves the built-in `build` agent from a YAML config + CLI flags (no env) (§5.2) |
| 2026-10-06 | `a2aFactory.ResumeSession` now **merges** the inbound/workspace ruleset over the session's stored rules instead of replacing them (dedupe, idempotent), so an in-process session keeps its own policy while staying constrained by the inbound rules — the same semantics the HTTP API adopts (`a2a_factory.go`, `docs/DESIGN-HTTP.md` §7) |
| 2026-10-10 | Card `skills` semantics corrected: skills are **operator-authored descriptive labels** (`cards.<agent>.skills`, validated to carry `id`+`name`+`description`), **not** a mirror of the agent's tools/subagents/skills — the earlier "skill id must name an existing capability" rule is removed. An agent that configures none advertises an **empty `skills` array** (field always present, `[]` never `null`); the synthesized "self" default skill is dropped (`pkg/a2a/server/card.go`, `cmd/a2aserve/run.go`). |
| 2026-10-15 | Large-file exchange added (`DESIGN-MEDIA` **{M18}** §4.13): any A2A client discovers the server **from its card** (standard), so the per-modality capability model projects onto the card (standard `inputModes`/`outputModes` + a **storage extension** with the ingest contract); a peer may upload out-of-band to the advertised target and send `FileWithUri`, which we resolve locally (our store) or as a peer `url`. §4.2 updated. |
| 2026-10-15 | Inbound attachment spool relocated from the workspace (`<workdir>/.a2a/...`) to the SDK state root (`{Root}/data/a2a-spool/<turnID>/`): a read-only/share lease's working directory **is** the shared source, so spooling into it wrote the "read-only" source and multiple leases collided; under `{Root}` the spool never touches the project, is per-instance, and needs no `.a2a/.gitignore` (already outside the workspace, hence unscanned). The spool dir is pre-approved for reads like tool output and pruned by the same hourly retention sweep (7 days). §4.2 updated. |
| 2026-10-16 | Outbound registration surface: a markdown agent file's frontmatter may now carry a `backend` (`type: a2a` + `a2a.endpoint`/`cardURL`/`headers`), so an A2A peer is declared through `ConfigDirs` — no `RegisterAgent` code. `internal/defs/config.AgentInfo` gains `Backend` and `applyAgentInfo` lands it, matching the §3.2 note that agent definitions from `ConfigDirs` are a registration path. Demonstrated by `examples/chat`, whose remote `echo` agent is now generated as a markdown file instead of registered programmatically. |

---

## 0. A2A protocol primer (read first)

> This section covers **the protocol itself only**, without any of this SDK's trade-offs. Readers unfamiliar with A2A should read this first, then §1 for the background judgments.

### 0.1 What it is

A2A (Agent2Agent) is an open standard that lets **mutually independent, mutually opaque AI agent systems** call each other across processes / hosts / vendors. It reuses existing Web standards (HTTP, JSON-RPC 2.0, SSE), with the goal of letting agents **discover** each other's capabilities, collaborate on tasks, and safely exchange information **without exposing internal state / memory / tools**.

In one line: **A2A is the "agent calls agent" protocol; MCP is the "agent calls tool" protocol** (see §0.6).

### 0.2 5W1H

| Dimension | Content |
|---|---|
| **Why** | Agent systems are heterogeneous (different languages / frameworks / vendors) and need a common language to delegate tasks and exchange context |
| **Who** | **A2A Client** (initiator) ↔ **A2A Server / Remote Agent** (the side exposing an A2A endpoint); either side can be an agent |
| **What** | Exchanges `Message` / `Part` / `Artifact` around a `Task` (a stateful unit of work) |
| **When** | Synchronous request/response, streaming real-time updates, or long tasks asynchronous + push notification + human-in-the-loop |
| **Where** | Three bindings over HTTP: JSON-RPC 2.0, gRPC, HTTP+JSON/REST; endpoints and capabilities are declared in the AgentCard |
| **How** | See §0.5: discover card → send message → advance the task state machine → fetch result / resume / cancel |

### 0.3 Core concepts and data model

- **Agent Card**: JSON metadata published by the server (well-known URI `/.well-known/agent-card.json`), declaring identity, interface URLs, transport bindings, `capabilities` (e.g. whether streaming is supported), `skills`, `securitySchemes`, `defaultInputModes/OutputModes`, `protocolVersion`. **A client fetches the card first to know how to call it.**
- **Message**: one communication turn, with a `role` (`user` / `agent`) and several `Part`s.
- **Part**: the smallest unit of content — text, file reference (URL / raw bytes), structured data.
- **Task**: A2A's core unit of work, with a unique `id` and a lifecycle state, producing `Artifact`s.
- **Artifact**: task output (document / image / structured data), composed of `Part`s.
- **Context**: an optional id that logically groups several related tasks / messages (equivalent to a "session").
- **Streaming / Push Notification**: the two asynchronous delivery mechanisms for task updates (SSE stream / server-to-client webhook callback).
- **Extension**: optional extensions beyond the core protocol (`capabilities.extensions[]`, with a `required` flag).

Identifier semantics (a key landing point for this design; see §4.1):

| Identifier | Semantics |
|---|---|
| `contextId` | Session: the grouping of multiple turns |
| `taskId` | A single **terminal** task (one level finer than a session) |
| `messageId` | A single message |

### 0.4 Task state machine

```
              ┌─────────────────────► canceled (terminal)
              │
submitted ─► working ─┬─► completed (terminal)
   │            │      ├─► failed (terminal)
   │            │      └─► rejected (terminal)
   │            │
   │            ├─► input-required ──(client sends another message)──► working
   │            └─► auth-required  ──(client provides credentials)───► working
   └─(a simple interaction may also return a Message directly, without creating a Task)
```

- **Terminal states**: `completed` / `failed` / `canceled` / `rejected` — a terminal task no longer accepts new messages.
- **Interruptible states**: `input-required` (awaiting more user information) / `auth-required` (awaiting authorization credentials) — exactly A2A's human-in-the-loop primitive, and the part this design values most (§5.4).

### 0.5 Core operations and a typical flow

Binding-agnostic core operations (the table uses JSON-RPC method names):

| Operation | Purpose |
|---|---|
| Send Message | Send a message; returns a `Task`, or a `Message` directly for simple interactions |
| Send Streaming Message | Like Send, but returns status / artifact events as an SSE stream |
| Get Task | Fetch a task and its history by `taskId` |
| List Tasks | List tasks by `contextId` / state (paginated) |
| Cancel Task | Cooperative cancellation |
| Subscribe to Task | Re-subscribe to a task's update stream after a disconnect |
| Push Notification Config (CRUD) | Configure / query / delete a task's webhook callback |
| Get Extended Agent Card | Fetch the authenticated extended card |

A typical (streaming + HITL) round trip:

```
Client                                      Server (Remote Agent)
  │  GET /.well-known/agent-card.json  ─────► │
  │  ◄────────────── AgentCard ──────────────│   discovery
  │                                            │
  │  SendStreamingMessage(message)  ─────────► │
  │  ◄──── Task{state: submitted} ────────────│
  │  ◄──── TaskStatusUpdate{working} ─────────│
  │  ◄──── TaskArtifactUpdate{append} ────────│   streaming output
  │  ◄──── TaskStatusUpdate{input-required} ──│   user input needed
  │                                            │
  │  SendMessage(taskId, answer)  ───────────► │   multi-turn resume
  │  ◄──── TaskStatusUpdate{working} ─────────│
  │  ◄──── TaskArtifactUpdate{lastChunk} ─────│
  │  ◄──── TaskStatusUpdate{completed} ───────│   terminal
```

Variants:

- **Non-streaming**: `SendMessage` blocks until a terminal or interruptible state before returning (default behavior);
- **Asynchronous**: `SendMessage{returnImmediately:true}` returns `working` immediately, after which updates are fetched via `GetTask` polling / `SubscribeToTask` / push notification;
- **Cancel**: the client calls `CancelTask`, and the server transitions to `canceled`.

### 0.6 A2A ≠ MCP

| | A2A | MCP |
|---|---|---|
| Connection | agent ↔ **agent** (peers, each with its own reasoning loop) | agent ↔ **tool / resource** (no autonomous loop) |
| Abstraction | stateful `Task`, multi-turn, HITL | stateless function call / resource read |
| Typical use | delegate a task to another agent | let the model call a tool |

This SDK's outbound path treats a remote agent as an **agent** (subagent / primary agent) rather than a "tool", which is a direct consequence of this distinction (§3.3 / §6.1).

### 0.7 Versions and conventions in this document

The latest A2A is **1.0.0** (previously 0.3.0 / 0.2.6 / 0.1.0). 1.0 contains **breaking changes**: removing the `kind` discriminator field, renaming the state enum to `TASK_STATE_*` (0.3 used lowercase kebab such as `input-required`), and changing the card's interface description to `supportedInterfaces[]` (including `protocolBinding` / `protocolVersion`), etc.

This SDK targets the implementation in `github.com/a2aproject/a2a-go/v2` (§3.1, decided as D4). The main text uses that library's method names (`SendMessage` / `SendStreamingMessage` / `GetTask` / `ListTasks` / `CancelTask` / `SubscribeToTask`); differences in field and enum spellings across A2A versions (0.3 / 1.0) (e.g. `TASK_STATE_*` vs `input-required`, `supportedInterfaces`) are always resolved by the version that library actually binds.

---

## 1. Background and three preliminary judgments

A2A is a protocol for agents to call each other across processes/hosts. The goal is bidirectional support:

- **Outbound (client)**: let the SDK's agents call remote A2A agents;
- **Inbound (server)**: let the SDK's agents be called by A2A clients.

Before starting, establish three judgments; they determine every subsequent design trade-off.

### Judgment one: do not copy adk-go's `adka2a` / `remoteagent`

ADK's premise is "stateless agent + SessionService + reentrant `runner.Run`", so it uses "end this turn with input-required → rerun on the next message" to buy **no in-process state**.

go-agent-sdk's premise is completely different:

- sessions are durable (SQLite, `session` / `session_message` tables);
- externally it is a **union of events** (`agent.Event` + `iter.Seq2`), not a task state machine;
- permissions/questions **block and suspend within a turn** (`core.Permission.Ask` / `core.Question.Ask` block on a channel).

Porting ADK's implementation into this SDK would discard the SDK's most valuable property (durable sessions + suspendable turns), so the **mapping table and event state machine must be redesigned**.

### Judgment two: this SDK already has the ready-made dividend of `input-required`

`core/permission.go` / `core/question.go` already have this shape:

| Existing capability | Location |
|---|---|
| Publish `permission.asked` / `permission.replied` | `schema/permission.go` + `permissionEvents.PublishAsked` |
| Publish `question.asked` / `question.replied` / `question.rejected` | `schema/question.go` + `core/question.go` |
| Block waiting for a reply | `core.Permission.pending` / `core.Question.pending` (`pendingRegistry`) |
| Reply by requestID | `Permission.Reply` / `Question.Reply` / `Question.Reject` |
| Enumerate pending | `Permission.Get/ForSession/List`, `Question.List` |

These are exactly all the primitives A2A's `input-required` + subsequent message recovery needs. ADK has to build its own long-running tool protocol; we only need to **bridge**.

### Judgment three: what really must be solved is the "unified abstraction", not the order of outbound/inbound

The order of directions is only a scheduling matter. What really determines the architecture's success are two questions (see §3.3 / §3.4):

1. **How can remote subagents and local subagents be design-compatible and usage-consistent?** → The answer is to place the difference in "the session's execution backend" + a mirrored child session, rather than forking at the `task` tool layer;
2. **Inbound, how should local subagents be defined and exposed?** → The answer is to explicitly declare **which agents provide service externally**: an exposed agent **each provides a card URL** (one card = one agent).

Once these two are settled, outbound and inbound are merely writing one adapter each on the same abstraction, and precedence does not matter.

---

## 2. Goals and non-goals

### 2.1 Goals

1. **Outbound**: the SDK's agents can call remote A2A agents. The only mechanism is "an agent with an A2A execution backend", which surfaces via the `task` tool as a **subagent** and can be selected as a **primary agent** (§3.3 / §6.1); no separate "tool form" is provided;
2. **Inbound**: the SDK's agents can be called as an A2A server, supporting streaming, multi-turn, `input-required`, cancel, and task re-query;
3. **Fidelity**: part / tool-call / HITL semantics are preserved as far as possible across round trips, supporting SDK↔SDK and **SDK↔ADK-go cross-implementation interop**;
4. **Security**: a remote caller **cannot** implicitly obtain local code-execution capability.

### 2.2 Non-goals

- Do not build opencode CLI's launcher / multi-form entry points (consistent with `AGENTS.md`'s principle of not copying CLI infrastructure);
- Do not build an A2A registry / discovery service;
- Do not introduce OTel / telemetry (the SDK currently has no telemetry, not introduced here);
- Do not invent **additional** capability-negotiation mechanisms beyond AgentCard declarations (no bidirectional capability handshake / downgrade negotiation). Do only minimal adaptation based on the AgentCard's `capabilities` / `defaultInputModes` / `defaultOutputModes` / `protocolVersion`: unknown non-required extensions are safely ignored, and an unsupported `required` extension is rejected (see §6.2);
- Do not port any of opencode's v1/legacy layer code.

---

## 3. Package structure and dependency strategy

A2A has **no counterpart** in opencode TS; it is a Go-specific capability and should be explicitly registered as "no corresponding TS directory" in `AGENTS.md`'s package architecture table, like `jsonschema/`.

```
a2a/                     protocol-agnostic core: types, mapping, state machine (no transport dependency)
a2a/server/              inbound: executor + AgentCard generation + http.Handler
a2a/client/              outbound: card resolution, message sending, A2A event → SDK event conversion; defines the thin interface
a2a/a2ago/               the a2a-go implementation of the outbound thin interface (the only outbound adapter layer that imports a2a-go; replaceable)
```

### 3.1 Dependency strategy

| Decision | Content | Rationale |
|---|---|---|
| Dependency library | **Support only `github.com/a2aproject/a2a-go/v2`** (v2), a single version; not compatible with `a2a-go` v0.x, and not both versions at once | adk-go currently depends on both `a2a-go v0.3.15` and `a2a-go/v2 v2.2.1`; dual-version coexistence is unfriendly to the dependency graph and is a pitfall to explicitly avoid |
| Type isolation | The public API uses the SDK's own types + `iter.Seq2`, never leaking a2a-go types | Follows `mcp/`'s approach of wrapping the official SDK |
| Adapter layer | The thin interface is defined in `a2a/client` (pattern modeled on ADK's `internal/agent/remoteagent.A2AClient`), implemented by `a2a/a2ago/` using a2a-go | The protocol is still evolving; upgrading requires changing only one place |
| Escape hatch | `server.Options.NativeOptions []any` passes through the native configuration of the wrapped library (a2a-go) | Avoid losing the protocol's full capability due to wrapping; the signature uses `any` so a2a-go types do not enter the public API, consistent with "type isolation" |

> **Dependency boundary**: this SDK **does not depend on adk-go** — adk-go is only a reference implementation in §1 / appendix A. `a2a/a2ago/` is the only outbound adapter layer that imports `github.com/a2aproject/a2a-go/v2`; a2a-go types do not cross the boundary of `a2a/a2ago/` and `a2a/server/` into the `a2a/` core and public API (see "type isolation" above).

→ **Decided (D4, §10.2): support only the single version `a2a-go/v2`.**

### 3.2 Positioning relative to existing extension mechanisms

A2A capabilities should **integrate with existing mechanisms**, not start a separate set:

| Existing mechanism | How A2A plugs in |
|---|---|
| `Client.On` / `Session.On` | After remote events are mirrored into the child session, `Client.On` and the child session's `Session.On` can observe them (the parent session's `Session.On` cannot see child sessions, just like a local subagent) |
| `Client.RegisterAgent` / `AgentConfig` (and agent definitions from `ConfigDirs`) | The **primary registration path** for a remote agent: once `AgentConfig` / `schema.AgentInfo` gains an `A2A` backend field, it can be registered as a named agent (enters `SubagentTypes`, can be a primary agent) |
| `PluginFunc` | Plugins contribute/rewrite agents (including remote ones) via `PluginContext.Agents` (`Transform`), and observe remote round trips via `On`; **no separate "remote agent tool" registration surface** |
| `SessionHooks` | Take effect only for **local** `task` calls (the delegation boundary): you can intercept/audit "is a remote agent being called" before delegation. A remote agent's **internal** tool calls do not go through local hooks and can only be observed via A2A events (downgraded to `ToolProgress`, not disguised as `ToolCalled`, see §3.3 / §6.2) |
| `SessionConfig.Permissions` | **Inbound only**: an external call triggers a **local** agent, execution happens locally, so the session-level ruleset injected by caller identity constrains **local tools**. A remote agent's internal tools are not constrained by local rules (§3.3) |
| `core/database` table creation | Add a core first-class **`turn`** entity (shared by local/remote subagents; the A2A `taskId` is `turn.id`, see §5.6). Just edit `migration.go`'s table list |
| `task` tool + `TaskRunner` (`task_run.go`) | Reused as the **entry and local orchestration** for the outbound **subagent form**: the `task` tool, `runTask`, `task_id`, progress, depth, and permission derivation are all unchanged. The real integration point is the lower-level `runner.SessionRunner` **drain dispatch** (§3.3), so neither `task.go` nor `task_run.go` changes; the remote-specific `a2a` permission assertion is appended by the A2A backend at that dispatch point (A7) |

> **Execution-side boundary (consistent with §3.3)**: A2A provides no primitives to control the peer's tools/permissions (opaque execution). **Permissions and hooks take effect only on the execution side**: for inbound the execution side is local → local rules apply; for outbound the execution side is remote → locally you can only control "whether to delegate" and "what context to send", and the remote's internal tool calls are **observable only, not interceptable**.

### 3.3 Unified abstraction: an agent's "execution backend" (backend)

**The question to answer**: how can remote subagents and local subagents be design-compatible and fully usage-consistent?

**Key finding**: the seam already exists; **no new Agent abstraction layer is needed**.

`runtime.buildRunner` injects the coordinator's drain as a "per-session call" function:

```go
r.coord = session.NewCoordinator(func(ctx, id, force) error {
    publishSessionStatus(id, busy); defer publishSessionStatus(id, idle)
    return r.runner.Run(ctx, runner.RunInput{SessionID: id, Force: force})
})
```

And `r.runner` (`*runner.LLMRunner`) satisfies the `runner.SessionRunner` interface (`Run(ctx, RunInput) error`); the runner in turn resolves **which agent to run per session** via the `runner.AgentSelector` interface's `Select(sessionID, id)` (the runtime-side implementation is `sessionAgents`).

In other words: **"which agent to use" and "how to run a session" are already two orthogonal injection points.**

#### Design

Place the agent's difference in "the session's execution backend", and do **not** fork inside the `task` tool or `runTask`:

1. `schema.AgentInfo` gains an optional backend field (e.g. `A2A *AgentA2ABackend`), declaring who executes the agent: a local LLM loop (default, zero value) or a remote A2A one (endpoint / card / auth / mapping policy); the public `AgentConfig` gains the same field, merged by `applyAgentConfig` (`Client.RegisterAgent` registers a remote agent accordingly);
2. runtime's drain becomes a dispatch: **first read the session row to get `session.agent`, then resolve the effective agent via `sessionAgents.Select(sessionID, &agentID)`** (consistent with the runner's internal resolution), and dispatch by its backend — a remote backend goes to `A2ARunner`, otherwise `LLMRunner`. Both Runners implement `runner.SessionRunner`;
3. **A remote subagent still creates a real local child session** (reusing `resolveChildSession`), and remote events are **mirrored** into that child session; this turn of the child session's execution is recorded as one `turn` (with `parent_turn_id` pointing at the parent turn), fully isomorphic to a local subagent (§5.6).

This is called the "**mirrored child session**" model. Usage consistency is not achieved by two parallel implementations "aligning" with each other, but by the two backends **sharing the same session semantics**.

**Clarifying two easily-misunderstood points:**

- A mirrored child session is **not** "running a local LLM inside the child session, which then calls A2A". That would add a layer of reasoning and token cost, and change `task_id` semantics. In a mirrored child session **no local LLM participates**; the child session is just a durable ledger driven by the `A2ARunner` — the conversation actually happens remotely, and the local child session carries its input, events and state.
- "Creating a child session" was never the `task` tool's job anyway; it is `runtime.runTask`'s job (`resolveSubagent` → `resolveChildSession` → `setSessionRegistry` / `inheritSession` / `publishTaskProgress` → `runChildSession`). This design replaces only the **last hop**: the drain's dispatch. Thus `task_id` reuse, and the restoration of an existing session's `Agent` / `Permission`, are all inherited unchanged.

#### Comparison of the two paths

```
task tool
  → RunTask
    → resolveSubagent          // look up the registry: both local and remote agents go here
    → resolveChildSession      // create / reuse the session row: both kinds of agent go here
    → …(registry / hooks / progress, all unchanged)
    → coord.Run(childID)
      → drain(childID, force)
        ├─ agent.Backend == nil   → LLMRunner.Run(childID)   // local: the LLM loop reads/writes this session
        └─ agent.Backend == A2A   → A2ARunner.Run(childID)   // remote: read pending input → send A2A
                                                             //        remote events → mirror into this session
```

The only new branch is the last line; everything else is reused.

#### Invariant checklist

A remote subagent must match a local subagent item by item; all must be tested:

| Invariant | Guaranteed by |
|---|---|
| `SubagentTypes()` lists both local and remote agents, with consistent ordering and description sources | Do not touch `SubagentTypes`; only let agent definitions carry a backend |
| `task` tool input / output schema unchanged | Do not touch `task.go` |
| `TaskOutput.SessionID` is always a local child session ID | Mirrored child session (`resolveChildSession`) |
| `task_id` resume | Local child session reuse; the remote `taskId` is the id of that child session's active `turn` (§5.6) |
| `session.next.tool.progress` fields consistent | Do not touch `publishTaskProgress` |
| `SubagentDepth` limit | Do not touch `checkSubagentDepth` |
| Permission derivation (parent rules → child session) | Do not touch `DeriveSubagentSessionPermission` |
| hooks inheritance / Abort propagation | Do not touch `inheritSession` / `coord.Interrupt`. **Note**: for an A2A child session, hooks have **no actual trigger point** — the remote's internal tools do not execute locally (§3.2); what is retained here is only the mechanism (local/remote share the same session semantics); what actually takes effect is the hook on the local `task` call at the delegation boundary and the Abort→`CancelTask` propagation |
| `Client.On` / `SessionMessages(childID)` can observe the remote conversation | Remote events are mirrored into the DB |
| Error shape (unified as `tool.Failure`) | Converted inside `A2ARunner`. Note the `task` tool's error path produces **only** `tool.Failure` and never `TaskOutput.State="error"` (`task.go` sets `State="completed"` only on success) |

#### Semantic degradations that must differ, and must be documented

| Item | Description |
|---|---|
| Permission scope | Local permissions only cover "whether calling that remote agent is allowed" and "whether local context may be sent out"; they **do not cover the remote's internal tool calls** |
| Path semantics | The remote cannot see the local filesystem → an explicit context-export policy is needed (§6.2 / A6); you cannot rely on stuffing local paths into the prompt |
| Cancel | Remote cancel is cooperative (A2A `CancelTask`) and is not guaranteed to stop immediately |
| usage / billing | Remote usage may be invisible → `StepEnded` / `Usage` events may be missing, and the UI must tolerate that |
| steering | A2A has no equivalent semantics; mapping must be defined separately |

#### An incidental net benefit

The same backend dispatch makes "**a remote agent as a primary agent**" available at zero cost (`CreateSession(Agent: "remote-x")` is enough) — exactly ADK `remoteagent`'s usage, but here it is merely a corollary of the same seam, requiring no extra design.

### 3.4 Inbound exposure: which agents can be called externally

**The problem to solve**: inbound, when acting as an A2A server, **which local agents provide service externally** (each with its own card).

**A2A's model**: one AgentCard describes **one** agent; exposing multiple agents means providing **multiple card URLs** (each with its own endpoint). A card's `skills` is a **descriptive** list of what the agent advertises; entries are **operator-authored** (`cards.<agent>.skills`) and are **not** required to mirror the agent's internal tools/subagents. **Which to list — or none at all — is a choice made when defining the card**; an agent with none advertises an empty `skills` array (the field is always present).

**Why an explicit declaration is needed**: no "default/visibility"-style heuristic can infer "which agents provide service externally". At the very least it cannot express these requirements:

1. "Exposable but not internally delegatable";
2. External-specific descriptions / security policy (redaction);
3. External identity → permission mapping (the card's `securitySchemes` / inbound authorization);
4. Explicitly **non-exposable** items — a heuristic will eventually miss one.

**Approach**: `AgentConfig` / `AgentInfo` gain an explicit exposure declaration (e.g. an `Expose` / `A2A` sub-structure) deciding whether an agent **provides a card externally**, and with what description and security requirements; an undeclared agent provides no card.

#### Multiple agents = multiple card URLs

In A2A, a card describes only **one** agent. For a server to expose N agents externally, it provides **N card URLs** (each with its own endpoint, possibly at different paths). So "inbound exposure" = deciding which local agents each own a card/endpoint; the client chooses an agent by **choosing which card/URL**.

Each agent called directly from outside runs in a **root session** (no parent), and its permission derivation differs from local `task` delegation:

| Dimension | Local delegation (task tool) | External direct call of an exposed agent |
|---|---|---|
| Session structure | A child session with `parent_id` | A root session with no parent |
| Permission source | `DeriveSubagentSessionPermission(parent rules, child agent)` | The agent's own ruleset, defined by the server (**no parent to derive from**); in multi-tenant cases, further narrowed by caller identity |
| Gate | `PermissionEvaluate(task, agentID, …)` | The exposure declaration itself is the gate (optionally: per-agent external-call permission) |
| Depth limit | Applies | Not applicable (no parent), but must prevent chained amplification of "external → internal delegation" |
| Cancel | Parent cancel → child interrupt | Driven by A2A `cancel` |

→ See §10.1 A3 (exposure model) / A4 (permissions of an exposed agent).

---

## 4. Core mapping specification

> This section is the foundation of the whole scheme and must be reviewed and approved before implementation.

### 4.1 Identity mapping

| A2A | SDK | Description |
|---|---|---|
| `contextId` | `sessionID` | The SDK session is itself durable and resumable, naturally carrying multiple turns; consistent with ADK's choice |
| `taskId` | `turn.id` (a new core first-class durable entity, see §5.6) | An A2A task has a terminal state; an SDK session is perpetual while one turn is terminal — the two are isomorphic |
| `messageId` | `session_message.id` | Eases alignment between `GetTask` and auditing |
| Caller identity (`CallContext.User`) | The session's agent + `Permissions` ruleset + workspace | Security-critical, see §5.5 |
| task in `input-required` | The turn's `awaiting_input` → a core pending permission / question request | See §5.4 / §5.6 |

Supporting facts (already backed by the data model; no new concepts needed):

- The session row already carries `ProjectID` (`insertSessionRow`);
- `schema.LocationInfo{Directory, WorkspaceID, Project}` and `schema.ProjectInfo{Worktree, Sandboxes, ...}` already exist;
- `core/project/directories.go` already supports one project having multiple directories.

### 4.2 Part mapping

A2A natively has only **text / file / data** part kinds and does not define "tool call / thought" semantics — these are expressed via **data part + metadata**. To interoperate with ADK-go (goal 3), shared semantics **directly adopt the existing conventions of `a2a-go` / ADK** (the `adk_` prefix, `function_call` / `function_response` types); SDK-specific semantics use the `sdk_` prefix. Basis: `server/adka2a/v2/parts.go`.

| SDK side | A2A side | Note |
|---|---|---|
| `TextDelta` / `TextEnded` | Text part | Streaming is expressed by the artifact's `append` / `lastChunk` (§4.5) |
| `ReasoningDelta` / `ReasoningEnded` | Text part + meta `adk_thought=true` | Consistent with ADK |
| `ToolCalled` | Data part, meta `adk_type="function_call"` (plus `adk_is_long_running=true` for long tasks) | **Must be restorable on the peer**; the lifeline of coding-agent observability |
| `ToolSuccess` / `ToolFailed` | Data part, meta `adk_type="function_response"` | Failure information goes into the response payload |
| `ToolProgress` | Data part, meta `sdk_type="tool_progress"`; or just send a `working` heartbeat | SDK-specific (§5.7) |
| `Usage` / `StepEnded` | artifact metadata `adk_usage_metadata` | Cost observability |
| `FileDiffInfo{File, Patch, Additions, Deletions, Status}` | Data part, meta `sdk_type="file_diff"` | SDK-specific; the code change itself is a consumable artifact |
| Outbound user media (the last user message's `PromptFileAttachment`/`MediaPart`) | File **raw part** (`NewRawPart` + `MediaType`/`Filename`) when it carries bytes, else **URL part** (`NewFileURLPart`) | `a2aBackend` decodes the media and sends it as file parts through `a2a/a2ago` |
| Inbound non-text part (raw bytes / file URL) | File part | Raw parts are spooled under the SDK state root (`{Root}/data/a2a-spool/<turnID>/`, never the workspace — a read-only lease's working directory is the shared source); the spool is pre-approved for reads like tool output and pruned by the hourly retention sweep. A missing extension is derived from `MediaType` so the read tool recognizes the kind. URL parts are referenced (not fetched). **Delivery to the model** follows the operator policy (`DESIGN-MEDIA` {M16} §4.12), the same engine HTTP uses — source selection (`media.prefer`: inline / upload→file / url), downsample, spool — because the remote caller cannot know our model/provider; `inboundMedia: spool \| inline` remains the fallback. **Large-file exchange** (`DESIGN-MEDIA` {M18} §4.13): a peer may upload out-of-band to the target our AgentCard advertises (a `storage` extension) and send a `FileWithUri`; a URI naming our store is read back locally (spool-equivalent), a peer's external url is a `url` source. |
| Other structured data | Data part; ADK wraps unrecognized data as a `<a2a_datapart_json>…</a2a_datapart_json>` text/plain | Guarantees round-trip fidelity |

Inbound media never reaches the model as a native media part: spooling turns it into a workspace path the read tool consumes, so an unsupported inbound MIME cannot break the turn. Media that does reach the request (for example an image the read tool emits) is subject to the same model-capability gate as the HTTP prompt: the context builder omits media the protocol or model cannot carry and substitutes a short text note (see `docs/DESIGN-HTTP.md` §5.2), so no attachment can wedge a session.

### 4.3 Metadata naming conventions

A2A has no standard metadata key space. For interoperability, **shared keys reuse ADK's key names**; SDK-specific keys use their own prefix. **Hand-written strings are forbidden everywhere**; go through constants. Basis: `server/adka2a/v2/metadata.go`.

| Direction | Prefix | Constant (aligned with ADK) |
|---|---|---|
| SDK → A2A (written into A2A object metadata) | `adk_` (shared) / `sdk_` (SDK-specific) | `a2a.ToA2AMetaKey(key)` (ADK also uses the `adk_` prefix) |
| A2A → SDK (written into session event custom metadata) | `a2a:` | `a2a.ToSDKMetaKey(key)` (ADK uses `a2a:`) |

- **Shared keys (aligned with ADK)**: `type`, `thought`, `partial`, `is_long_running`, `usage_metadata`, `error_code`, `session_id`, `invocation_id`, `author`, `branch`, `app_name`, `user_id`, `task_id`, `context_id`.
- **SDK-specific keys**: `file_diff`, `tool_progress`, etc. (the `sdk_` prefix).

### 4.4 Event → task state machine

| Timing | Emitted |
|---|---|
| No existing turn (new taskId) | Create a `turn` + `Task{state=submitted}` |
| Before starting `Send` | `TaskStatusUpdateEvent{working}` |
| text / reasoning / tool delta | `TaskArtifactUpdateEvent{append=true, lastChunk=false}` |
| `permission.asked` / `question.asked` | `TaskStatusUpdateEvent{input-required}`; the status message carries a data part with `adk_type=function_call` (the ADK long-task convention, see §4.7) |
| `Idle` | Add `lastChunk=true` to close the artifact → `completed` |
| `ErrorEvent` / `StepFailed` | `failed` (the message carries the error text) |
| A2A `cancel` | `Session.Abort()` → `canceled` |

### 4.5 Artifact granularity

**It is recommended to provide only one granularity**: per-turn streaming append, emitting `lastChunk=true` at turn end.

Rationale: ADK's dual `OutputArtifactPerRun` / `OutputArtifactPerEvent` modes are legacy baggage (a partial artifact must be "erased" with an empty data part at the end), and peer aggregation logic is prone to duplicate publication. This SDK's delta events naturally fit a single "append + close" model. If a second mode is genuinely needed later, add only per-message, and **do not** introduce partial-erasure semantics.

### 4.6 What is explicitly not guaranteed (semantic degradation list)

Any attempt at "lossless bidirectional mapping" will fail. The following are **explicit degradations**, which must be documented:

- The SDK's `steer` and A2A's "append a message to an in-progress task" are not the same semantics → map only in controlled scenarios (see §5.3);
- The SDK's `CompactionStarted/Ended` has no A2A counterpart → appears only in metadata;
- The SDK's `Retried` has no counterpart → metadata;
- One SDK turn may contain multiple LLM calls (steps), while A2A has only one task lifecycle → intermediate step boundaries are downgraded to metadata;
- A2A's `relatedTasks` / task-graph capability is not used here;
- **HITL encoding differs**: this SDK's `input-required` bridges permission / question; ADK uses a **long-running function call** (`adk_is_long_running` + `function_call`/`function_response`; on the ADK side `HandleInputRequired` matches by function call ID). Across implementations, convert according to the §4.2 conventions.

### 4.7 ADK-go interoperability checklist (wire-level)

The goal is SDK↔ADK-go **bidirectional** interop (§2.1 goal 3). The following wire conventions must be aligned, all taken from `a2a-go/v2` + adka2a v2 (`server/adka2a/v2/*`, `agent/remoteagent/v2/*`):

1. **Transport / discovery**: `a2a-go/v2` + the AgentCard's `supportedInterfaces` (JSON-RPC / gRPC / HTTP+JSON); the well-known `/.well-known/agent-card.json` returns a **single** AgentCard.
2. **Identity**: `contextId ↔ sessionID` (on the ADK side `sessionID = ContextID`). The ADK client records `taskId` / `contextId` in the session event's custom metadata (keys `a2a:task_id` / `a2a:context_id`, read back via `GetA2ATaskInfo`) — we must do the same as a client in order to resume.
3. **Part**: §4.2 (text + `adk_thought`; data + `adk_type=function_call/function_response`, `adk_is_long_running`; file → URL/raw). Unknown data degrades on the ADK side into `<a2a_datapart_json>` inline text, without erroring.
4. **Artifact**: use `TaskArtifactUpdateEvent`'s `append` / `lastChunk`. **Do not** set `adk_partial` (that is ADK's legacy mode: the client skips its own aggregation and expects the server to have already aggregated). In non-legacy mode the ADK client aggregates by `ArtifactID`, and the server closes with `lastChunk=true`.
5. **State**: `submitted → working →` terminal (`completed`/`failed`/`canceled`); `input-required` expresses HITL. **A non-terminal status message is displayed by the ADK client as a thought (reasoning)**; a `failed` with only one text segment is treated as an error message.
6. **HITL**: ADK uses a **long-running function call** for HITL — the `input-required` status message contains a data part with `adk_type=function_call` (`adk_is_long_running=true`); on the next turn the client sends the user's answer back as a `function_response` (with `taskId` / `contextId`), and the server matches by call ID (ADK: `HandleInputRequired`). To interoperate with an ADK client, when carrying permission/question we **must use this function_call/response encoding**. To let the caller display the prompt, on the SDK side we put the question's `QuestionInfo` (prompt/options) into that function_call's `args.questions` (ADK treats it as the long task's call arguments; this is a **fidelity enhancement**, not required for the peer to understand).
7. **Client incremental sync**: the ADK client sends only the events "since the last remote response", and renders other local agents' events as user text (`presentAsUserMessage`), without resending everything; outbound, we likewise send only the delta.
8. **Simple interactions**: A2A allows returning a `Message` directly for simple requests (no `Task`); the ADK client treats a `Message` as a turn with `TurnComplete=true`.

---

## 5. Inbound (Server) design

### 5.0 Responsibility boundary: what a2a-go provides / what we implement

We **assemble on top of `a2a-go/v2/a2asrv` and do not reinvent the wheel**. The protocol and transport layers are provided by it; we only do "SDK ⇄ A2A" adaptation:

| Capability | Owner | Mount point |
|---|---|---|
| JSON-RPC / REST(HTTP+JSON) bindings, SSE streams, keep-alive, panic handling | **a2a-go** (inherited) | `NewJSONRPCHandler` / `NewRESTHandler` / `WithTransportKeepAlive` |
| Request pipeline: caps validation, concurrency, inactivity, clustering, interceptors, push, `GetTask`·`ListTasks`·`SubscribeToTask` | **a2a-go** (configured) | `NewHandler(executor, opts…)` + `WithTaskStore` / `WithPushNotifications` / … |
| Authentication declarations (`securitySchemes`) and request metadata (`CallContext`) | **a2a-go** (inherited); enforcement is ours via `Options.Security` | `a2asrv/auth.go`; see §5.5 |
| AgentCard type + well-known / dynamic card handler | **a2a-go** (inherited) | `NewStaticAgentCardHandler` / `NewAgentCardHandler` |
| A2A types (Task / Message / Part / Artifact / Event) | **a2a-go** | |
| **Executor** (Execute / Cancel / Cleanup) | **we implement** | Implement `a2asrv.AgentExecutor`: map the session event stream to A2A events |
| **TaskStore persistence** | **a2a-go defines the interface, we implement** | Implement `a2asrv/taskstore.Store` with the DB (mapped to core `turn`, §5.6) |
| **Card generation (agent → AgentCard)** | **we** | `CardBuilder` (§5.1) |
| **Type / event mapping (§4)** | **we** | The core workload |
| **HITL bridging, backend dispatch, `turn`, exposure declaration, authorization policy** | **we** | §5.4 / §3.3 / §5.6 / §3.4 / §5.5 |

After this section we write only the "we" parts; rows marked **a2a-go** only explain "how to configure / implement its interface", without further design.

### 5.1 Components we implement

| Component | Responsibility | Implementation (Go) |
|---|---|---|
| `CardBuilder` | Generates one AgentCard for **each explicitly exposed agent** (**one card = one agent**, each with its own URL/endpoint); `skills` are **operator-authored descriptive labels** (validated only to carry `id`/`name`/`description`; not tied to the agent's tools/subagents), and unset → an **empty array** (`[]`, never a synthesized default skill); `capabilities`, `defaultInputModes`/`defaultOutputModes` and `securitySchemes` are declared truthfully (§3.4 / §6.2) | `a2a/server.buildCard` |
| `Executor` | Implements `a2asrv.AgentExecutor` (Execute / Cancel): maps the `session.Send` event stream to A2A events (§4) | `a2a/server.executor` (dependencies injected via `executorConfig`) |
| `TaskStore` | Implements `a2asrv/taskstore.Store` (Create / Update / Get / List), mapped to core `turn` (§5.6) | `a2a/server.turnTaskStore` (version/pending live in turn metadata) |
| Pending (formerly `PendingRegistry`) | Parked turns + requestIDs awaiting reply | `executor.pending` + `a2aSession.turns` (a channel per `turnID`, released uniformly by `OnTurnEnd`) |
| `WorkspaceResolver` | Caller identity → workspace / Root / permission ruleset | `a2a/server.WorkspaceResolver` (`WorkspaceResolverFunc`), resolved on each inbound call and handed to the `Factory` |
| `ArtifactExternalizer` | Artifact externalization / rewrite hook (§8) | `a2a/server.ArtifactExternalizer`, called before each artifact event is emitted |
| `Handler()` | Returns an `http.Handler`: composes a2asrv's card handler with `NewJSONRPCHandler` (the JSON-RPC binding) using the standard library `http.ServeMux`; with multiple exposed agents, provides each card URL/endpoint separately | `a2a/server.Server.Handler`; when `BaseURL` is empty, derives the card endpoint from the request Host |
| `Security` (optional) | Bearer authentication on the RPC endpoints (cards stay public) | `a2a/server.Security`; `Handler` gates only the JSON-RPC endpoints (constant-time compare, 401); `buildCard` declares the matching HTTP bearer scheme |

- `Factory`: the host provides a `server.Factory`; the SDK ships `agent.NewA2AFactory(client)` adapting a single-project `Client` (applying the inbound default deny ruleset and `Workspace.Permissions`). Multi-workspace is up to the host to implement `Factory` per `Workspace`.
- Security: `server.Options.Security{Bearer, SchemeName}` requires `Authorization: Bearer <token>` without importing a2a-go, and keeps the card truthful. a2a-go ships no authenticator (it only attaches request metadata to `CallContext`), so the SDK enforces the token itself.
- Escape hatch: `server.Options.NativeOptions []any` passes through `a2asrv.RequestHandlerOption` / `TransportOption` (auth, rate limiting, push, etc.); the signature uses `any` to keep a2a-go types out of the public API (§3.1).
- Outbound: the `agent` side drives the remote via `a2aBackend` (through the `route.LLMClient` seam); `a2a/a2ago` is the only outbound adapter layer that imports a2a-go.

### 5.2 Wiring and HTTP dependencies

**Return an `http.Handler`; the host mounts it on its own mux.**

- **HTTP dependencies use only the standard library `net/http`**; protocol bindings use the handlers provided by `a2a-go/v2/a2asrv` (`NewStaticAgentCardHandler` / `NewJSONRPCHandler`, which are themselves `http.Handler`). **Do not introduce a third-party router / server framework** (adk-go's `gorilla/mux` is just its launcher's composition choice).
- The SDK **does not start a server, bind a port, or read env** itself: the host takes the `http.Handler` and mounts it on its own `http.Server`; tests use `httptest`.
- Do not do adk-go's `launcher`-style tight coupling of the server with a startup method. Rationale: consistent with the SDK's long-standing philosophy of "narrow input, no env reading, no default location" (`Root` required, `ConfigDirs` explicit), and it makes embedding in an existing HTTP service simple.
- The repository also ships a command, **`cmd/agentd`**, that serves one client per configured server over HTTP/SSE and/or A2A (see `docs/DESIGN-AGENTD.md`); it supersedes the earlier `cmd/a2aserve` (A2A only) and `cmd/agentserve` (HTTP only), which are removed. It parses a YAML config (string values may reference `${VAR}`/`${VAR:-default}`; no implicit environment reads) and runs one `http.Server` per listener with graceful shutdown. It is an application, not part of the SDK API. Like the removed commands it defaults `dropErroredTurns: true`: a remote client cannot run the interactive revert dance, so a turn the provider rejected (`error.type: "provider"`) is excluded from later request contexts, and a message the provider keeps rejecting cannot wedge the session (interrupts and declines are kept; see `docs/DESIGN-HTTP.md` §5.2).

### 5.3 A turn's lifecycle and message semantics

Execution flow:

1. Resolve the card / `taskId` / `contextId` → decide "new session", "resume session", or "continue an input-required task";
2. message parts → prompt + attachment spooling + path injection;
3. `session.Send(ctx, MessageOptions{Prompt, Mode})` → event stream → A2A event stream (§4.4);
4. Termination: `Idle` → completed; `ErrorEvent` → failed; `asked` → input-required.

A2A's message semantics are richer than the SDK's and need explicit mapping:

| Arriving message | Mapping |
|---|---|
| No `taskId`, new `contextId` | New session + new turn |
| Has `taskId`, task in `input-required` | Resolve as a reply (`Permission.Reply` / `Question.Reply`), **continuing the parked turn** |
| Has `taskId`, task in `working` | `SessionDeliverySteer` |
| Has `taskId`, task already terminal | Reject (`UnsupportedOperationError`) → **D3 decided** |

**Concurrency model**: an SDK session can run only one turn at a time (queue/steer); in A2A, tasks in the same context are serial and different contexts are concurrent. The one-to-one `contextId → session` mapping naturally guarantees serialization.

### 5.4 HITL: the most critical design decision

Architecturally, for a remote user to answer permissions/questions there is really **only one choice: park + resume**. **When interoperating with an ADK client, the `input-required` payload must be encoded as `function_call` (with `function_response` as the answer) per §4.7**, otherwise the ADK side cannot answer.

#### Design: park + resume

When an `asked` event arrives, the A2A layer **parks** this turn's iterator (the `session.Send` goroutine is already blocked on `Ask`'s channel), emits `input-required`, and writes the requestID into the turn state; when the next A2A message returns it calls `Reply`, the blocked goroutine naturally continues, and the original iterator keeps yielding.

It simultaneously satisfies:

- **Semantic fidelity**: the model context, tool-side state, and completed steps are all preserved;
- **No duplicate LLM calls, no duplicate billing**.

Cost: the parked state is **in-process** (limited by sticky sessions).

#### The only fallback path: rebuild (not a parallel option)

When a parked turn is unreachable (process crash/restart, or non-sticky horizontal scaling), the in-process state is lost and only **fallback rebuild** remains: the turn state is already persisted (§5.6), so after a restart, if an active turn is found in `awaiting_input`, the "question + tool call awaiting approval" is resumed as a new turn.

- Cost: the model must re-reason; tools may be replayed (dangerous for `bash` / `write`).

Conclusion: **the online path is always "park + resume"; "rebuild" is only a fallback recovery when a parked turn is unreachable**, not a parallel design requiring a choice between the two. ADK has only the latter (end this turn, rerun next turn); we have both, and that is the most worthwhile part of this investment. **Decision in §10.2 D1 (decided).**

**Reference implementation (ADK)**: ADK's entire inbound path is this "rebuild" path — `Executor.Execute` first calls `HandleInputRequired` (`server/adka2a/v2/input_required.go:152`) to validate that the reply covers all pending `function_call`s in the status message, returning `input-required` + a validation error if any are missing, and rerunning on the durable session once complete (`executor.go:197/215/238`); ADK has no in-process park. Our `turn.awaiting_input` + `pending_request_id` is equivalent to its `StoredTask`.

#### Accompanying SDK-side changes

1. **Already present**: `agent.Event` already carries permission/question events — `PermissionAsked` / `PermissionReplied` / `QuestionAsked` / `QuestionReplied` / `QuestionRejected`, mapped by `events.go`'s `mapEvents` and surfaced through `Session.Send` and `On`. Inbound can directly observe `asked` with no further change;
2. **To do**: express "awaiting input" on the **turn** (`awaiting_input`, §5.6), not on the session; the session's busy/idle is derived from the active turn. If the session remained busy while parked it would block other `Send`s on the same session, so semantically the turn state must distinguish this.

### 5.5 Authentication and authorization

What an agent can do (tools / subagents / skills) is **decided when the agent is defined**; the card's `skills` only **describe** it externally — the protocol does not participate, and the caller cannot change it. This section addresses only **the trust boundary of the session triggered by an external call**.

**Authentication**: A2A defines the card's `securitySchemes` and leaves enforcement to the host; a2a-go's `a2asrv` attaches request metadata to `CallContext` but ships **no authenticator**. The SDK therefore exposes a narrow seam, `server.Options.Security{Bearer, SchemeName}`: when set, `Handler` requires `Authorization: Bearer <token>` on the JSON-RPC endpoints (constant-time compare, 401) while the discovery card stays public, and `buildCard` declares the matching HTTP bearer scheme. Hosts needing other schemes (mTLS, OAuth) mount a2a-go options via `server.Options.NativeOptions` (§3.1 escape hatch).

**Authorization**: the protocol **explicitly leaves this to the implementation** (spec §7.5: authorization is "based on the authenticated identity and its own policies", implementation-specific, MAY reference requested skills / actions attempted / data access policies). a2a-go provides only identity, not policy, so this part is still ours to define:

| Rule | Content |
|---|---|
| Exposure scope | A call can only land within the **set of exposed cards/agents** (§3.4) |
| Permission policy | An externally triggered session uses an **explicitly supplied** permission ruleset (**configurable**: program-generated / config file / user input, all acceptable), defaulting to `deny` for dangerous tools (`agent.DefaultInboundPermissions`, relaxable with `WithInboundPermissions`), **not** the client-level `ApproveAll`; `ask` goes through §5.4's HITL (**decided, see A4**) |
| Workspace isolation | `WorkspaceResolver` maps (caller identity, contextId) to a workspace (`Directory` / `Root` / `Permissions`); reuses the existing `Project` / `Directory` / `Location` / `Sandboxes`. The `Factory` creates the session per `Workspace`; `turn` reuse is scoped by `agentID + contextID` |
| No implicit privilege escalation | A2A metadata **may not** override agent / model / permission |

### 5.6 Turn: the persistence carrier for an A2A task (a core first-class entity)

The protocol logic of `GetTask` / `ListTasks` / `SubscribeToTask` is handled by **a2a-go's handler**; what we must do is **implement `a2asrv/taskstore.Store`** and land it on core's `turn` (+ events). So this section does not design "task queries", only the mapping of "A2A task ↔ our persistence".

**Problem localization**: an A2A `taskId` corresponds to "one turn", but a turn originally had no independent durable row in the SDK. A local subagent never needed such a record because its `task_id` happens to **be** the child session id (`resolveChildSession` directly does `store.Get(TaskID)`) — the task lands on an existing primary key. Only exposing a **turn** as an addressable id remotely creates this extra state.

**Option C (decided)**: promote `turn` to a **core first-class durable entity**, shared by local and remote subagents, rather than adding an A2A-specific `session_task` table.

```text
turn
  id                 text PRIMARY KEY   -- the A2A taskId when inbound
  session_id         text NOT NULL      -- i.e. contextId
  parent_turn_id     text               -- a local/remote subagent child turn → parent turn
  input_id           text               -- the session_input that triggered this turn (anchor)
  state              text NOT NULL      -- running | awaiting_input | completed | failed | canceled | rejected
  pending_request_id text               -- the permission/question request while awaiting_input
  agent / model      text
  anchor_seq         integer            -- starting event sequence (history/artifact boundary)
  end_seq            integer            -- ending event sequence (NULL if not terminal)
  metadata           text               -- protocol extras (remote taskId / contextId mapping)
  time_created / time_updated integer
  indexes: (session_id, time_created), (state)
```

Key points:

- **Mapping**: `contextId ↔ session_id`, `taskId ↔ turn.id`;
- **One entity, two uses**: every local subagent turn, every A2A inbound task, and every A2A outbound mirrored child turn lands in the same `turn` row; `parent_turn_id` expresses the delegation hierarchy, eliminating the asymmetry of "not needed locally, needed remotely";
- **Persistence**: `turn` is a core concept (`schema` + `core/database` table creation), maintained by the coordinator at turn start / suspend / termination; `GetTask`·`ListTasks`·`SubscribeToTask` are handled by the a2a-go handler, and we implement `a2asrv/taskstore.Store` to read/write it (it can be a projection of events, aligned with the existing `session_message`);
- **The landing point of `input-required`** is the turn's `awaiting_input` (see §5.4), not session state;
- **Naming caveat**: the `task` tool's `task_id` is still the child session id (≈ context), **not** a turn; only A2A's `taskId` corresponds to `turn.id`. The two must not be conflated;
- **No A2A-specific `session_task`**: that would both collide in name with the subagent `task` tool and wrongly record "a turn" as "a session's task".

> **On migration**: the SDK has not been released, so **no versioned migrations**. Directly add/remove/modify table statements in `core/database/migration.go`; old development databases (`.data`) locally and under examples can be deleted and recreated.

→ **Decision in §10.1 A8 (decided: option C).**

### 5.7 Long tasks and heartbeat

An SDK turn may run for a long time (a multi-step tool loop), and clients/gateways will time out. There are three layers; **prefer inheriting `a2a-go` and do not invent**.

1. **Transport-level keep-alive (already in a2a-go)**: `a2asrv.WithTransportKeepAlive(interval)` enables an SSE heartbeat — the streaming responses of JSON-RPC (`jsonrpc.go`) and HTTP+JSON (`rest.go`) start a `time.Ticker` that calls `SSEWriter.WriteKeepAlive` each interval, preventing API gateways from dropping idle connections. **Off by default (0)**. We only need to pass the host's configuration through to this option (or via `a2a.WithNativeOptions`), **without implementing heartbeat ourselves**.
2. **Semantic-level progress**: map `StepStarted` / `ToolProgress` to `TaskStatusUpdateEvent{working}` so the client/application sees "still running". This is an **optional** progress expression, distinct from transport-level keep-alive. **Progress updates only the status and does not write artifacts** (no output pollution).
3. **Disconnect / very long tasks (already in a2a-go)**: `a2asrv.WithPushNotifications(store, sender)` provides webhook push (`a2asrv/push`, in-memory store by default); the client may disconnect and receive task updates pushed by the server. Not treated as a dependency this cycle (§2.2), but the mechanism is ready.

> There is also `a2asrv/workqueue`'s `Heartbeater` (`HeartbeatInterval` / `Heartbeat`) — that is the heartbeat for **work item leases in cluster mode**, unrelated to client keep-alive, and relevant only when implementing a distributed executor.

---

## 6. Outbound (Client) design

### 6.0 Responsibility boundary: what a2aclient provides / what we implement

The outbound path is **built on `a2a-go/v2/a2aclient`**, likewise not reinventing the wheel:

| Capability | Owner | Mount point |
|---|---|---|
| card resolution / transport selection (JSON-RPC / REST) / credentials | **a2a-go** (inherited) | `NewFromCard` / `NewFromEndpoints` / `Factory` + `auth` |
| Protocol operations: `SendMessage` / `SendStreamingMessage` / `GetTask` / `ListTasks` / `CancelTask` / `SubscribeToTask` / push config / `GetExtendedAgentCard` | **a2a-go** (inherited) | Methods on `a2aclient.Client` |
| Interceptors / logging | **a2a-go** (configured) | `CallInterceptor` |
| **SDK event ⇄ A2A event conversion** | **we** | The core workload (§4) |
| **backend integration** (`A2ARunner`, mirrored child session, `turn`) | **we** | §3.3 / §6.1 |
| **context export, HITL bridging, cancel policy** | **we** | §6.2 |

### 6.1 The only mechanism: an agent with an A2A backend

The outbound path **has no separate integration form**. A remote A2A agent is semantically just an agent (with `contextId`/multi-turn/HITL/streaming events), so it uses the same mechanism as a local agent: `schema.AgentInfo` carries `backend = A2A`, and the runner dispatches by backend (§3.3). It naturally gains two presentations — **the default (natural) use is as a subagent**, and **using it as a primary agent is an extension**:

| Presentation | How it arises | Cost |
|---|---|---|
| **Subagent** | Appears in `task`'s `subagent_type` list (`SubagentTypes` filters by `Mode != primary`) and is delegated by `task` | Zero changes: `task.go` / `runTask` / `SubagentTypes` all untouched |
| **Primary agent** | `CreateSession(Agent: <remote agent id>)` | Zero cost: the backend is an attribute of the agent |

Implementation points are in §3.3: **do not** fork in `runTask`; instead dispatch on the coordinator's drain by the agent's backend, so the remote agent gets a real mirrored child session. The direct benefit is that the `task` tool, `SubagentTypes`, progress events, depth limit, permission derivation, and hooks inheritance are all unchanged.

**Why not a "tool form"** (the former `a2a.NewRemoteTool`, now removed):

| Dimension | Tool | Remote agent |
|---|---|---|
| Multi-turn / context | None (independent each time) | `contextId ↔ sessionID` natively |
| HITL (`input-required`) | None; can only degrade to failure or a hint | Isomorphic to `permission.asked` / `question.asked`; can park/resume |
| Streaming fidelity | Only one coarse channel, `ToolContext.Progress` | The full event stream is mirrored into the child session |
| `task_id` resume | None | Naturally reused |
| depth / permission derivation / hooks / abort | All would have to be rebuilt | All reused |
| Implementation | Duplicates a set of card/protocol/conversion logic with `A2ARunner` | A single backend, with `a2a/client` as an internal dependency |

Compressing a stateful peer-agent loop into a synchronous function call necessarily loses multi-turn, HITL and event fidelity, and forms a **lossy path** that would be selected by default, contradicting §3.3's goal of "usage consistency". The only narrow "tool-like" scenario (purely stateless single request/response) is expressed with a **single-turn child session**, without a separate form.

→ **A1 / A2 / A7 are all decided (§10.1).**

### 6.2 Mapping details

**What to send** (= A6)

- Delegation (subagent form): send the `task` prompt; primary-agent form: send the first message;
- When files / structured input are needed, **carry them explicitly as parts** (text / data / file), **never** implicitly stuffing local paths into text; a remote file part is spooled by its inbound adaptation (§4.2). Implementation: `a2aBackend` turns the last user message's media into raw/URL file parts and sends them through `a2a/a2ago`.
- When resuming the same task, carry `taskId` / `contextId` (§4.7);
- The SDK has a durable (mirrored) child session and knows exactly what has been sent, so it sends only **new** input; unlike ADK it need not push back "which events the remote is missing" (§4.7).

**What to take back**

- Remote artifact → `TextDelta` (keeps the UI real-time);
- Remote tool call data part → downgraded to `ToolProgress` or a structured log; **do not disguise it as a local `ToolCalled`** (otherwise local permissions / auditing / hooks would mistake the remote tool call for their own);
- `thought` metadata → `ReasoningDelta`.

**Remote input-required → local**

- Default: map to a local `QuestionRequest` (through the existing `OnQuestion` path), the parked turn blocks, and after the user answers it resumes via `Reply` (mirrored child session §3.3 + park §5.4);
- If the remote raises it during pure one-way delegation and no one is available locally to answer: degrade to a failure + hint text, letting the local model decide whether to ask the user with the `question` tool;
- **Interop with an ADK server**: if the remote expresses input-required with a long-running `function_call`, the local answer must be sent back as a `function_response` (with `taskId` / `contextId`) (§4.7).

→ **D2 decided.**

**Cancel and cleanup**

- Local `Session.Abort()` / ctx cancellation → remote `CancelTask`. ADK's `cleanupRemoteTask` is a complete reference: cancel only when "the last event is non-terminal" and it is not input-required, with a 5-second timeout;
- Local `Session.Close()` → check and converge all non-terminal remote tasks;
- A remote task in `input-required` with the local side ending normally → do not cancel (keeps it resumable).

**Secondary use of card resolution**

After fetching the card, besides connecting the client, extract information from it. This is integral to §2.2's "minimal capability adaptation": A2A's capabilities are **declared + discovered** (the server unilaterally declares in the AgentCard, and the client adapts after reading), with no bidirectional negotiation loop. Both ends should implement this minimal set.

| Card field | Use |
|---|---|
| `description` | The subagent / primary-agent description |
| `skills` | The remote agent's capability description; **do not split it into multiple subagents** (one card = one agent, §3.4) |
| `capabilities.streaming` | Whether the remote supports streaming; choose `SendStreamingMessage` accordingly, or degrade to `SendMessage` if not (see the note below) |
| `defaultInputModes/OutputModes` | Decides whether attachment spooling / text fallback is needed |
| `capabilities.pushNotifications` | Whether push is configurable (not a dependency this cycle) |
| `capabilities.extensions[]` | `required=true` and unsupported → reject; otherwise safely ignore |
| `protocolVersion` | Compatibility judgment; cross-implementation interop (SDK↔ADK-go) relies on it |
| `securitySchemes` / `securityRequirements` | Choose an auth scheme this SDK supports |

> **Transport selection (an intentional deviation from ADK v2)**: ADK v2 decides whether to send `SendMessage` or `SendStreamingMessage` based on the **local running mode** (`StreamingMode`), and **does not read the card's `capabilities.streaming`** (`agent/remoteagent/v2/a2a_agent.go:292`; although its test card declares `Streaming: true`, the branch only looks at the local mode). This SDK has no equivalent local mode switch, so it instead **defers to card capabilities**: if streaming is declared, use `SendStreamingMessage`, otherwise degrade to `SendMessage`; if the local side requests streaming while the server declares no support, handle it per A2A's `UnsupportedOperationError` (§3.3.4 Capability Validation). This is a deliberate choice, not a copy of ADK.

The server (inbound) must likewise publish **accurate** `capabilities`: `streaming=true` if streaming is supported, `pushNotifications=false` if push is not implemented, and provide `defaultInputModes/OutputModes` and `protocolVersion` (§5.1 `CardBuilder`).

---

## 7. Phase 0: SDK-side prerequisites to change first

**SDK-side changes** that must be made before A2A lands (`turn` and backend dispatch are fairly general; the exposure marker is A2A-specific):

| Change | Location | Why it is required |
|---|---|---|
| Core first-class `turn` entity + lifecycle recording (including `awaiting_input`) | `schema` / `core/database` table creation / coordinator | Shared by local/remote subagents; the A2A `taskId` = `turn.id`, supporting `taskstore.Store` (for a2a-go's `GetTask`) and restart recovery; session busy/idle derived from the active turn |
| agent **backend** field + drain dispatch point | `schema.AgentInfo` / `client.go` (`AgentConfig`) / `runtime.go` (`buildRunner`) | The prerequisite for outbound (subagent form); **reuse the existing `runner.SessionRunner`, do not add an `AgentRunner` abstraction**; `task.go` / `task_run.go` unchanged (§3.3) |
| "Can be called by A2A" marker and AgentCard generation | `CardBuilder` + agent metadata (`Expose`) | Decides which agents each provide a card URL; avoids exposing internal agents (§3.4) |

---

## 8. Roadmap and acceptance

| Phase | Content | Acceptance | Status |
|---|---|---|---|
| 0 | The three prerequisites from §7 (permission/question event mapping was already ready, not listed) | Existing tests green; new turn-state tests | ✅ `turn` schema + table + `TurnStore`; `AgentInfo.Backend`/`Expose`; backend dispatch (`runner.Backends`) |
| 1 | `a2a/` types + `a2a/client` (card resolution, `SendMessage` / `SendStreamingMessage`, cancel) + backend adaptation + drain dispatch (subagent form) | A remote agent can be delegated by the local model via `task`; timeout / cancel / error paths all tested | ✅ `a2a/client` + `a2a/a2ago` + `a2aBackend`; `TestRemoteA2AAgentAsMainAgent` / `AsSubagent` / `ReusesRemoteSessionAcrossTurns` |
| 2 | `a2a/server` inbound MVP (`Executor` + `taskstore.Store` + card + `http.Handler`, input/output all text) | SDK↔SDK end-to-end: an A2A client sends a message and gets a completed task + artifact | ✅ `a2a/server`; `TestServerSendStreamingMessage` / `GetTask` / `DerivesCardEndpointFromRequest` |
| 3 | HITL loop (park + resume), multi-turn context, restart recovery (fallback rebuild, §5.4) | permission / question round-trip tests; post-restart fallback recovery test | ✅ `TestServerInputRequiredRoundTrip` / `TestExecutorRebuildsAfterRestart` / `TestServerRestartRebuildsParkedTurn` |
| 4 | part-level fidelity (tool trace, thought, usage, file diff) | **Interop tests against ADK-go** (cross-implementation verification of protocol fidelity) | ✅ inbound thought/`function_call`/`function_response`/`file_diff`/`usage`; outbound provider-executed tool trace, HITL `function_response`. **Cross-implementation tests: `examples/adka2a` (a separate module, adk-go server) + `examples/chat`'s `TestAdkA2AInterop` (SDK client ↔ ADK server)** |
| 5 | Productionization: `WorkspaceResolver`, auth (configure a2a-go), keep-alive (configure a2a-go), rate limiting (configure a2a-go's limiter), artifact externalization hook | Multi-tenant isolation tests; `go test -race` green | ✅ `WorkspaceResolver`, `ArtifactExternalizer`, `Options.NativeOptions` (auth/rate-limit/push pass-through), `Options.KeepAlive`; `TestExecutorResolvesWorkspace` / `ExternalizesArtifact` / `ServerAcceptsNativeOptions`; `-race` green |

> Note: Phase 1 did not add a separate `A2ARunner` abstraction, but reused `runner.SessionRunner`'s LLM seam (`route.LLMClient`) — see §3.3. Phase 4's "sparring against ADK-go" cross-implementation test has landed: `examples/adka2a` (an adk-go server, a separate Go module) is reached by `examples/chat`'s `TestAdkA2AInterop` through the SDK's A2A client.
>
> Close-out: §4.2/§5.3 inbound attachment spooling, §6.2 outbound cancel/cleanup, §5.6/A8 all `turn` fields (including local turns and `parent_turn_id`), §5.3 inbound steer, and §4.7 outbound remote `taskId` resume have all been filled in by the implementation.

The **test matrix** references adk-go's A2A tests (`a2a_e2e_test.go` + `*_compat_test.go` scenario naming), covering:

`input-required`, `multi-hop input-required`, `input-required cancellation`, `cleanup propagation`, `single-hop final response`, `streaming success`, `streaming error`, `session reuse`, `callbacks`, `converters`, `custom part converter`, `error on card resolution`, `error on server error`.

---

## 9. Risks

**No architectural-level risk.** A2A is a standard protocol; interoperability is guaranteed by protocol correctness; security is resolved by §5.5; semantic loss is the known degradation of §4.6; the documentation is synced (`AGENTS.md` package architecture table, `docs/DESIGN.md` §4 package structure and the A2A note); promoting `turn` to first-class is a decided cost (A8).

The only thing that needs test coverage is **concurrency correctness** (parking + persistence + event dispatch) — that is an **implementation-quality** concern (present in any concurrent code), not a design risk: cover it with `go test -race` + `testing/synctest`; Phase 0 builds the turn state model first, and the recovery path references ADK's `HandleInputRequired`.

The remaining items are not risks (stated to avoid over-worrying):

- **Interop**: guaranteed by protocol correctness. §4.2's adoption of the `adk_*` conventions is only a **fidelity enhancement** (letting tool trace / thought be restorable on the peer), not part of the protocol; even if it drifts, unknown data parts are gracefully degraded to text by ADK, **without affecting core interoperability**.
- **Security** (a remote triggering local code execution): resolved by §5.5 — auth inherited from a2a-go, authorization denying dangerous tools by default, workspace isolation, and prohibiting metadata privilege escalation. It is a **configuration surface**.
- **Semantic loss**: the **known degradation** of §4.6.
- **Documentation consistency**: `AGENTS.md` and `docs/DESIGN.md` are already synced (package architecture table / package structure / A2A note).
- **Promoting `turn` to first-class**: the **cost** of the decided option C (A8).

---

## 10. Decision list (all converged)

### 10.1 Architectural decisions (A series)

| ID | Question | Decision |
|---|---|---|
| A1 | The implementation site of a remote agent | Falls in the "session execution backend" — add an `A2ARunner` implementing `runner.SessionRunner`, with the coordinator drain dispatching by agent backend; do **not** fork at the `runTask` / `task`-tool layer (§3.3); **the outbound path has no separate "tool form"** (§6.1) |
| A2 | Whether a remote subagent creates a local mirrored child session | Yes. Consistency comes from "the two backends sharing the same session semantics", not from two implementations aligning with each other (§3.3) |
| A3 | Exposure model | An explicit expose declaration decides which agents each provide a card URL (**one card = one agent**) (§3.4) |
| A4 | Permissions of an exposed agent called directly from outside | Use the agent's own ruleset defined by the server (no parent to derive from); in multi-tenant cases narrow by caller identity. **Permissions only need to be "configurable"** — program-generated / config file / user input are all acceptable, and the SDK does not care about the source; **deny dangerous tools by default**, with the host explicitly relaxing it. `DeriveSubagentSessionPermission` does not apply (§3.4 / §5.5) |
| A5 | How remote usage / model are expressed in events and progress | `publishTaskProgress`'s `model` may be empty or take the value declared by the card; `Usage` / `StepEnded` may be missing, and the UI must tolerate that |
| A6 | How local context is exported to the remote | **Explicitly given by the caller** — the `task` prompt / first message, with files carried explicitly as parts; we convert to A2A text/data/file parts and **never** implicitly stuff in local paths. A remote file part is spooled by **its** inbound adaptation (§4.2); for increments, follow ADK's `toMissingRemoteSessionParts` idea (send only events since the last remote response) |
| A7 | The permission action for a remote call | **Reuse the `task` action** (consistent with local for the subagent form; resource = remote agent id, allowing fine control). The primary-agent form does not go through `task`; the host that creates the session is responsible (§3.3 / §6.1) |
| A8 | Mapping and persistence of an A2A task | **Option C**: `contextId ↔ sessionID`, `taskId ↔ turn.id`; `turn` is promoted to a **core first-class durable entity**, shared by local/remote subagents, with no A2A-specific `session_task` table (§4.1 / §5.6) |

### 10.2 Implementation-level decisions (D series)

| ID | Question | Decision |
|---|---|---|
| D1 | HITL route | The only design = park + resume (online); when a parked turn is unreachable (restart / non-sticky), fall back to rebuilding a turn — not a choice between two (§5.4) |
| D2 | How to localize a remote input-required | Map to a local `QuestionRequest` (through the existing `OnQuestion`, park/resume); during pure one-way delegation with no one to answer, degrade to failure + hint; when interoperating with ADK, reply with `function_response` per §4.7. Do **not** map to a local `permission` (local permissions do not cover remote internal calls) |
| D3 | A terminal task receiving a new message | Reject — the A2A spec says a terminal task no longer accepts messages (`UnsupportedOperationError`) (§5.3) |
| D4 | Dependency strategy | Support only the single version `github.com/a2aproject/a2a-go/v2` (not compatible with v0.x), isolated by the thin `a2a/a2ago/` adapter layer |
| D5 | Artifact granularity | Only per-turn streaming append + `lastChunk`; drop ADK's dual modes (§4.5) |

---

## Appendix A: comparison with adk-go `adka2a`

| Dimension | adk-go | go-agent-sdk (this document) |
|---|---|---|
| session semantics | stateless runner + `SessionService`, `contextId = sessionID` | durable SQLite session, `contextId = sessionID` |
| `input-required` | a purpose-built long-running tool protocol; end this turn, rerun next turn | directly bridges the existing `permission.asked` / `question.asked` + `Reply` |
| suspend capability | none (replay only) | yes (in-turn blocking + park/resume) |
| artifact granularity | dual modes (per-run / per-event), partial must be erased | a single per-turn append + close |
| outbound form | only `remoteagent.NewA2A` (a first-class agent) | likewise "an agent with an A2A backend", surfacing via `task` as a subagent and usable as a primary agent (no separate tool form) |
| streaming selection | decided by the local `StreamingMode`, not reading the card's `capabilities.streaming` (`a2a_agent.go:292`) | defers to the card's `capabilities.streaming`, degrading to non-streaming if unsupported (§6.2) |
| version strategy | dual-version coexistence v0.3.15 + v2.2.1 | only the single version v2 |
| HITL encoding | long-running `function_call` / `function_response` (`adk_is_long_running`) | bridges `permission.asked` / `question.asked`; for an ADK client uses the same `function_call` encoding per §4.7 |
| task persistence | a2a tasks go to a2a-go's task store, sessions to `SessionService` | a core first-class `turn` (`taskId = turn.id`), implementing a2a-go's `taskstore.Store` (A8) |
| server wiring | the launcher provides the A2A server (`adka2a` Executor) | returns an `http.Handler`, mounted by the host |
| security model | `userID` from `CallContext`, workspace isolation not emphasized | permission ruleset + workspace isolation + untrusted by default |

Reusable ideas (worth referencing but not copying): the data-part encoding style for typing parts, `cleanupRemoteTask`'s cancel determination, and the card skills generation approach (ADK lists tools / subagents all as skills, which is protocol-valid; whether to enable it is policy, see §3.4), plus the e2e test matrix.
