# go-agent-sdk HTTP API design

> Status: **draft (design only; not implemented)**
> Scope: a language-agnostic HTTP API for go-agent-sdk, so hosts written in other
> languages (Python / Node / Java / …) can drive the SDK across a process boundary
> Relationship: this document is a sub-volume of `docs/DESIGN.md`, covering only
> the HTTP API. The general architecture, public API and porting rules are still
> defined by `docs/DESIGN.md` and `AGENTS.md`.
> Convention: the Go code below is **illustrative only**; it expresses type
> boundaries and contracts, not final signatures.

## Change log

| Date | Change |
|---|---|
| 2026-10-06 | First draft: motivation, mirror table, endpoint reference, streaming model, security, `cmd/agentserve`, roadmap |
| 2026-10-06 | Detailed review against the SDK source: durable vs live-only events + per-session stream merge, per-session prompt worker, session-handle lifecycle/concurrency, HITL semantics (permission vs question), error model, OpenAPI, config/validation |
| 2026-10-06 | Whole-document consistency passes; `cmd/agentserve` scope; decision list consolidated (D1–D19) |
| 2026-10-06 | Adopted the `AGENTS.md` "align with opencode v2" principle — §16 reframed as required alignment work (SDK changes allowed) instead of shims; `/message` full + `/context` active; resume **merges** inbound over stored (D19), with `a2aFactory.ResumeSession` landed on merge (test in `a2a_backend_test.go`) |
| 2026-10-06 | Whole-document review: consolidated the change log; renumber `§1.0`→`§1.1` |
| 2026-10-06 | §11 decision (after cost/benefit): **no schema-first / generated-SDK pipeline** — SSE isn't OpenAPI-expressible and the consumer set is small; the contract is this document plus a hand-written reference client, with OpenAPI as optional discovery only. Dropped B2, demoted B1 to recommended |
| 2026-10-06 | Cleanup after that decision: `baseURL` is documented as optional-doc-only; the `bad_request` source is "JSON decode / validation" (no schema pipeline) |
| 2026-10-06 | Dropped OpenAPI entirely (a hand-maintained spec would drift with no codegen consumer) and the Python/Node client deliverable (that client belongs in the consumer's repo); this repo ships the Go server plus a Go end-to-end test |

---

## 1. Motivation

Sometimes embedding go-agent-sdk in-process is impossible: Go is not importable
from Python/Node without cgo/FFI bindings, and re-implementing the agent loop in
another language is out of scope. A concrete case:

- **WeCom (企业微信) bot SDK** ships only Python and Node.js implementations
  ([`wecom-aibot-python-sdk`](https://pypi.org/project/wecom-aibot-python-sdk/),
  [`@wecom/aibot-node-sdk`](https://www.npmjs.com/package/@wecom/aibot-node-sdk);
  see the [WeCom bot docs](https://developer.work.weixin.qq.com/document/path/101463));
  there is no Go SDK. The bot handler must be written in Python/Node, but the
  agent logic should run on go-agent-sdk.
- Porting the WeCom bot SDK to Go just to embed go-agent-sdk is not a realistic
  trade-off: the bot SDK carries transport, signature, encryption and reconnect
  concerns that have nothing to do with the agent loop.

The right seam is an **HTTP API on the go-agent-sdk side**: the foreign SDK calls
it as a normal web service.

```
WeCom platform ──► Python/Node bot (wecom-aibot sdk) ──HTTP──► go-agent-sdk server
                                 (session/prompt/SSE)              (agent loop)
```

The case generalizes: any host whose language cannot link the Go library — a
polyglot deployment, a bot/frontend in Python/Node/Java, a service that wants the
agent loop in a separate restartable process, or several clients sharing one
agent instance.

### 1.1 Why HTTP (and not the alternatives)

| Alternative | Why not |
|---|---|
| cgo/FFI bindings or a native Python/Node binding | Per-language, high maintenance, and still couples two runtimes in one process (a crash takes both down) |
| gRPC / JSON-RPC | Heavier client setup (protoc/codegen) and no browser-native streaming; this is not an agent-to-agent protocol |
| Fork a Go binary per message | Loses durable sessions and live streaming; process startup cost per turn |
| A2A | That is agent↔agent (§1.2); here the caller is the host driving its own SDK |

HTTP + SSE is universal: every language has an HTTP client, SSE needs no schema
compiler, and the same surface doubles as a future web API.

### 1.2 Relationship to A2A

This is deliberately **not** A2A. A2A (`docs/DESIGN-A2A.md`) is "agent calls
agent": mutually opaque agents exchange `Task`/`Message` with capability
negotiation. The HTTP API here is "**host drives one SDK instance**": a
control-plane over the SDK's own `Client`/`Session` surface. The **caller and
transport are trusted infrastructure** (not a peer agent or an arbitrary remote
tenant), but the end-users whose messages the caller relays are **not** — which
is why inbound sessions still default to deny for dangerous tools (§8.3). It is
the analog of opencode's own web API (`packages/server` + `packages/protocol`).

### 1.3 What we mirror from opencode

opencode already solved this for its CLI/desktop/TUI clients. We mirror its
shape, dropping what does not exist in this SDK. This table is a summary; §5 is
the authoritative endpoint list and §5.5 names the dropped groups.

| opencode concept | go-agent-sdk equivalent |
|---|---|
| `packages/server` (Effect `HttpApi` handlers) | new `server/` package (hand-rolled `net/http`) |
| `packages/protocol` (route groups; Effect schemas) | folded into `server/`; the wire contract is documented here (§11) |
| `GET /api/health` / `/api/agent` / `/api/skill` | same (inventory reads; `/api/agent` is filtered to the allow-list, §5.1) |
| session CRUD + `switchAgent` / `switchModel` / `compact` / `interrupt` / `wait` / `revert` | same, over `Client` / `Session` |
| `GET /api/session/:id/message` and `/context` | both, aligning v2: `/message` = full, `/context` = active context (needs a new full-message read, §5.2 / §16 B4) |
| `POST /api/session/:id/prompt` (admit + schedule) | same, via a per-session worker over `Session.Send` (§6.1) |
| `GET /api/event` (global SSE) | same (`EventStream().AllBounded`) |
| `GET /api/session/:id/event` (durable SSE) | same, but **durable replay merged with live** (§5.4), because deltas/HITL are live-only in this SDK |
| SSE envelope (`server.connected`, heartbeat, `event: message`) | same (§6.3) |
| permission / question request + reply routes | same, over `Client.Permission()` / `Client.Question()` |
| auth: Basic user/password from `OPENCODE_*` env | **Bearer by default** (Basic optional); **no env**, secrets from config (§8.1) |
| location-scoped routes (`location` query) | **dropped**: one `Client` = one project/root |
| provider / model catalog (models.dev) + credential / integration (OAuth) routes | **dropped**: the host supplies `Protocol` + credentials; `catalog`/`credential`/`integration` are not ported (`AGENTS.md`) |
| `pty` / `project-copy` / `reference` / `fs` / `command` routes | **dropped** (§5.5) |
| `session.active` / `session.history` routes | **pending alignment (B6)**: v2 has both; this draft omits them (running state is observable via `session.status`, replay via `/event`) |

---

## 2. Goals and non-goals

### 2.1 Goals

1. **Remote control without Go.** A host in another language can create a
   session, send a prompt, stream assistant/tool/event output, answer
   permission/question prompts, and abort — the *client* needs no Go code (only
   the server process does).
2. **Stable, documented wire contract.** The endpoints, payloads, SSE frames and
   event shapes are specified here, so a client can be written against them.
3. **Same semantics as in-process.** Durable sessions; turns serialized per
   session; subagent events visible; hold-and-reply HITL; reconnection with
   durable replay.
4. **SDK philosophy preserved.** No env reading, no port binding, no default
   location — the host supplies everything and mounts the `http.Handler`.
5. **Embeddable and standalone.** The same surface is both an `http.Handler`
   (`server.New`) and a runnable process (`cmd/agentserve`).
6. **Secure by default.** Auth required for non-loopback binds; API-created
   sessions run under deny-dangerous inbound permissions (§8).

### 2.2 Non-goals

- **Not multi-tenant.** One `server.Server` wraps one `agent.Client` (one
  project/root); multi-tenancy is the host's job (N handlers / N ports).
- **Not A2A or MCP**; those are separate sub-volumes.
- **Not a runtime control plane.** Agents, tools, plugins, MCP servers,
  references and skills are configured at startup (code / `ConfigDirs` /
  credentials); there are no HTTP routes to register or mutate them.
- **No unbounded request-holding.** There is deliberately no "send and block for
  the whole answer" endpoint; completion is observed via SSE/replay (D3). The
  sole exception is the bounded `/wait?timeout=` poll (§5.2).
- **Not a UI.** No bundled web page or static assets; CORS exists only so a host
  may build its own browser client.
- **No model catalog or credentials over HTTP.** They are constructor inputs to
  `Client`, never HTTP state.
- **SSE only.** No WebSocket / gRPC / PTY transports.

---

## 3. Package structure

```
server/                         HTTP API (Go-specific; no direct TS counterpart, like a2a/)
├── server.go                   Server, Options, New, Handler(), Close
├── routes.go                   route table (net/http ServeMux) + middleware chain
├── health.go                   GET /api/health
├── inventory.go                GET /api/agent, /api/skill, /api/tool
├── session.go                  session CRUD / switch / interrupt / compact / wait / revert
├── message.go                  GET message (full) / context (active)
├── worker.go                   per-session prompt worker (§6.1)
├── event.go                    SSE: GET /api/event, GET /api/session/{id}/event (replay+live)
├── permission.go               permission list/get/reply
├── question.go                 question list/get/reply/reject
├── interactions.go             hold mode + timeout policy glue
├── auth.go                     bearer / basic middleware
├── cors.go                     CORS middleware
├── errors.go                   error envelope + SDK error → status mapping
├── sse.go                      SSE writer, heartbeat, wire event envelope
└── wire.go                     request/response DTOs
cmd/agentserve/                 standalone command (YAML + flags, no env), like cmd/a2aserve
```

Notes:

- **Go-specific package.** It has no direct TS counterpart (like `a2a/`), so it
  should be registered in `AGENTS.md`'s package table rather than claimed as a
  mirror of a `packages/*` directory. The HTTP *shape* is modeled on
  `packages/server` + `packages/protocol`; the Effect `HttpApi` / DI internals
  are not ported.
- **Dependency direction: `server/` → root `agent`**, one way, plus the public
  `schema/` and `core/` types (`EventStream()` returns `core.Event`). The root
  package does **not** import `server` (no cycle). Unlike A2A, no root-side
  adapter is needed; the server is intrinsically coupled to the public `agent`
  API.
- `server` collides in name with `a2a/server`; they are distinct import paths.
  A file using both aliases one (e.g. `a2asrv`).
- Uses only the standard library `net/http` (no third-party router), consistent
  with `a2a/server`, and **adds no dependency** (`go.mod` unchanged).
- `cmd/agentserve` is `package main` (not importable), like `cmd/a2aserve`.

---

## 4. Public API (Go)

```go
package server

// Options configures a Server. It covers the HTTP surface only: every model/SDK
// setting lives on the injected *agent.Client (see the mapping table below).
type Options struct {
    // Auth, when set, requires a credential on every /api route except the ones
    // listed in Public. Nil disables authentication (loopback use). Type and
    // scheme (bearer/basic) are defined in §8.1.
    Auth *Auth

    // Agents is the allow-list of agent names the API may create/use (§12.4).
    // Empty allows every registered agent; cmd/agentserve always passes a
    // non-empty list (default [build]) for a tighter posture.
    Agents []string

    // CORS adds browser origins to the default allow-list (loopback:
    // http://localhost:* / http://127.0.0.1:*, plus same-origin). Non-browser
    // clients send no Origin and are unaffected.
    CORS []string

    // InboundPermissions is applied to sessions created through the API,
    // merged over the agent's own ruleset. Defaults to
    // agent.DefaultInboundPermissions() (deny dangerous tools).
    InboundPermissions schema.PermissionRuleset

    // InteractionTimeout, when > 0, auto-resolves a held permission/question
    // request that no client answers in time. See §9.
    InteractionTimeout time.Duration
    // InteractionFallback decides the auto-resolution on timeout:
    // "reject" (default) or "allow". "allow" auto-approves permission requests
    // and rejects questions without an answer; it is dangerous and explicit.
    InteractionFallback string

    // SubscriberCapacity bounds the live event queue per SSE subscriber.
    // Zero uses the default (256); negative is rejected.
    SubscriberCapacity int
    // KeepAlive is the SSE heartbeat interval. Negative disables it; zero uses
    // the default (15s).
    KeepAlive time.Duration

    // Public lists route patterns that skip Auth (e.g. "/api/health").
    Public []string
}

type Server struct { /* ... */ }

// New binds a Server to one Client. It switches the client to
// HoldInteractions(true) so permission/question prompts surface over HTTP.
func New(client *agent.Client, opts Options) (*Server, error)

// Handler returns the http.Handler for the host to mount on its own mux/server.
// The SDK never binds a port or reads env.
func (s *Server) Handler() http.Handler

// Close releases SSE subscribers and attached session handles. It does not
// close the Client (the host owns it).
func (s *Server) Close() error
```

**Config ↔ API mapping.** `cmd/agentserve` (§12) splits its YAML/flag input
across three destinations; only the middle column is `server.Options`:

| Config key(s) | Destination |
|---|---|
| `listen`, `tls`, `allowInsecure` | `cmd/agentserve` process (socket / HTTPS / bind policy), not `Options` |
| `auth.bearer` / `auth.username`+`auth.password` | `Options.Auth` (§8.1) |
| `cors` | `Options.CORS` |
| `inboundPermissions` | `Options.InboundPermissions` |
| `interactionTimeout` / `interactionFallback` | `Options.InteractionTimeout` / `Options.InteractionFallback` |
| `subscriberCapacity` / `keepAlive` / `publicRoutes` | `Options.SubscriberCapacity` / `Options.KeepAlive` / `Options.Public` |
| `agents` | `Options.Agents` (§12.4) |
| `root`, `workingDirectory`, `configDirs`, `model`, `protocol`, `modelBaseURL`, `credentials`, `contextLimit`, `outputLimit`, `subagentDepth`, `titleAuto` | `agent.ClientOptions` → the `*agent.Client` passed to `New`; never HTTP state |

So `server.Options` is HTTP-only; the command translates its config into both an
`agent.Client` and a `server.Options`. §12.1/§12.2 are the command-level view of
this same split.

### 4.1 Public additions to the root package

The server needs the raw event bus, which lives on the unexported runtime:

```go
// EventStream exposes the raw event bus for protocol adapters (the HTTP server,
// future transports). It is the raw counterpart to Client.On, which maps to the
// high-level Event union. Callers use AllBounded for the live stream (every
// event, including live-only deltas and HITL) and Durable(aggregateID, after)
// for persisted replay; per-session streaming merges the two (§5.4). Mirrors
// Client.TurnStore.
func (c *Client) EventStream() core.Event
```

Two small read-side additions back `GET /api/session/{id}` and `POST …/wait`
without scanning `ListSessions` or attaching a handle:

```go
// GetSession returns a session's stored info (including its current model)
// without attaching a handle. It returns schema.SessionInfo (tagged, with
// Model/Permission); the server maps it to the reduced wire DTO of §5.2.
func (c *Client) GetSession(ctx context.Context, sessionID string) (schema.SessionInfo, error)

// WaitSession blocks until the session is idle or timeout elapses, and reports
// whether it is idle (matching the /wait response, §5.2).
func (c *Client) WaitSession(ctx context.Context, sessionID string, timeout time.Duration) (bool, error)

// SessionHistory returns the full transcript (every session_message row,
// ignoring compaction); SessionMessages stays the active context. Backs B4.
func (c *Client) SessionHistory(sessionID string) ([]schema.SessionMessage, error)
```

The per-session event stream (§5.4) is composed by the server from
`EventStream()` (durable replay merged with the live filtered stream); no
additional public API is needed.

Everything else uses the existing public API (`CreateSession`, `ResumeSession`,
`ListSessions`, `DeleteSession`, `SessionMessages`, `Session.Send`, `Abort`,
`Compact`, `SetAgent`, `SetModel`, `SetTitle`, revert methods, `Permission()`,
`Question()`, `Agents()`, `AgentTools`, `Skills()`).

---

## 5. Endpoint reference

Base path `/api`. JSON everywhere except SSE. All responses are JSON objects;
list responses use `{ "data": [...] }` (opencode convention). Auth applies to all
routes except `Options.Public`.

### 5.1 Meta

| Method | Path | SDK call | Response |
|---|---|---|---|
| GET | `/api/health` | — | `{"healthy":true}` |
| GET | `/api/agent` | `Client.Agents().All()` (`*core.Agent` → `[]schema.AgentInfo`) | `{"data":[AgentInfo]}` |
| GET | `/api/skill` | `Client.Skills().List()` | `{"data":[SkillInfo]}` |
| GET | `/api/tool?agent=` | `Client.AgentTools(agent, perms)` | `{"data":[ToolInfo]}` |

`/api/tool` requires `agent` (400 when missing) and that agent must be in the
`Options.Agents` allow-list (§12.4), else `403`; `perms` is
`Options.InboundPermissions`. `/api/agent` returns only non-hidden agents that
are in the allow-list, so the API never advertises what it cannot use.

`AgentInfo` here is a **reduced wire DTO**, not `schema.AgentInfo`: the SDK type
already carries JSON tags for `system` (system prompt) and `permissions`
(`schema/agent.go`), so marshalling it directly would leak both. The wire shape
keeps only `id`, `description`, `mode`, `hidden`, `model`, and `backend`
(local/A2A marker).

`SkillInfo` (`schema.SkillInfo`) already carries `content` (the full SKILL.md
body) and an absolute `location`; if the host should not expose skill text, map
it to a reduced DTO (`name`, `description`).

### 5.2 Sessions

| Method | Path | SDK call | Response |
|---|---|---|---|
| GET | `/api/session` | `ListSessions` + server-side `search`/`limit`/`order` | `{"data":[SessionInfo]}` |
| POST | `/api/session` | `CreateSession` | `{"data":SessionInfo}` |
| GET | `/api/session/{sessionID}` | `GetSession` | `{"data":SessionInfo}` |
| DELETE | `/api/session/{sessionID}` | `DeleteSession` | `204` |
| POST | `/api/session/{sessionID}/agent` | `Session.SetAgent` | `204` |
| POST | `/api/session/{sessionID}/model` | `Session.SetModel` | `204` |
| POST | `/api/session/{sessionID}/title` | `Session.SetTitle` | `204` |
| POST | `/api/session/{sessionID}/prompt` | `Session.Send` (per-session worker, §6.1) | `202 {"data":{"queued":true}}` |
| POST | `/api/session/{sessionID}/interrupt` | `Session.Abort` | `204` |
| POST | `/api/session/{sessionID}/compact` | `Session.Compact` | `{"data":{"compacted":bool}}` |
| POST | `/api/session/{sessionID}/wait?timeout=` | `WaitSession` (bounded) | `200 {"data":{"idle":bool}}` |
| GET | `/api/session/{sessionID}/message` | `SessionHistory` (full transcript, §16 B4) | `{"data":[Message]}` |
| GET | `/api/session/{sessionID}/context` | active context (`SessionMessages`, post-compaction) | `{"data":[Message]}` |
| GET | `/api/session/{sessionID}/event?after=` | `EventStream()` (replay+live, §5.4) | SSE |
| POST | `/api/session/{sessionID}/revert/stage` | `Session.RevertStage` | `{"data":RevertState}` |
| GET | `/api/session/{sessionID}/revert` | `GetSession(...).Revert` (no attach) | `{"data":RevertState\|null}` |
| POST | `/api/session/{sessionID}/revert/clear` | `Session.RevertClear` | `204` |
| POST | `/api/session/{sessionID}/revert/commit` | `Session.RevertCommit` | `204` |

Requests:

```jsonc
// POST /api/session
{ "agent": "build", "model": "deepseek-flash", "title": "optional" }

// POST /api/session/{id}/agent
{ "agent": "plan" }

// POST /api/session/{id}/model   (schema.ModelRef)
{ "id": "deepseek-flash", "providerID": "openai-compatible-chat", "variant": "optional" }

// POST /api/session/{id}/title
{ "title": "Refactor the parser" }

// POST /api/session/{id}/prompt
{
  "prompt": "fix the failing test",
  "delivery": "queue",          // "queue" (default) | "steer"; anything else → 400
  "files": [                     // queue only; base64 or data URL (SDK never reads a path/URL)
    { "uri": "data:image/png;base64,…", "mime": "image/png", "name": "shot.png" }
  ]
}

// POST /api/session/{id}/revert/stage
{ "messageID": "msg_…" }         // this SDK has no `files` option on RevertStage
```

`SessionInfo`: `id`, `title`, `agent`, `model`, `directory`, `parentID`
(non-empty for a subagent child session), `timeCreated`, `timeUpdated`. `model`
is read from the session row (the root `SessionInfo` omits it), so a client can
confirm the active model after `POST …/model`.

Notes:

- **No paging yet.** `ListSessions` / `SessionMessages` return the full set; the
  server applies `search`/`limit`/`order` in memory. Opaque cursors are an open
  question (§16 S1), not part of this draft's response shape.
- **`/message` vs `/context` (align v2).** opencode exposes both
  `session.messages` (full projected messages) and `session.context` (active
  context after the last compaction). This SDK currently has only the active-
  context read (`Store.Context`, `core/session/history.go`), so aligning requires
  a **new full-message read** (§16 B4); `/message` returns that, `/context`
  returns the active context. Pre-compaction turns are otherwise recoverable only
  as durable *events* (`/event?after=0`).
- **`GET /api/session` query semantics.** `limit` (default 50, max 200) and
  `order` (`asc` | `desc` by `timeUpdated`, default `desc`) are applied in memory
  by the server, because `ListSessions` returns everything newest-first.
  `search` is a **case-insensitive substring match on `title`**. No opaque cursor
  (see the paging note below).
- **Unknown sessions 404.** Sub-resources that would otherwise return empty
  results (`/message`, `/event`) first verify the session exists (`GetSession`)
  and return `404`; `ListSessions`/`GetSession` cover the non-existent case, so a
  client never gets a silent empty list or a stream that never yields.
- `Message` is `schema.SessionMessage`, whose concrete types already carry
  `type` + JSON tags (assistant/user/tool/compaction/…), so it is marshalled
  directly (no DTO needed).
- **Root-package response types have no JSON tags.** `SessionInfo` and
  `AgentTool` ([`client_session.go`](../client_session.go), [`client.go`](../client.go)),
  and `PermissionRequest` / `QuestionRequest` (§5.3), are plain structs, so
  marshalling them yields PascalCase keys. The server maps them to explicit wire
  DTOs with the documented lowercase names.
- **`/wait` is the only blocking route.** Every other state change returns
  immediately (D3). `/wait?timeout=` (default **30s**) blocks until the session
  is idle or the timeout elapses, then returns `{"data":{"idle":true|false}}` — a
  busy-but-timed-out session is a normal result, not an error. It exists for
  clients that cannot hold SSE and prefer a bounded poll; other clients use
  `/event` + `session.status`.
- **Prompt admission is asynchronous.** The `202` acknowledges the server queue,
  not SDK admission; the resulting `messageID` arrives as the durable
  `session.next.prompt.admitted` event on `/api/event` or during replay (§5.4).
- **Prompt `delivery`.** `steer` maps to `Session.Steer`, which takes only prompt
  text — `files` are ignored there (attach them with `queue`). An unrecognised
  `delivery` value is rejected `400`, not silently treated as `queue`.
- **Revert needs snapshots.** `revert/stage` / `clear` / `commit` depend on the
  snapshot service; on a project without snapshots they fail (map to `503`).
  `GET …/revert` returns `null` when nothing is staged.

### 5.3 Interactions (permission / question)

| Method | Path | SDK call | Response |
|---|---|---|---|
| GET | `/api/permission` | `Permission().List()` | `{"data":[PermissionRequest]}` |
| GET | `/api/session/{sessionID}/permission` | `Permission().ForSession(id)` | `{"data":[…]}` |
| GET | `/api/session/{sessionID}/permission/{requestID}` | `Permission().Get(id)` | `{"data":PermissionRequest}` |
| POST | `/api/session/{sessionID}/permission/{requestID}/reply` | `Permission().Reply` | `204` |
| GET | `/api/question` | `Question().List()` (filtered) | `{"data":[QuestionRequest]}` |
| GET | `/api/session/{sessionID}/question` | `Question().List()` filtered | `{"data":[…]}` |
| GET | `/api/session/{sessionID}/question/{requestID}` | `Question().Get(id)` | `{"data":QuestionRequest}` |
| POST | `/api/session/{sessionID}/question/{requestID}/reply` | `Question().Reply` | `204` |
| POST | `/api/session/{sessionID}/question/{requestID}/reject` | `Question().Reject` | `204` |

```jsonc
// permission reply
{ "reply": "once", "message": "optional" }   // "once" | "always" | "reject"
// question reply (labels per question, in order)
{ "answers": [["yes"], ["option-b"]] }
```

**The `{sessionID}` in these paths is authoritative.** `Permission().Get` /
`Question().Get` / `.Reply` / `.Reject` key only by request ID, so the handler
must load the request and reject with `404` when its `SessionID` differs from the
path. Without that check the route silently permits cross-session reads/replies.
(It is not a privilege boundary in the single-tenant design — an authenticated
client can read any session anyway — but it becomes one if a host adds
per-session authorization.)

### 5.4 Events

| Method | Path | SDK call | Response |
|---|---|---|---|
| GET | `/api/event` | `EventStream().AllBounded(ctx, cap)` | SSE: every live event, all sessions |
| GET | `/api/session/{sessionID}/event?after=` | `EventStream()` composed: durable replay **+** live filtered by session | SSE: this session only |

**Durable vs live-only events is the crux.** Only event definitions with a
`Durable` marker are persisted; the rest are live-only and are **not** returned
by `core.Event.Durable`. Live-only events include:

- the streaming deltas — `session.next.text.delta`,
  `session.next.reasoning.delta`, `session.next.tool.input.delta`
  (`schema/session_event.go`);
- `session.status` (`busy`/`idle`) — `session.idle` exists as a deprecated,
  never-published definition (`schema/session_status_event.go`); only
  `session.status` is emitted (`runtime.go`);
- all HITL events — `permission.v2.asked`/`.replied`,
  `question.v2.asked`/`.replied`/`.rejected` (`schema/permission.go`,
  `schema/question.go`).

Consequences:

- `/api/event` (live) is the **primary streaming channel** and carries
  everything, including deltas and HITL.
- `/api/session/{id}/event` must **merge two long-lived iterators** into one
  channel: `EventStream().Durable(ctx, id, after)` — which replays persisted
  events **and then keeps yielding live durable events** (it does not end;
  `signalDurableWakes`, `core/event.go`) — and `EventStream().AllBounded(ctx, cap)`
  filtered to this session (the live leg, carrying live-only deltas and HITL).
  The legs overlap, so events are **de-duplicated by `id`**. To preserve publish
  order and close the gap: subscribe the live leg first and **buffer** it while
  the persisted replay is read, then flush the buffer (dropping anything already
  replayed) before switching to pure live.
- A client that subscribes **only** to the durable replay of
  `/api/session/{id}/event` would miss every delta and every permission/question
  ask and would wedge on an unanswered prompt — hence the merge is mandatory,
  not optional.
- On reconnect, deltas produced while disconnected are not recoverable (they are
  not persisted); the client recovers final state from the durable replay and
  the authoritative `message` endpoint. Pending HITL is recovered from
  `/api/permission` / `/api/question` (§5.3), not from replay.
- **Session-less events stay global.** Events whose `data` has no `sessionID`
  (e.g. `mcp.tools.changed`, `mcp.browser.open.failed`) cannot be attributed to a
  session, so they appear only on `/api/event`, never on a per-session stream.
- **`after` semantics.** Omitting `after` streams **live only** (no replay) so a
  fresh subscriber is not dumped the whole history; supplying `after=<seq>`
  first replays that session's durable events after `seq`, then continues live.
  Live-only events carry no sequence, so a client tracks the last durable `seq`
  for reconnects.
- **Replayed frames are lean.** Persisted events are read back as
  `{id, type, data, durable}` only — `location` and custom `metadata` are not
  persisted (`readAfter`, `core/event.go`), so those fields appear on live
  frames but are `omitempty` on replay. Clients must not depend on them being
  present after a reconnect.

### 5.5 Dropped opencode groups

`provider`, `model`, `integration`, `credential`, `project-copy`, `pty`,
`reference`, `fs`, `command`, `location`. Reasons: no SDK equivalent, CLI
infrastructure excluded by `AGENTS.md`, or subsumed by the agent's own tools
(`fs`/`command` would duplicate `read`/`glob`/`grep`/`bash` while bypassing the
permission system). Note `reference` here means the reference *routes*; reference
*sources* become command config (§12.6).

---

## 6. Streaming model

### 6.1 Prompt scheduling: a per-session driver, not request-holding

The central rule: **an HTTP request never spans a turn.** Long turns, provider
latency and tool execution can far exceed any bot platform's webhook timeout
(WeCom, Slack, etc.), and a turn must survive a dropped connection.

The SDK exposes exactly one "admit + run" entry — `Session.Send` — and no public
admit-only path; `Coordinator.Wake` is unused outside tests. So the server drives
execution itself and **serializes prompts per session**:

```
POST /api/session/{id}/prompt   ──► enqueue, return 202 (immediately)
        worker (one per session) ──► Session.Send(ctx, msg), drained to Idle
GET  /api/session/{id}/event    ──► SSE: durable replay + live (separate connection)
```

- `delivery:"queue"` (default): the server keeps **one worker per session** (a
  FIFO of pending prompts). The worker pops one prompt at a time, runs it with
  `Session.Send` on a **server-lifetime context**, and drains the iterator to
  `Idle` before the next. One prompt = one turn.
- `delivery:"steer"`: the server calls `Session.Steer(prompt)` (admit-only, no
  run) and returns; the active turn absorbs it at the next step boundary. If no
  turn is running, it stays queued until the next `queue` prompt drives a worker
  run.
- A prompt whose `Send` fails (e.g. admission error) does not kill the worker:
  the failure surfaces as an `ErrorEvent` on the session stream, and the worker
  continues with the next prompt.
- **A parked turn blocks the queue.** When the running turn parks on a
  permission/question ask (§9), the worker is blocked inside `Send`, so any
  prompts queued behind it wait until the ask is resolved (reply, the
  `InteractionTimeout` watcher, `/interrupt`, or `Client.Close`). This is the
  serialization guarantee, but callers should know a stuck ask stalls the whole
  session, not just the current turn.

Why not call `Send` concurrently (one goroutine per prompt)? `Session.Send`
subscribes to **session-scoped**, not turn-scoped, events, and `coord.Run`
merely *joins* an already-active run instead of queuing a new one. A prompt
admitted after the running loop's final pending check can then be stranded until
the next `Send`. A single per-session worker removes the race.

The prompt response is **not** the event source: the worker's iterator is drained
only to drive execution, and the client reads events from
`/api/session/{id}/event` (or `/api/event`). The turn keeps running after the
request returns and after the SSE connection drops.

**Open the stream before (or with) the prompt.** A client that prompts first and
subscribes after can miss live-only deltas emitted in between; open
`/api/session/{id}/event` first (or reconnect with the last known `?after=<seq>`,
which recovers the durable events but not the live-only deltas).

### 6.2 Sequence

```
client                                            server
  │ POST /api/session                ───────────► │ CreateSession
  │ ◄──────────────── {data:{id}} ────────────────│
  │ POST /api/session/:id/prompt     ───────────► │ enqueue; worker: Send
  │ ◄──────────────── 202 queued ─────────────────│
  │ GET  /api/session/:id/event      ───────────► │ replay(Durable) + live
  │ ◄──── server.connected ────────────────────────│
  │ ◄──── session.next.text.delta ────────────────│
  │ ◄──── session.next.tool.called ───────────────│
  │ …                                                    (turn runs)
  │ ◄──── session.next.step.ended ────────────────│
  │ ◄──── session.status {status:{type:"idle"}} ──│
```

### 6.3 SSE wire format

Mirrors opencode's event handler:

```
event: message
data: {"id":"evt_…","type":"session.next.text.delta","data":{…}}
<blank line>

: heartbeat
<blank line>          ← every KeepAlive (default 15s)
```

- The first frame is always `{"type":"server.connected","data":{}}`.
- Frames are named `message`; the payload `type` discriminates.
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache,
  no-transform`, `X-Accel-Buffering: no`, `X-Content-Type-Options: nosniff`.
- Backpressure: the live leg uses `AllBounded(capacity)`. On overflow the
  iterator yields `core.EventSubscriberOverflowError`; the server emits a final
  `event: error` frame — `data: {"error":"subscriber_overflow","capacity":N}` —
  and closes, so a slow client cannot stall the agent loop. The durable replay
  leg is read from the DB and is not lossy.
- Resumption uses the **query parameter, not** the SSE `Last-Event-ID` header: a
  client reconnects to `/api/session/{id}/event?after=<last durable seq>`. Frames
  carry no SSE `id:` field (matching opencode).

### 6.4 Event envelope and event data

The wire type is the SDK's canonical payload, not the high-level `agent.Event`
union (which drops live-only and intermediate payloads and has no JSON
discriminator). A small DTO maps `schema.EventPayload`:

```go
type wireEvent struct {
    ID       string         `json:"id"`
    Type     string         `json:"type"`
    Data     any            `json:"data"`
    Durable  *wireDurable   `json:"durable,omitempty"`
    Location *wireLocation  `json:"location,omitempty"`
    Metadata map[string]any `json:"metadata,omitempty"`
}
type wireDurable struct {
    AggregateID string `json:"aggregateID"`
    Seq         int    `json:"seq"`
    Version     int    `json:"version"`
}
// schema.LocationRef has no JSON tags, so it is mapped explicitly.
type wireLocation struct {
    Directory   string  `json:"directory"`
    WorkspaceID *string `json:"workspaceID,omitempty"`
}
```

`schema.EventPayload` has no JSON tags, so this DTO is defined in `server/wire.go`
(we do not mutate the shared schema's marshaling — the same applies to
`schema.LocationRef`).

**Event `data` is `any` on the wire; clients parse it by `type`.** The shape is
not schema-validated and there is no typed event union or generated client (§11).
The field names are declared by `schema.EventDefinition.Schema` (e.g.
`session.next.text.delta` →
`{sessionID, assistantMessageID, textID, delta, timestamp}`) and the high-level
`agent.Event` union documents their meaning. `type` values include the
`session.next.*` family (e.g. `session.next.shell.started`,
`session.next.step.ended`), `todo.updated`, `session.status`, and the
`permission.v2.*` / `question.v2.*` / `mcp.*` families.

---

## 7. Session handle management

`Client.CreateSession` / `ResumeSession` attach a `*Session` and track it;
attaching the same id twice fails, and every handle also installs a per-session
event subscription. The server therefore keeps **one shared handle per session**,
guarded by a per-session lock, and evicts it when the session is idle and
unreferenced — caching one handle per session forever would leak a subscription
per session.

```go
type Server struct {
    client *agent.Client
    mu       sync.Mutex
    sessions map[string]*sessionEntry
}
type sessionEntry struct {
    handle *agent.Session // lazily attached via CreateSession / ResumeSession
    worker *worker        // non-nil while a prompt queue is being drained (§6.1)
    mu     sync.Mutex     // guards handle creation / eviction, NOT the turn
}
```

- `POST /api/session` → `CreateSession` (with `InboundPermissions`), capture its
  info, then **close the handle**: the session row persists, so an unused session
  keeps no subscription. The entry is created lazily on first use.
- `attach(id)` takes the server mutex only for a **short critical section**:
  return the entry's handle or `ResumeSession` it **once** (single-flight; two
  concurrent prompts must not both call `ResumeSession` — the SDK rejects the
  second `trackSession`). The lock is **never held across a turn**.
- The worker then calls `Session.Send` and blocks for the whole turn **without
  holding any lock**, so `/interrupt`, `/steer` and the `Set*` switches still
  reach the running turn. Concurrency safety comes from the SDK: `Abort`,
  `Steer`, `SetAgent`/`SetModel`/`SetTitle` are safe while a turn runs, and
  `Compact` self-guards (`ErrSessionBusy` → 409). **Revert is rejected (409)**
  while a turn is active because it mutates files.
- `ResumeSession` keeps the stored agent; it is passed
  `Permissions: core.PermissionMerge(stored, Options.InboundPermissions)` so the
  API ruleset is **layered over** (not substituted for) the host's stored
  per-session rules (see below).
- `DELETE` → **abort and wait out** the worker's in-flight turn (via
  `Session.Abort`; `DeleteSession` alone does not stop a running turn), then
  close the handle, evict the entry, and `DeleteSession` (which cascades to child
  sessions and their event logs). Otherwise the delete races the runner's writes
  and the event log removal.
- Eviction closes the handle and drops the entry once the worker has drained
  **and** no transient operation is in flight (ref-count / quiescence), so a live
  call never runs on a closed handle. Transient ops therefore *attach → operate →
  release* and a session idle between uses is simply resumed next time (a small
  cost) instead of holding a subscription forever. `Server.Close()` closes the
  rest.

Read paths (`ListSessions`, `SessionMessages`) use `Client` methods directly and
do not require a handle; child (`task`) sessions are read the same way and their
events arrive on the global stream.

**Resumed-session permissions.** `ResumeSession` keeps a session's stored
ruleset only when no ruleset is passed; a passed ruleset **replaces** it
(`overrideStoredPermissions`, `client_session.go`). Because substitution would
discard a host's per-session policy, the server instead reads the stored ruleset
(`GetSession().Permission`), merges `Options.InboundPermissions` over it
(`core.PermissionMerge`, inbound wins on conflicts), and passes the result: the
API ruleset is enforced on every externally driven session **without clobbering**
the host's rules. The merged set is persisted and the merge is idempotent across
reconnects. A2A's `a2aFactory.ResumeSession` now merges the same way, so both
transports behave identically.

---

## 8. Security

### 8.1 Authentication

```go
type Auth struct {
    Scheme   string // "bearer" (default) | "basic"
    Token    string // bearer
    Username string // basic
    Password string // basic
}
```

- Bearer is the default — the natural fit for service-to-service foreign SDKs
  and consistent with `a2a/server.Security`.
- Basic is offered for opencode-client compatibility.
- `New` rejects an `Auth` with an unknown `Scheme` or a missing credential
  (bearer token / basic password), mirroring `a2a/server.Security`.
- These are **inbound API credentials**. Do not confuse them with the model
  provider `credentials` in `cmd/agentserve` (§12), which authenticate the SDK's
  outbound LLM calls.
- Credentials are constructor inputs; the SDK **never reads env** (unlike
  opencode's `OPENCODE_SERVER_PASSWORD`). Config files are the only secret
  source (see `cmd/agentserve`).
- Constant-time comparison; missing/invalid → `401` with `WWW-Authenticate:
  Bearer` (or `Basic realm="..."`).
- `Options.Public` lists unauthenticated routes (typically only `/api/health`);
  entries match the request path **exactly** (no prefix matching), so a public
  route cannot accidentally expose a subtree.
- Auth is header-based with **no cookies**, so CSRF is not applicable; keep it
  that way (do not move to cookie sessions without adding CSRF protection).
- CORS preflight (`OPTIONS`) is answered by the CORS middleware and never reaches
  a handler, so it bypasses auth; the actual request is still authenticated.
- **SSE + browsers**: `EventSource` cannot set an `Authorization` header. When
  CORS/browser use is enabled, also accept the token as
  `?auth_token=<token>` (opencode's approach); service clients keep using the
  header. The query form can leak via logs, history and referrers, so it is a
  fallback only. If browser use is out of scope, SSE requires the header and is
  not usable from `EventSource`.

### 8.2 CORS

Browser clients (a host-built UI) need CORS; non-browser clients send no
`Origin` and are unaffected. By default only same-origin and loopback origins
(`http://localhost:*`, `http://127.0.0.1:*`) are allowed, matching opencode;
`Options.CORS` / `--cors` add more. The preflight response must allow the
`Authorization` and `Content-Type` headers and the method set the API uses
(`GET`/`POST`/`DELETE`), or browser calls fail before reaching a handler.

### 8.3 Authorization / inbound permissions

Sessions created through the API run under `Options.InboundPermissions` merged
over the agent's ruleset; **resumed non-API sessions keep their own stored rules**
(§7), so uniform enforcement requires creating sessions through the API. The
default reuses `agent.DefaultInboundPermissions()`, which denies `bash`, `write`,
`edit`, `apply_patch` and `external_directory` — no code execution and no file
mutation. It does **not** restrict read-only tools, `webfetch`, or `task`: a
public caller can still read project files, make outbound HTTP requests
(`webfetch` is an SSRF/exfiltration vector) and delegate to subagents. A host
exposing the server to untrusted users should extend the ruleset (e.g. deny
`webfetch` and `task`); a trusted host widens it deliberately. The same ruleset
gates HITL: `question`/`task` are permission-checked tools, so denying them
disables those asks (§9). API-level authorization (which agents may be used at
all) is separate — that is the `agents` allow-list (§12.4).

### 8.4 Bind policy

The SDK binds nothing. `cmd/agentserve` mirrors `cmd/a2aserve`: loopback by
default, **refuse a non-loopback bind without auth** unless `--allow-insecure`
is set. HTTPS is available only through the config file
(`tls.certFile` / `tls.keyFile`), not a flag. Because the SDK itself never binds,
a host that mounts `Handler()` on a public address owns this check — `server.New`
does not know the eventual bind address.

### 8.5 Other hardening (host responsibility)

The SDK does not implement limits that belong to the deployment:

- **Request body size** — cap it at the host / reverse proxy; large prompts or
  `files` payloads otherwise reach memory unbounded.
- **Rate limiting / concurrency** — not provided; a reachable server should be
  fronted by a limiter.
- **TLS policy** (min version, ciphers) follows the configured cert/key; the SDK
  has no TLS option surface.

---

## 9. Human-in-the-loop (hold mode)

On `New`, the server calls `client.HoldInteractions(true)`: permission and
question prompts no longer call the registered handlers but block the turn until
the client replies over HTTP. This is required — otherwise the default
`ApproveAll` / `RejectQuestions` would silently resolve them and the HTTP client
would never see a prompt.

**HITL is downstream of the tool/permission model.** A `permission.v2.asked`
appears only when a tool call resolves to `ask` under the effective ruleset
(agent ∪ session ∪ `Options.InboundPermissions`). A `question.v2.asked` appears
only when the built-in `question` tool is available to the agent **and** its own
`Assert(action:"question")` does not deny — the tool is permission-gated like any
other (`core/tool/question.go:102`); if that action resolves to `ask`, a
permission prompt precedes the question. Tools denied by the ruleset are filtered
out of the materialized set, so they are never offered to the model. Consequence:
a ruleset that denies `question` disables HITL questions entirely, and denying
`task` disables subagent asks (the `task` tool asserts action `task`,
`task.go:194`). The default `Options.InboundPermissions` denies neither, so both
work out of the box.

**Only `ask` parks.** `allow` and `deny` resolve immediately in
`Permission.Assert` and emit no `permission.asked`, so a ruleset that allows a
tool (or denies it) never prompts. That said, the default when **no rule
matches** is `ask` (`PermissionEvaluate`, `core/permission.go:117`), so silencing
permission prompts requires an explicit allow rule such as
`{action:"*",resource:"*",effect:allow}` — an empty ruleset still asks. This is
about permission only: `question.asked` is the `question` tool's own output, not
a permission event, so allow-all does not remove questions themselves, only the
permission gate in front of them.

Flow:

```
a tool call resolves to "ask"
  → permission.asked on the LIVE event stream (never on durable replay)
  → client POST …/permission/{id}/reply {reply:"once"}
  → Permission().Reply → the blocked turn resumes
```

- **Delivery: live only.** `permission.v2.asked` / `question.v2.asked` are not
  durable (§5.4), so they arrive on `/api/event` (or the live leg of
  `/api/session/{id}/event`) and never through durable replay. A client that was
  disconnected when the turn parked must recover the pending request from
  `/api/permission` / `/api/question` (polling) before replying.
- **Subagent asks.** A `task` child session raises its asks under the **child**
  `sessionID`, so they appear on the global `/api/event` but not on the parent's
  per-session stream. Subscribe globally (or to the child session) to answer
  them.
- **Interrupt releases questions, not permissions.** `Question.Ask` selects on
  the run context (`core/question.go`), so `Session.Abort` / `/interrupt`
  releases a parked question. `Permission.Assert` does **not** observe the
  context (`core/permission.go:361`, a bare `<-item.done`), so `Abort` cannot
  release a parked permission: it is settled only by
  `POST …/permission/{id}/reply` (reject works), by the `InteractionTimeout`
  watcher, or by `Client.Close()`. `/interrupt` should therefore also reject the
  session's pending permissions (`Permission().ForSession` + `Reply`): the reject
  unblocks the assert, and the cancelled context then settles the turn. Rejecting
  **without** aborting only surfaces a declined tool to the model, so pair the
  two.
- **Reply semantics.** `reply:"always"` persists a saved rule (only when the
  request carries `save` resources), so later calls of the same action/resource
  skip the prompt — it survives restarts (`permissionSaved`). `reply:"reject"`
  with `message` returns that text to the model as feedback
  (`PermissionCorrectedError`); `question` replies carry one label list per
  question.

Risks and mitigations:

- **Wedge**: if no client ever replies, the turn blocks forever. Mitigation:
  `Options.InteractionTimeout > 0` runs a watcher that resolves stale requests
  via `Options.InteractionFallback` (`reject` by default; `allow` is an explicit,
  dangerous opt-in for permissions). A bot that only supports text (no approval
  UI) can set a short timeout, or set deny-by-default rules and never hold.
  Pending requests carry no timestamp, so the watcher records arrival time by
  subscribing to the live `permission.asked` / `question.asked` events and, on
  timeout, resolves the request via `Permission().Reply` /
  `Question().Reply` | `Reject`. `Client.Close()` settles all pending requests
  (decline/reject), so shutdown always unblocks a parked turn even if the
  watcher is disabled.
- **Shared mode**: `HoldInteractions` is a global flag on the `Client`, so it
  affects **every** user of that client — the HTTP server, the A2A server, and
  any in-process `Session.Send`. Do not share one `Client` between the HTTP API
  and A2A, and expect in-process callers to block on asks too; use separate
  clients, or one transport per client. `Server.Close()` does **not** reset it,
  so a host reusing the client in-process afterwards must call
  `HoldInteractions(false)` itself.

---

## 10. Error model

Uniform JSON envelope (opencode-style) plus an HTTP status:

```jsonc
{ "error": "not_found", "message": "session not found: ses_…", "sessionID": "ses_…" }
```

`error` is a stable machine code, `message` is human-readable, and any further
fields are request-specific context (`sessionID`, `requestID`, `field`, `ref`).

| `error` | Status | SDK source |
|---|---|---|
| `bad_request` | 400 | JSON decode / validation / unknown agent |
| `unauthorized` | 401 | missing/invalid credential |
| `forbidden` | 403 | agent outside the `agents` allow-list (§12.4) |
| `not_found` | 404 | session / message / interaction request missing; unknown path |
| `method_not_allowed` | 405 | router: wrong HTTP method |
| `conflict` | 409 | `ErrSessionBusy` (compact while a turn runs) |
| `unavailable` | 503 | coordinator stopped (`session.ErrStopped`); snapshots unavailable for revert |
| `internal` | 500 | unexpected error (message redacted, `ref` logged) |

Handler errors are mapped centrally in `server/errors.go`; internal causes are
logged server-side and not leaked to the client.

Two cases sit outside the table:

- **Middleware**, not handlers, produces the rest. A recover wrapper turns a
  handler panic into `500 internal` (never a dropped connection), and the
  router's own **404** (unknown path) and **405** (wrong method) are normalised
  into the same envelope (`not_found` / `method_not_allowed`) instead of
  `net/http`'s plain text.
- **After an SSE stream starts**, the status and headers are already sent, so an
  error cannot use this envelope: it arrives as an `event: error` frame and the
  stream closes (§6.3). Clients must handle both error shapes.

**SDK errors are mixed.** Some are sentinels (`session.ErrStopped`,
`ErrSessionBusy`); others are plain `fmt.Errorf` strings — "unknown agent: X"
(`CreateSession`), "session not found: X" (`ResumeSession`, `revertSession`). The
mapping classifies what it can by error type and falls back to matching those few
known strings. Adding typed errors (`ErrUnknownAgent`, `ErrSessionNotFound`) is a
recommended SDK improvement, not a blocker (§16 B1).

---

## 11. Wire contract and clients

**The contract is this document**, plus the code and the end-to-end test in
`server/server_test.go`. There is **no OpenAPI document and no generated SDK**:
a hand-maintained spec with no code-generation consumer would only drift from the
handlers.

**We deliberately did not copy opencode's schema-first pipeline.** opencode
defines each route as an Effect `Schema` and generates its client from
`OpenApi.fromApi(...)`. We rejected the Go analogue as **not cost-effective**:

- **SSE — the core value — is not expressible in OpenAPI**, so a generated client
  still needs hand-written streaming code.
- The consumer set is small and known (first: a Python/Node WeCom bot), and **that
  client belongs in the consumer's repo, not here** — this repo ships the Go
  server and a Go example/test, not another language's SDK.
- Go has no Effect-Schema equivalent; matching opencode would mean extending
  `jsonschema` with `$defs`/`$ref` **and** `oneOf`/`discriminator`, typing ~40
  event schemas, and writing an OpenAPI emitter — a large build for a need a
  small hand-written client covers.

Consequences:

- The server uses `net/http` and plain Go structs (json tags) for encode/decode;
  `jsonschema` stays confined to the **tool** framework.
- **No typed event union and no generated SDK**; event `data` is documented by
  `type` (§6.4) and each consumer maps it however it likes.
- To exercise the API end to end, this repo provides a Go example/test that
  drives `session → prompt → SSE → permission reply`; the WeCom bot, in another
  language, consumes the same wire contract.
- If the consumer set ever grows (Go/Java/many clients), schema-first can be
  revisited.

---

## 12. `cmd/agentserve`

A standalone application (not part of the SDK API), parallel to `cmd/a2aserve`:
**YAML config file + CLI flags, no environment variables**. It builds the
`Client`, constructs `server.New(client, opts)`, and runs an `http.Server` with
`Handler()` and graceful shutdown — the same lifecycle as `cmd/a2aserve`.

Configuration precedence is **defaults < file < flags**; a flag the user set
overrides the file, and a repeatable flag **replaces** the file's list. Unknown
**file keys are rejected** (`yaml.Decoder.KnownFields(true)`). **Secrets live
only in the file** (credentials, auth token) — flags would leak into the process
list; the same rule as `cmd/a2aserve`.

### 12.1 Config file

An annotated example will ship as `cmd/agentserve/agentserve.example.yaml` (copy
to `agentserve.yaml`, which is git-ignored). The complete field set:

| YAML key | Type | Default | Required | Meaning |
|---|---|---|---|---|
| `listen` | string | `127.0.0.1:8787` | no | HTTP listen address `host:port` |
| `root` | string | — | **yes** | SDK state directory (`ClientOptions.Root`); the only state location |
| `workingDirectory` | string | process cwd | no | Project/worktree root (agent tools operate here) |
| `configDirs` | []string | `[]` | no | Agent/skill markdown directories (`ClientOptions.ConfigDirs`) |
| `model` | string | — | **yes** | Default model id |
| `protocol` | string | — | **yes** | Wire protocol id, e.g. `openai-responses` |
| `modelBaseURL` | string | protocol default | no | Model API base URL (non-default hosts); for `bedrock-converse` derived from `credentials.aws.region` when empty |
| `credentials.apiKey` | string | — | yes\* | Bearer/header protocol API key (secret) |
| `credentials.aws.region` | string | — | yes\* | Bedrock region |
| `credentials.aws.accessKeyId` | string | — | yes\* | Bedrock access key (secret) |
| `credentials.aws.secretAccessKey` | string | — | yes\* | Bedrock secret (secret) |
| `credentials.aws.sessionToken` | string | `""` | no | Bedrock session token (secret) |
| `contextLimit` | int | `0` | no | Model context window (tokens); drives auto-compaction |
| `outputLimit` | int | `0` | no | Model output budget (tokens) |
| `subagentDepth` | int | `1` | no | Max nested `task` subagent recursion (`0` = default, i.e. 1) |
| `titleAuto` | bool | `false` | no | Auto-generate session titles via the `title` agent |
| `agents` | []string | `[build]` | no | **Allow-list** of agent names the API may create/use (see 12.4) |
| `auth.bearer` | string | — | no\*\* | Bearer token required on all routes except `publicRoutes` (secret) |
| `auth.username` | string | `opencode` | no | Basic-auth username (alternative to bearer; secret with password) |
| `auth.password` | string | — | no\*\* | Basic-auth password (secret) |
| `inboundPermissions` | []rule | SDK deny-by-default | no | Ruleset merged over the agent for API-created sessions |
| `interactionTimeout` | duration | `0` (disabled) | no | Auto-resolve a held permission/question after this long |
| `interactionFallback` | string | `reject` | no | Fallback effect on timeout: `reject` \| `allow` |
| `subscriberCapacity` | int | `256` | no | Bound on each live SSE subscriber's queue |
| `keepAlive` | duration | `15s` | no | SSE heartbeat interval; negative disables |
| `publicRoutes` | []string | `[/api/health]` | no | Route patterns that skip auth |
| `cors` | []string | `[]` | no | Browser origin allow-list (extra to the loopback default) |
| `tls.certFile` | string | — | no\*\*\* | HTTPS certificate file |
| `tls.keyFile` | string | — | no\*\*\* | HTTPS key file |
| `allowInsecure` | bool | `false` | no | Permit a non-loopback bind without auth (explicit opt-out) |

\* exactly one credential source: `credentials.apiKey` **or** `credentials.aws`.
\*\* `auth` is optional, but a non-loopback bind **requires** it unless
`allowInsecure`. Within `auth`, `bearer` takes precedence; otherwise a complete
`username` + `password` selects Basic.
\*\*\* `tls` requires both files; when set the server serves HTTPS.

> **`inboundPermissions` replaces, it does not extend.** When the key is present
> the server uses exactly those rules as `Options.InboundPermissions`; the SDK's
> `DefaultInboundPermissions()` is applied only when the key is absent. The
> example above therefore denies only `bash` — a host that wants the other
> defaults (`write`/`edit`/`apply_patch`/`external_directory`) must list them
> explicitly, or omit the key.

```yaml
listen: 127.0.0.1:8787
root: ~/.go-agent-sdk             # required
workingDirectory: /srv/project
configDirs: [/srv/project/.agents]
model: deepseek-flash             # required
protocol: openai-compatible-chat  # required
modelBaseURL: https://api.deepseek.com
credentials:                      # required; the only secret source (no env)
  apiKey: sk-...                  # or credentials.aws: { region, accessKeyId, secretAccessKey, sessionToken }
contextLimit: 128000
outputLimit: 8192
subagentDepth: 1
titleAuto: false
agents: [build]                   # allow-list of usable agents
auth:
  bearer: <token>                 # optional; required for non-loopback binds
inboundPermissions:               # REPLACES the SDK deny-by-default (not additive)
  - { action: bash, resource: "*", effect: deny }
interactionTimeout: 5m            # optional HITL fallback
interactionFallback: reject
subscriberCapacity: 256
keepAlive: 15s
publicRoutes: [/api/health]
cors: []                          # extra browser origins
# tls:                            # optional; serve HTTPS when both are set
#   certFile: /etc/agentserve/tls.crt
#   keyFile: /etc/agentserve/tls.key
allowInsecure: false
```

### 12.2 CLI flags

Each flag overrides the file (only flags the user actually set are applied); a
repeatable flag replaces the file's list. **Secret fields have no flags**
(credentials, `auth.*`) — they are file-only.

| Flag | Overrides | Meaning |
|---|---|---|
| `--config <path>` | — | YAML config file |
| `--listen <host:port>` | `listen` | Listen address |
| `--root <dir>` | `root` | SDK state directory (required) |
| `--working-dir <dir>` | `workingDirectory` | Project root (default: cwd) |
| `--config-dir <dir>` | `configDirs` | Agent/skill dir (repeatable) |
| `--model <id>` | `model` | Default model (required) |
| `--protocol <id>` | `protocol` | Wire protocol (required) |
| `--model-base-url <url>` | `modelBaseURL` | Model API base URL |
| `--subagent-depth <n>` | `subagentDepth` | Max nested `task` subagent depth |
| `--title-auto` | `titleAuto` | Enable automatic session titles |
| `--agent <name>` | `agents` | Usable agent (repeatable) |
| `--interaction-timeout <dur>` | `interactionTimeout` | HITL fallback timeout |
| `--interaction-fallback <v>` | `interactionFallback` | `reject` \| `allow` |
| `--public-route <pattern>` | `publicRoutes` | Unauth route pattern (repeatable) |
| `--cors <origin>` | `cors` | Browser origin (repeatable) |
| `--subscriber-capacity <n>` | `subscriberCapacity` | SSE subscriber queue bound |
| `--keep-alive <dur>` | `keepAlive` | SSE heartbeat interval |
| `--allow-insecure` | `allowInsecure` | Allow non-loopback bind without auth |

Usage:

```sh
agentserve --config agentserve.yaml
# or flags only (credentials and auth still need the file)
agentserve --root ~/.go-agent-sdk --model deepseek-flash \
  --protocol openai-compatible-chat --model-base-url https://api.deepseek.com \
  --config-dir ./agents
```

### 12.3 Validation

Startup fails, before binding, on any of:

- `root`, `model`, `protocol` missing;
- `agents` empty;
- neither `credentials.apiKey` nor `credentials.aws` set;
- `credentials.aws` set but missing `region` / `accessKeyId` / `secretAccessKey`;
- `auth` set with neither `bearer` nor a complete `username` + `password`;
- `interactionFallback` not one of `reject` / `allow`;
- an `inboundPermissions` rule with an unknown `effect` (not `allow` / `deny` /
  `ask`);
- a **non-loopback** `listen` without auth and without `allowInsecure`;
- `tls` set with only one of `certFile` / `keyFile`;
- negative `contextLimit` / `outputLimit` / `subagentDepth` / `subscriberCapacity`
  (a `subscriberCapacity` of `0` means "use the default", not invalid);
- any duration unparsable; `interactionTimeout` negative (`keepAlive` negative
  is allowed — it disables the heartbeat);
- any agent name in `agents` unknown to the registry (checked after the client is
  built, before binding);
- any unknown YAML key.

This mirrors `cmd/a2aserve`'s validation exactly, plus the HTTP-only checks
(`interaction*`, `subscriberCapacity`, `cors`).

### 12.4 `agents` as an access-control allow-list

`agents` names the registered agents the API may use. A `POST /api/session`
naming an agent outside the list, or a per-session `POST …/agent` switch to one,
is rejected (`403 forbidden`). Listing an unknown agent name fails at startup.
This is the HTTP analog of `cmd/a2aserve`'s `agents` (which selects exposed
agents), and it is why the default is `[build]` rather than "all".

When `POST /api/session` omits `agent`, the server uses the **first entry of the
allow-list** — not a hardcoded `build` — so `agents: [plan]` works for
agent-less requests. (Relying on `CreateSession`'s `build` default would make an
agent-less request fail whenever the list omits `build`.)

**Scope: the primary agent only.** The allow-list gates the agent a session runs
as (and `POST …/agent` switches to). Subagents reached through the `task` tool
are not checked against it — they are governed by the `task` permission and the
agent's own subagent list, exactly as in-process. A host that must constrain
which subagents a turn may delegate to does so through `InboundPermissions`
(e.g. deny `task`, or the specific subagent types).

### 12.5 Lifecycle

`agentserve` builds the `Client`, wires `server.New(client, opts)` with the
derived `Options` (`Auth`, `Agents`, `CORS`, `InboundPermissions`,
`InteractionTimeout`, `InteractionFallback`, `KeepAlive`, `SubscriberCapacity`,
`Public`), then on `SIGINT`/`SIGTERM`: `Server.Close()` (cancels SSE subscribers
and signals workers) → `httpServer.Shutdown(ctx)` (now completes; still bounded
by a timeout) → `Client.Close()`. `Server.Close()` must not block on parked
workers (§13); `Client.Close()` settles pending interactions and stops turns.
Order matters: running `Shutdown` first would make it wait out its whole timeout
on live SSE streams (§13).

### 12.6 Out of scope for the command

The YAML/flag surface covers the SDK's **data** inputs (model, credentials,
agent/skill dirs, permissions, HTTP), and will gain **`mcp` and `references`**
sections — both are data, aligning with v2's config model (§16 S3). The genuinely
**code-only** extension points, `Plugins` and custom `Tools`, are not
expressible here: a host that needs them embeds the SDK directly
(`agent.NewClient`) and mounts `server.Handler()` itself.

---

## 13. Concurrency and lifecycle

- Turns are serialized per session by the SDK coordinator; different sessions
  run concurrently. `prompt` returns immediately; the per-session worker (§6.1)
  runs on a server-lifetime context so a disconnected client does not cancel the
  turn.
- SSE subscribers are bounded (`SubscriberCapacity`); on overflow the stream
  errors and closes rather than blocking the agent loop. The client reconnects
  and recovers persisted state from durable replay; live-only deltas produced in
  the gap are accepted as lost.
- `Server.Close()` unsubscribes, cancels SSE streams, and signals the workers to
  stop waiting. It does **not** abort an in-flight turn: cancelling the `Send`
  context only stops the caller waiting (SDK semantics), and it cannot release a
  parked permission ask either (§9). It must **not block** waiting for a worker.
  `Client.Close()` (called after, §12.5) is what actually stops turns
  (`stopTurns`) and settles pending interactions.
- **SSE and graceful shutdown.** An SSE stream lives until the client goes away,
  so `http.Server.Shutdown` — which waits for active requests — would block on it
  for the whole shutdown timeout. `Server.Close()` therefore cancels every SSE
  subscriber's context (closing the streams) and signals the workers, and the
  lifecycle runs it **before** `Shutdown` (§12.5).
- **HTTP server timeouts.** Set `ReadHeaderTimeout` (and a body limit at the
  proxy, §8.5); do **not** set a short `WriteTimeout`, which would cut SSE
  mid-stream. Boundaries come from `Server.Close()` and the SSE keep-alive.

---

## 14. Roadmap

| Phase | Content |
|---|---|
| 0 | `server/` skeleton: `New`/`Handler`, health, error envelope (recover + 404/405 normalisation), auth, CORS. Add `Client.EventStream`, `GetSession`, `WaitSession`, and sentinel errors. **Hold mode deferred to Phase 3** (below). |
| 1 | Sessions + messages: CRUD, prompt (per-session worker), interrupt, compact, wait, switch agent/model/title; `agents` allow-list enforcement; the request/response wire DTOs; **SDK full-message read** for `/message` + `/context` (B4). |
| 2 | Event SSE: global `/api/event`, merged replay+live `/api/session/{id}/event`, heartbeat, the `wireEvent` envelope. |
| 3 | Interactions: enable `HoldInteractions(true)` in `New`, plus permission/question list/get/reply/reject + timeout policy (via live stream/polling). |
| 4 | `cmd/agentserve` (YAML + flags, graceful shutdown). |
| 5 | Go end-to-end example/test driving a real session (`session → prompt → SSE → permission round-trip → reconnect`). |

Every phase ships `httptest`-based tests (auth, error mapping, worker
serialization, SSE replay/dedup, HITL); Phase 5 adds the full end-to-end
round-trip.

> **Status:** Phases 0–3 and the `cmd/agentserve` command (Phase 4) are
> implemented in `server/` and `cmd/agentserve/`; the SDK additions are
> `Client.EventStream` / `GetSession` / `WaitSession` / `SessionHistory` and the
> `ErrUnknownAgent` / `ErrSessionNotFound` sentinels. Phase 5's end-to-end
> coverage lives in `server/server_test.go`.

**Sequencing note.** `New` sets `HoldInteractions(true)` (§9), which parks every
`ask` until a reply arrives. Until the interaction routes exist (Phase 3),
enabling hold would wedge a turn with no way out — so Phases 0–2 run with the
default handlers and hold is switched on only in Phase 3 (or Phase 3's reply
routes must land together with it).

---

## 15. Decision list

| # | Decision | Rationale |
|---|---|---|
| D1 | New `server/` package + `cmd/agentserve`; not folded into `a2a/` | HTTP host-driven control plane ≠ A2A; shape modeled on `packages/server` + `packages/protocol` (internals not ported, §3) |
| D2 | `server/` imports root `agent`; no root-side adapter (unlike A2A) | HTTP is inherently coupled to the public API; wire DTOs are added only where an SDK type leaks secrets (§5.1), not as a parallel interface layer |
| D3 | `prompt` = enqueue + per-session worker + return `202`; **no unbounded request-holding** (only `/wait`, bounded by `timeout`) | Survives bot webhook timeouts and disconnects |
| D4 | Wire events = raw `schema.EventPayload` DTO + `server.connected` | Full fidelity, stable discriminator, matches opencode's `/api/event` |
| D5 | Bearer auth by default (Basic optional); no env | Service-to-service foreign SDKs; SDK philosophy; consistent with `a2a/server.Security` |
| D6 | Server holds interactions (`HoldInteractions(true)`) with a timeout fallback | Required to surface prompts; timeout prevents a wedged turn |
| D7 | No `location` scoping; one `Client` per handler | The SDK `Client` is already bound to one project/root |
| D8 | Document the wire contract here; **no OpenAPI document and no generated SDK** | SSE isn't OpenAPI-expressible, a hand-maintained spec would drift, and the consumer's client belongs in its own repo (§11) |
| D9 | Expose `Client.EventStream()`; add `GetSession`/`WaitSession` | Adapters need the raw bus; the two read helpers keep read paths from attaching handles |
| D10 | One prompt **worker per session** (not one goroutine per prompt) | `Send` is session-scoped; concurrent `Send`s can strand a queued prompt |
| D11 | Per-session stream = durable replay **merged with** live filtered | Deltas and HITL are live-only; replay alone carries no output and no asks |
| D12 | Event `data` is documented by `type`, not typed via OpenAPI | No codegen; the client parses per the documented field lists (§6.4, §11) |
| D13 | One shared `*Session` handle per session, shared by the worker and transient ops; the lock guards only creation/eviction, never the turn; evicted when idle | Each handle installs an event subscription (a permanent cache leaks one per session), and holding the lock across a turn would block `/interrupt` (§7) |
| D14 | The `agents` allow-list is the API-level authorization, separate from the tool permission system | "Which agent may be used" ≠ "which tools a turn may run" (§8.3, §12.4) |
| D15 | Lifecycle order `Server.Close()` → `http.Server.Shutdown` → `Client.Close()` | Live SSE keeps `Shutdown` waiting; cancel the streams first (§13) |
| D16 | Uniform error envelope: stable `error` code + HTTP status, mapped centrally; post-start SSE errors are `event: error` frames | One shape for clients; sentinel SDK errors make the mapping non-stringly (§10) |
| D17 | `cmd/agentserve` configures data inputs and gains `mcp`/`references` sections (align v2); `Plugins`/`Tools` stay code-only | MCP/references are data (§16 S3); plugins/tools are Go code (§12.6) |
| D18 | `inboundPermissions`, when set, **replaces** the SDK deny-by-default ruleset (it is then *merged* over a session's stored rules on resume — D19) | Matches `cmd/a2aserve`'s `WithInboundPermissions`; no surprising implicit merge *of the default* (§12.1) |
| D19 | HTTP `ResumeSession` **merges** `InboundPermissions` over the stored session rules (not substituting) | Enforces the API ruleset on every externally driven session without discarding the host's per-session policy (§7) |

---

## 16. Alignment tasks and decisions

Items where opencode v2 has a counterpart, cited per item. Align where it adds
value; not every v2 feature is needed here — e.g. **typed event schemas existed
only to drive code generation, which we dropped (§11)**, so they are no longer
listed. The Go-specific surface (inbound permissions, `agents` allow-list, HITL
timeouts, bind policy) has no TS counterpart; its policy choices are called out
below.

### Align with v2

- **B1. Typed errors (recommended).** v2 uses tagged error classes carrying
  `httpApiStatus` (`packages/protocol/src/errors.ts`). The SDK mixes sentinels
  with plain `fmt.Errorf` strings; adding typed errors (`ErrUnknownAgent`,
  `ErrSessionNotFound`) makes the HTTP mapping exact instead of partly
  string-based (§10). *Not blocking.*
- **B3. Prompt id.** v2's `session.prompt` accepts an optional `id` and returns
  `SessionInput.Admitted`, with `ConflictError` on duplicates; adopt the same.
- **B4. Messages vs context.** v2 exposes both `session.messages` (full) and
  `session.context` (post-compaction); `/message` returns the full transcript and
  `/context` the active context. *Landed: `Client.SessionHistory` /
  `Store.Messages`.*
- **B6. `session.active` / `session.history` endpoints.** v2 exposes a
  running-session list and a finite durable-event page; this draft omits both.
  The history page is a one-shot `Durable` read (bounded, unlike the SSE stream);
  the active list needs a small SDK read.
- **S1. Paging.** v2 uses opaque `SessionsCursor` / message cursors; implement
  the same (replacing this draft's full-list + `limit` ≤ 200).
- **S2. CORS / `?auth_token`.** v2 accepts `?auth_token` on all routes and ships
  loopback CORS (`packages/server/src/middleware/authorization.ts`); keep both —
  do not drop browser support.
- **S3. MCP / references config.** v2's config/service model covers MCP (and
  references); add sections to `cmd/agentserve` (§12.6).

### Go-specific (no TS counterpart)

- **G1. Resume permissions.** *Decided and landed*: resume **merges** the inbound
  ruleset over the stored session rules (§7, D19); `a2aFactory.ResumeSession` now
  merges too, so HTTP and A2A behave the same.
- **G2. HTTP default ruleset.** *Decided*: keep the shared
  `DefaultInboundPermissions` unchanged for parity with A2A (reads, `webfetch`,
  `task` allowed — no extra `webfetch` deny). A host tightens explicitly (§8.3).
