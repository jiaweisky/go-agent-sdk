# go-agent-sdk `agentd` — multi-transport serve design

> Status: **implemented** (`cmd/agentd`; the staged plan and quality gates are in §11).
> Scope: merge `cmd/agentserve` (HTTP/SSE, `docs/DESIGN-HTTP.md`) and `cmd/a2aserve`
> (A2A, `docs/DESIGN-A2A.md`) into a single multi-transport command, **`cmd/agentd`**:
> one process, one config, N listeners, **one `Client` per server**.
> Relationship: a sub-volume of `docs/DESIGN.md`. It consumes the HTTP and A2A
> wire contracts unchanged; it changes only the **command/assembly layer**.
> Convention: Go snippets are **illustrative only** (type boundaries, not final signatures);
> **code comments must be self-contained** — no `§…` or cross-references to these docs inside
> Go code.

## Change log

| Date | Change |
|---|---|
| 2026-10-15 | `http` signing can be delegated to an external `presignURL` service (POST `{key, method, expires}` → `{uploadUrl, reference}`), mutually exclusive with the built-in `presignSecret` (nginx secure_link). |
| 2026-10-15 | Storage credential coverage: `http` gains `auth` (`none`/`basic`/`bearer`, on **both** writes and reads) plus `presignSecret` (nginx secure_link, so a private origin's url carries a ticket); `s3`/`cos` accept **no** credentials for an anonymous/public bucket (urls are plain, no presign); A2A **outbound** ingest uploads now carry the client's auth headers (previously none → 401 against a secured peer). |
| 2026-10-15 | Added a **native `cos` storage backend**: Tencent COS over its own REST API (`q-sign-algorithm=sha1`), needing no S3/SigV4. `storage.backend` ∈ `http` / `local` / `s3` / `cos`; the S3-compatible `s3` backend still reaches COS via its S3 endpoint. |
| 2026-10-15 | `media.inlineLimitBytes`/`maxBytes` accept a **per-modality** map (`{default, image, audio, video, file}`) or a bare number; added `servers[].maxBodyBytes` (HTTP request-body cap, 128 MiB default → 413); audio/video downsample is wired to **ffmpeg**; the spool dir is read-allowed. |
| 2026-10-15 | DeepSeek's OpenAI-compatible chat is its **own protocol** (`deepseek-chat`, catalog `protocol:` value) so `openai-compatible-chat` (vLLM) does not carry the `file` block; `mediaUpload` gains `expiresAfter` (provider-side `expires_after`; deletion stays the host's). |
| 2026-10-15 | Added an optional per-server `storage` block for `DESIGN-MEDIA` **{M17}/{M18}**: a provider-independent media store. Backends `http` (a PUT base → url), `local` (a shared directory), `s3` (**S3-compatible**: AWS S3 / MinIO / COS, SigV4-signed), and `cos` (**native** Tencent COS, `q-sign-algorithm=sha1`) serve the model edge **and** the A2A file exchange — the AgentCard storage extension (**`reference`** + **`broker`** modes), `PUT`/`GET(Range)`/slot-`POST` ingest endpoints, and inbound `FileWithUri` resolution. |
| 2026-10-15 | `media` block updated for `DESIGN-MEDIA` **{M17} reference hosting**: `prefer` ∈ `inline` / `url` / `file`, where `url` is produced by the provider-independent `storage` store (object storage) and `file` by the provider-bound `mediaUpload` (Files API); `url` is no longer caller-only. |
| 2026-10-15 | Added **§5.3 admin read-only endpoints**: `GET /api/config` (whitelist-redacted, structured, with provenance) and `GET /api/info` (version/pid/listen/servers). Served by agentd's router on each listener's origin-root server with that server's credential (not public by default); global scope; `?section=`/`&server=` filters; `schemaVersion`; read-only, no hot reload. Loader gains `LoadWithEnv` (records expanded `${VAR}` names). |
| 2026-10-11 | First draft: motivation, current state, target architecture, config, lifecycle, constraints, migration, testing, decisions (D1–D4). |
| 2026-10-11 | Added §5.1 (streamlined config-driven CLI, D5) and §5.2 (routing / in-process multiplexing, D6/D7); clarified the `ClientOptions` drift is **config-only** — agents/subagents/skills still load from `configDirs` in both commands. |
| 2026-10-11 | Configuration settled: a `servers[]` list (renamed from `transports`; per-entry `kind`) with per-server settings, and a **single global `models` catalog** selected per server by id subset + `defaultModel`; camelCase keys, inline secrets (D8); A2A `baseURL` optional. |
| 2026-10-11 | Review fixes: §6 corrected (`cfg.Servers`, `BuildClient(catalog, server)`, per-listener TLS, `kind`); §8/§9 wording; §5.2 notes (host ignores port, HTTP root rule, `Cards(ctx)` needs `baseURL`); decided to **remove** `cmd/agentserve`/`cmd/a2aserve` with no compat (D9); open questions closed. |
| 2026-10-11 | Added §11 Implementation plan & quality gates: staged extraction (P0–P5), golden characterization + `-race` gates, and an 8-point review checklist; added a **self-contained code comments** convention (Go comments must not reference these docs). |
| 2026-10-11 | Config coverage completed against both commands' `config.go`: added a **fields-by-kind** table (shared / http-only / a2a-only) and the missing fields `allowInsecure`, `inboundMedia`, `tls{certFile,keyFile}`, `dropErroredTurns`, `cards`/`skills`, `ModelConfig.providerID`; expanded validation (bind/auth guard, tls, limits/durations, enums, aws, skills); decision D10 (unified `keepAlive` default `15s`). |
| 2026-10-11 | Review: §5.2 routing table split by kind (A2A has no per-message model); `host`+`mount` precedence and root-mounted-vs-`default` clarified; top-level `/.well-known/*` interception noted; §9 config tests extended; §11 checklist #1 now points to the fields-by-kind table (de-duplicated). |
| 2026-10-11 | Review: fixed the §6 sketch's Client-leak path (a single deferred close over every created Client; deterministic startup order); §5.2 lead wording; §9 assembly wording; moved the discovery `/.well-known` note out of the bullet list. |
| 2026-10-11 | Review: host routing matches `X-Forwarded-Host` first (proxy-correct, same rule as card URLs); aggregate discovery scoped to "the A2A entries on that listener" (per origin), not globally; noted `subscriberCapacity` default; status → **design settled, ready to implement**. |
| 2026-10-11 | Implemented P0–P5. Final package layout: the shared builder/config package is **`cmd/agentd/config`** (the old `cmd/serveconfig` was merged in and split by concern: `config.go`, `server.go`, `validate.go`, `routing.go`, `load.go`, `catalog.go`, `credentials.go`, `media.go`, `client.go`, `a2a.go`); command lifecycle (`serve`, `shutdownTimeout`) lives in `cmd/agentd/run.go`; tests split accordingly (validation → `cmd/agentd/config`, run/routing → `cmd/agentd`). Supersedes D4's "or `cmd/serverunner`" and the `cmd/serveconfig` references below. |
| 2026-10-11 | `models[].model` is now **required** and is the only value sent to the provider; `models[].id` is purely the catalog/session key and is **never** sent. Removed both fallbacks (`model`←`id` for the wire name, `id`←`model` for the catalog key); an unknown session id resolves to the default entry's metadata. |
| 2026-10-11 | A2A `cards` is now **mandatory per exposed agent**, requiring an explicit `name` and `description` (D11); `version` defaults to `1.0.0`, skills stay optional, and a card for a non-exposed agent is rejected. Enforced in `cmd/agentd/config` validation, not in `pkg/a2a/server`. |
| 2026-10-11 | Config values support explicit environment references: `${VAR}` / `${VAR:-default}` in any string value, `$$` for a literal `$`, undefined-and-no-default is an error (fail-fast). Enables secrets, `modelBaseURL`, `workingDirectory`, `cards.name`/`description` (and host/platform facts) to stay out of a committed file. Updates D8. |
| 2026-10-11 | Ported the upstream question policy (from the removed `a2aserve`/`agentserve`): per-server `questions: park \| decline` (`agent.WithInboundQuestions` / `server.Options.Questions`); default **`decline`** for a2a, **`park`** for http. `decline` rejects the `question` tool so a turn ends with the dismissal instead of parking at input-required. |

---

## 1. Motivation

`cmd/agentserve` and `cmd/a2aserve` are two binaries that do the same thing up to the
transport boundary: load a config, build an `agent.Client`, wrap it in a transport adapter,
serve it, and shut down gracefully. Today:

- The `buildClient` assembly is **duplicated** (`cmd/agentserve/run.go` /
  `cmd/a2aserve/run.go`), and the two commands' **config surfaces have already drifted** —
  `cmd/a2aserve` wires fewer `ClientOptions` fields than `cmd/agentserve`:

  | `ClientOptions` field (YAML key) | `cmd/agentserve` | `cmd/a2aserve` |
  |---|---|---|
  | MCP servers (`mcp`) | yes | **no** |
  | References (`references`) | yes | **no** |
  | Auto title (`TitleConfig` / `titleAuto`) | yes | **no** |
  | `SubagentDepth` **limit** (`subagentDepth`) | yes | **no** |
  | Media uploader | yes | yes |

  These are **`Client`-level options, not transport features**: an A2A session runs on the
  same `Client`, so it **can** use them — the gap is only that `cmd/a2aserve`'s **config**
  wires fewer of them today.

  What is **not** drifted: agents, modes, **subagents and skills all load from `configDirs`
  (files)** in *both* commands (`defsconfig.ApplyDirAgents` / `defsskill.DiscoverSkills`).
  And agent frontmatter has **no** `mcp`/`references` fields — MCP servers and references
  exist *only* as `ClientOptions`, which is exactly why the missing YAML keys in
  `cmd/a2aserve` are the whole gap. A single shared `buildClient` closes it: every server can
  set the same fields (values differ per server).

- Each binary re-implements CLI flags, config structs, and lifecycle wiring.
- Operators who want both protocols run two processes and two configs that must be kept
  in sync by hand.

Both adapters share the same core: they drive the same `agent.Client` / `Session`. The
split is at the **assembly layer**, not the engine — so it can be removed.

## 2. Goals and non-goals

**Goals**

- One binary (`agentd`), one config, one process, one signal handler, one graceful shutdown.
- Mount one or more transports (`http`, `a2a`) in the same process.
- A single shared `buildClient` — one builder, so every server's `ClientOptions` *can* carry
  the full field set (values still differ per server: models, root, …); fixes the §1 drift.
- Keep the transport **adapters** (`pkg/server`, `pkg/a2a/server`) and their wire
  contracts **unchanged**.

**Non-goals**

- Unifying the adapters or the wire protocols. They are different contracts
  (JSON session API + global SSE + `permission/{rid}/reply` vs
  `Task`/`Part`/`Artifact` + `AgentCard` + task store) and stay separate.
- Sharing one `Client` across servers (see §7).
- Any web UI, new endpoints, or schema-first codegen (consistent with `DESIGN-HTTP.md` §11).

## 3. Current state (facts)

- `cmd/agentd/config` holds the **transport-agnostic** pieces: YAML loading
  (`config.Load`), `PermissionRuleset`, and the catalog/credential/media-uploader helpers.
  Graceful serve (`serve(ctx, *http.Server, *TLS, beforeShutdown)`) lives in `cmd/agentd/run.go`
  (it owns `shutdownTimeout`), since it is command lifecycle, not configuration.
- HTTP path: `server.New(ctx, client, server.Options{…})` → `srv.Handler()`; the HTTP server
  calls `client.HoldInteractions(true)` in `New` (`pkg/server/server.go`).
- A2A path: `agent.NewA2AFactory(client, …)` also calls `client.HoldInteractions(true)`
  (`pkg/agent/a2a_factory.go`); wrapped by `pkg/a2a/server.New(...)` → `srv.Handler()`.
- `ClientOptions.Root` is **per-instance SDK state** (session DB, snapshots, references,
  tool output, logs; `pkg/agent/client.go`). Snapshots live under
  `{Root}/data/snapshot/<projectID>/<hash(worktree)>` (`pkg/core/snapshot.go`).
- `HoldInteractions` is a **global flag on a `Client`**, so a single `Client` must not be
  shared between the HTTP and A2A front-ends (`DESIGN-HTTP.md` §9, "Shared mode").

## 4. Target architecture

One process hosting **N `servers[]` entries** (any number, any mix, including several of
the same kind). **Each entry gets its own `Client`**, therefore its own `Root` (its own
session DB / snapshots / working directory).

```
agentd (one binary, one config)
├── process services: config load, logging, signals, graceful shutdown
└── one handler per `servers[i]` entry (each its own Client):
      http → pkg/server handler        }
      http → pkg/server handler        }  mounted on a listener — one http.Server per
      a2a  → pkg/a2a/server handler    }  distinct `listen`; several entries may share
                                          one listener via host/mount routing (§5.2)
```

Rationale for one `Client` per entry: `HoldInteractions` is global and each server is an
**interactive front-end** over the same held permission/question state; sharing a `Client`
makes "which front-end answers this ask" ambiguous and couples the lifecycles.

## 5. Configuration

A single config: a top-level **global `models` catalog** (each model's full definition,
once) plus a `servers` list. Each server chooses the model **ids** it exposes and its
default; everything else about a server is self-contained. No per-server duplication of
model config.

```yaml
# agentd.yaml
models:                              # GLOBAL catalog: id -> ModelConfig (defined once)
  - id: deepseek-chat
    model: deepseek-chat                    # wire model name (required)
    protocol: deepseek-chat                   # OpenAI-compatible chat + DeepSeek's `file` block
    modelBaseURL: https://api.deepseek.com   # optional; the route default when omitted
    credentials: { apiKey: ${DEEPSEEK_API_KEY} }   # provider secret (or aws: { ... }); ${VAR} supported
    inputModalities: [text, image]           # → /api/model "input"
    media:                                   # per-modality accepted forms → /api/model "forms"
      forms: { image: [inline, url, file] }  # DeepSeek: image only; audio/video/file omitted = rejected
    contextLimit: 128000                     # token window (compaction / usage)
    outputLimit: 8192
  - id: gpt-4o
    model: gpt-4o
    protocol: openai-responses
    credentials: { apiKey: sk-... }
    inputModalities: [text, image]
    contextLimit: 128000

servers:
  - name: http
    kind: http
    listen: 127.0.0.1:8787
    root: /var/lib/agentd/http          # REQUIRED, distinct per server
    workingDirectory: /srv/project
    configDirs: [ ./agents ]
    models: [deepseek-chat, gpt-4o]     # subset of the global catalog (ids only)
    defaultModel: deepseek-chat         # used when an agent/subagent sets no model
    agents: [ build ]                   # reachable allow-list
    mediaUpload: { ... }                # optional
    dropErroredTurns: true              # default true
    auth: { bearer: sk-... }            # or { username, password }; secret in the file
    allowInsecure: false                # opt out of the non-loopback-without-auth guard
    inboundPermissions: [ ... ]
    interactionTimeout: 5m
    interactionFallback: reject
    keepAlive: 15s
    # tls: { certFile: /etc/agentd/tls.crt, keyFile: /etc/agentd/tls.key }   # listener-level

  - name: a2a
    kind: a2a
    listen: 127.0.0.1:8788
    root: /var/lib/agentd/a2a           # REQUIRED, distinct per server
    workingDirectory: /srv/project
    configDirs: [ ./agents ]
    models: [deepseek-chat]             # this server exposes only this model
    defaultModel: deepseek-chat
    baseURL: https://agents.example.com
    agents: [ build ]                   # exposed agents
    inboundMedia: spool                 # spool | inline (fallback; see `media:`)
    media:                              # inbound media delivery policy (operator-owned, {M16})
      inlineLimitBytes: 262144          # or per-modality: { default: 262144, video: 2097152 }
      prefer: [file, inline]            # order of wire forms: inline | url | file
      upload: true                      # allow hosting: url via `storage`, file via the provider's `mediaUpload`
      downsample: true                  # images in-SDK; audio/video via ffmpeg (on PATH; soft-fail)
      fetchURL: true
      maxBytes: 52428800                # or per-modality: { default: ..., image: ..., video: ... }
    maxBodyBytes: 134217728             # HTTP request-body cap (0 = 128 MiB default; 413 above)
    # Hosting configured separately ({M17}/{M18}); `storage` serves BOTH the model edge (url) and the A2A file exchange (ingest):
    # storage:     { backend: http|local|s3|cos, url: https://store/x/, auth: none|basic|bearer, username?, password?, token?, header?, value?, presignSecret?|presignURL?, ... }
    #              # `http` credential (auth basic/bearer, or a custom header/value) validates BOTH writes and reads; signing is `presignSecret` (nginx secure_link; signs PUT slots and the Put read url) or `presignURL` (external signer for PUT slots), mutually exclusive
    #              # `s3` is S3-compatible (AWS S3 / MinIO, and COS via its S3 endpoint); the native `cos` backend
    #              # drives Tencent COS over its own REST API: { cos: { region, bucket, secretID, secretKey, sessionToken? } }
    #              # omit the s3/cos credentials (or set http auth none) for an anonymous/public origin or bucket
    # mediaUpload: { url: https://api.deepseek.com/files, fields: {purpose: user_data}, idField: id, expiresAfter: 86400 }  # provider Files API → file_id; expiresAfter → provider expires_after
    cards: { build: { name: ..., description: ... } }   # REQUIRED per exposed agent (name + description)
    auth: { bearer: sk-... }            # A2A auth is bearer-only
    dropErroredTurns: true
    allowInsecure: false
    # tls: { certFile: ..., keyFile: ... }   # listener-level (shared by entries on the same `listen`)
```

**TLS** is a property of the **listener** (one `http.Server` per `listen`): a `tls:`
declared on an entry applies to its listener, and entries that share one `listen` must use
the same `tls` (validated).

**Models.** A single **global `models` catalog** (top-level) defines each model once as a
full `ModelConfig`; a server only selects **ids**:

- `models[].id` — the catalog id / agent-facing name; **never sent to the provider**.
- `models[].model` (**required**) — the wire model name sent to the provider; it never falls
  back to `id`.
- `models[].providerID` (optional) — keys the provider credential (defaults to `protocol`);
  when several models share a provider, the first non-empty credential wins.
- `models[].protocol` — the wire route (e.g. `openai-chat`).
- `models[].modelBaseURL` — the model API endpoint; omitted ⇒ the protocol route's default.
- `models[].credentials.apiKey` (or `.aws`) — the provider secret.
- **capabilities** — `models[].inputModalities` (`text` / `image` / `audio` / `video` /
  `pdf`) or `models[].media.input`; `models[].media.forms` (per modality: `inline` / `url` /
  `file`); `models[].contextLimit` / `models[].outputLimit` (tokens). Surface as `GET /api/model` →
  `{ id, protocol, input, forms }`.
- **per server**: `models: [id, …]` selects the subset this server exposes (omitted ⇒ the
  whole catalog); `defaultModel: id` picks the default (omitted ⇒ the first of the subset).
  Both are ids only — no model config is repeated. An agent or subagent that names a model
  (markdown `model:` frontmatter) must use an id **in that server's subset**.
- Secrets live in the config file (keep it mode-0600), **or** are referenced from it
  explicitly: any string value supports `${VAR}` (required) and `${VAR:-default}`, with `$$`
  for a literal `$`. Environment is never read implicitly — only where the file asks — so the
  command stays fully config-driven (`DESIGN-HTTP.md` §8.1).

**Fields by kind.** Some fields apply to one kind only:

| Field | Applies to | Notes |
|---|---|---|
| `name` / `kind` / `listen` / `root` / `workingDirectory` / `configDirs` / `models` / `defaultModel` / `host` / `mount` | both | bind + routing + models |
| `mediaUpload` / `inboundPermissions` / `keepAlive` / `dropErroredTurns` / `tls` / `allowInsecure` | both | `dropErroredTurns` defaults **true** |
| `media` + `storage` + `mediaUpload` + `maxBodyBytes` | both | inbound media delivery + file exchange (`DESIGN-MEDIA` {M16}/{M17}/{M18}); `prefer` ∈ `inline`/`url`/`file`, where `url` is produced by the **provider-independent `storage`** store (`http` PUT base / `local` shared dir / `s3` / native `cos` object store → url; also the A2A ingest store) and `file` by the **provider-bound `mediaUpload`** (Files API → file_id). `media` keys: `inlineLimitBytes` / `prefer` / `upload` / `downsample` / `spool` / `fetchURL` / `maxBytes` (the two size keys are per-modality: a number or `{default, image, audio, video, file}`); `maxBodyBytes` caps an HTTP request body; `storage` keys: `backend` (`http`/`local`/`s3`/`cos`), `auth` (`none`/`basic`/`bearer`, write **and** read) / `username` / `password` / `token` / `header`+`value` (custom), `presignSecret` (nginx secure_link) **or** `presignURL` (an external signer) + `presignTTL`, or `s3`/`cos` credentials (omit → anonymous/public bucket); `storage` also advertises the AgentCard storage extension and serves the A2A ingest. Unset = inline-first (unchanged) |
| `questions` | both | the `question` tool policy: `park` or `decline`; default **`decline`** for a2a (an A2A client polls task state and cannot answer), **`park`** for http |
| `auth` | both | http allows `bearer` **or** `username`/`password` (`username` defaults `opencode`); **a2a is bearer-only** |
| `agents` | both | http = reachable allow-list; a2a = exposed agents |
| `interactionTimeout` / `interactionFallback` / `subscriberCapacity` / `cors` / `publicRoutes` | **http only** | hold-mode HITL + SSE/browser tuning |
| `baseURL` / `cards` / `inboundMedia` / `default` | **a2a only** | card advertising + inbound media (`spool` / `inline`) + origin-root card owner; `cards` is **required per exposed agent** |

MCP servers and references are `Client`-level per server (both kinds, after the merge).
`subscriberCapacity: 0` (the default) uses the HTTP adapter's built-in SSE bound.
`cards` is **required** for A2A: every exposed agent (`agents[]`) must have a card carrying an
operator-authored `name` and `description` (the card is the discovery contract a client
fetches first, so it is explicit rather than derived from the agent's internal definition);
`version` defaults to `1.0.0`, and `cards.<agent>.skills` entries are optional
**operator-authored descriptive labels** that must carry `id` + `name` + `description` (an
agent with none advertises `[]`). A card for an agent not in `agents[]` is rejected.

`servers` is a **list**: any number of entries, in any mix, **including several of the
same kind**. Each entry is independent — its own `Client` (own `root`), its own listener,
its own auth / agents / working dir:

```yaml
servers:
  - name: http-a            # an HTTP listener for project A
    kind: http
    listen: 127.0.0.1:8787
    root: /var/lib/agentd/a
    workingDirectory: /srv/a
  - name: http-b            # a second HTTP listener for project B
    kind: http
    listen: 127.0.0.1:8788
    root: /var/lib/agentd/b
    workingDirectory: /srv/b
  - name: a2a               # an A2A listener (one entry; can expose several agents)
    kind: a2a
    listen: 127.0.0.1:9099
    root: /var/lib/agentd/a2a
    workingDirectory: /srv/a
    agents: [ build ]
```

So "HTTP / A2A / both" is just the shape of the list — one entry, two entries, three, or a
dozen. The only rules are distinct `name` and `listen`, and a distinct `root` **per entry**
(§7) — nothing is limited to one-per-kind.

Validation (fail-fast, reusing today's `validate()` rules per server):

- `servers` non-empty; each `name` unique.
- `listen` **may be shared**: entries with the same `listen` are multiplexed under one
  `http.Server` and must be distinguishable by `host` / `mount` (§5.2); otherwise each
  `listen` should be distinct.
- each `root` present and **unique** across entries.
- duplicate **kinds** are allowed (several `http`, several `a2a`) — only `name` / `listen` /
  `root` must be distinct.
- at most one A2A entry may be `default`; at most one entry per `listen` has neither `host`
  nor `mount` (it owns that listener's root).
- **models**: the global catalog is non-empty with unique ids; each server's `models` ids
  exist in the catalog; its `defaultModel` (if set) is within that server's subset; every
  `models[]` entry passes `ModelConfig` validation.
- **auth / bind** (per server): a non-loopback `listen` requires `auth` unless
  `allowInsecure: true`; `auth` must set `bearer` (both kinds) or `password` (**http only**,
  `username` defaults `opencode`) — an A2A entry must use `bearer`.
- **tls**: when set, both `certFile` and `keyFile` are required; entries sharing a `listen`
  must use the same `tls`.
- **numbers / enums**: `contextLimit` / `outputLimit` / `subagentDepth` / `subscriberCapacity`
  ≥ 0; `keepAlive` / `interactionTimeout` parse (`interactionTimeout` ≥ 0);
  `inboundMedia` ∈ {`spool`, `inline`}; `inputModalities` ∈ {`text`, `image`, `audio`,
  `video`, `pdf`}; `media.forms.<modality>` ⊆ {`inline`, `url`, `file`} for modality ∈
  {`image`, `audio`, `video`, `file`}; `inboundPermissions` effects ∈
  {`allow`, `ask`, `deny`}; `cards` skills carry `id` + `name` + `description`; **aws**
  credentials require `region` + `accessKeyId` + `secretAccessKey`.

### 5.1 CLI flags (streamlined)

Principle: **`agentd` is config-driven.** The config file is the single source of truth —
the `servers[]` list (which decides which listeners run) and all policy/tuning. The CLI
only points at it.

```
-config <path>   YAML config file (default: ./agentd.yaml)
-check           validate the config and exit (no listeners)
-version         print version and exit
```

Everything else comes from the config:

- **Which transports run** = the `servers[]` entries (any number, any mix, incl. several
  `http` and/or several `a2a`): `http` ⇒ HTTP/SSE listener, `a2a` ⇒ A2A listener. No CLI
  selector.
- **Models** (global catalog + per-server subset / `defaultModel`), **root / workingDirectory
  / listen / TLS / auth** = config.
- **Reachable / exposed agents, MCP servers, references, auto-title, subagent depth, HITL
  timeouts, SSE tuning, CORS, public routes** = config.

Rationale:

- The two commands' ~18/13 flags collapse to essentially `-config`; nothing about agentd's
  behavior is chosen on the command line.
- Operationally this matches the daemon model (`nginx -c`, `--config.file`): one file per
  deployment, no flag/config drift, and `-check` for CI/validation.
- It removes the current "flags overlay the config" puzzle (which flag wins, per-transport
  ambiguity) — the config is authoritative and complete.

No override flags: the CLI is `-config` (+ `-check` / `-version`) only — nothing about
agentd's behavior is chosen on the command line.

### 5.2 Routing (in-process multiplexing)

**Two routing levels.** Project selection is listener-level; agent/model selection is above
it (request- or agent-config level, per kind):

| Dimension | Granularity | Mechanism |
|---|---|---|
| project / working dir / `Root` (the isolation unit) | **listener (server) level** | one `servers[i]` = one `Client` |
| agent | request / endpoint level | **http**: `POST /api/session {agent}`, within the server's `agents` allow-list; **a2a**: which card/endpoint you call (the server's `agents` expose-list) |
| model | request / agent-config level | **http**: `POST /api/session {model}`; **a2a**: the agent's configured model (frontmatter) or the server's `defaultModel` — there is **no per-message model** in A2A |

The HTTP API has **no per-session location routing** (`DESIGN-HTTP.md` §5.5 drops
`location`; `session.next.moved` has no producer), so a project can only be chosen at the
listener — multi-project is always multi-entry.

**One `ip:port` serving several entries.** Both adapters return a plain `http.Handler`
(`pkg/server` and `pkg/a2a/server`), so one `http.Server` (one TCP/TLS entry) can host many
instances via a top-level router:

```
http.Server{ Handler: root }               # one per distinct `listen`
  root
    ├─ mount /a            → http.StripPrefix("/a", httpHandlerA)   (pkg/server)
    ├─ mount /b            → http.StripPrefix("/b", a2aB)           (pkg/a2a/server)
    └─ host c.example.com  → a2aC
```

Optional per-entry fields:

| Field | Effect |
|---|---|
| `host` | virtual host: match the request host (`X-Forwarded-Host` if present, else `r.Host`; hostname, port ignored) → this server (otherwise fall back to path mount) |
| `mount` | path prefix: mount + `StripPrefix`; for A2A it is also the source of the `baseURL` prefix |
| `default` | **A2A only**: owns the origin-root `/.well-known/agent-card.json` — the nginx `default_server` idea. A root-mounted A2A entry (neither `host` nor `mount`) is naturally the default; otherwise it defaults to the first `a2a` entry. |
| `baseURL` | **optional** — the A2A card's advertised endpoint base. Unset ⇒ derived from the request origin (`scheme://host`, honoring `X-Forwarded-*`), so **host routing needs none**. Set it for **path routing** (the origin fallback carries no path) or when the public URL differs from the request host. |

Resolution order: a matching `host` wins (an entry may set **both** `host` and `mount` — when
`host` misses, its `mount` still applies); otherwise the **longest matching `mount`** prefix;
an entry with **neither** serves the listener **root** (so at most one such entry per
`listen` — the common single-server case). With **no `host`**, servers fall back to **port
routing** (distinct `listen` addresses → one listener each) or, when sharing a `listen`, to
**path routing** via distinct `mount` prefixes.

**Host vs path.**

- **Host (virtual host)** — for A2A this is the *zero-config, spec-clean* choice: the server
  builds each card's endpoint URL from the **request origin** (`cardEndpoint` →
  `requestBaseURL`, honoring `X-Forwarded-*`), so different hosts get correct endpoints with
  no extra config; it also pairs naturally with TLS/SNI and multiple certificates. **Host
  matching uses the same origin rule** (`X-Forwarded-Host` first, then `r.Host`), so it stays
  correct behind a proxy.
- **Path** — works for both, with two A2A caveats:
  - The card *does* carry the access path: `SupportedInterfaces[].url` is the absolute
    JSON-RPC endpoint and may include the prefix. But it is built from `BaseURL` — the
    origin-based fallback `requestBaseURL` is **scheme+host only** (no path). So **every
    path-mounted A2A entry must set `baseURL` including its prefix**, or the advertised
    endpoint is wrong.
  - The origin-rooted discovery `/.well-known/agent-card.json` can belong to only **one**
    entry (the `default`); the others are reached via their prefixed card URL.

> Only A2A has a self-describing card that embeds an **absolute endpoint URL**; the HTTP
> API has no such document (clients already know the host:port they call), so there is no
> HTTP analogue of `baseURL` — it is purely an A2A advertising concern.

**Discovery.**

- `/.well-known/agent-card.json` (origin root) → the **default** A2A entry's single card (per
  listener / origin).
- `/.well-known/agents.json` (origin root) → **aggregate across the A2A entries on that
  listener (origin)**: each `pkg/a2a/server` already serves its own `DiscoveryPath`
  (`/.well-known/agents.json`, a JSON array of its exposed agents' cards); `agentd`'s top-level
  router additionally serves the **union** at that listener's origin root. This needs a small
  SDK addition: an exported accessor on
  `pkg/a2a/server.Server`, e.g. `Cards(r *http.Request) []*a2a.AgentCard` (cards built by
  the instance itself, so `BaseURL`/`Security` stay authoritative; the request supplies the
  origin when `BaseURL` is unset), which the root handler concatenates.
- Each entry keeps its own `/.well-known/agent-card.json` and `/.well-known/agents.json`
  under its `mount`/`host`.

> The top-level router intercepts `/.well-known/agent-card.json` and `/.well-known/agents.json`
> **before** any root-mounted entry's mux, so a server that owns the listener root cannot
> shadow them.

**Invariants (unchanged).** Multiplexing shares only the TCP/TLS entry point. Each entry
still has its **own `Client`** (own `Root`/isolation), its own auth, and its own
`HoldInteractions`; sessions/snapshots are never merged.

**Out of scope here.** The same goal via an **external reverse proxy**
(Caddy/nginx/Traefik) doing host/path routing in front of per-entry listeners — A2A already
emits proxy-correct card URLs — is a deployment choice, not an `agentd` feature.

### 5.3 Admin read-only endpoints (`/api/config`, `/api/info`)

An operator (e.g. an IM bridge's admin) can read agentd's **effective** configuration and
runtime info without the config file — useful across machines.

| Method + path | Body |
|---|---|
| `GET /api/config` | the loaded config, structurally identical to the YAML, secrets redacted, with provenance |
| `GET /api/info` | `{version, pid, listen, servers[], a2a[]}` for the listener it is served on |

- **Served by agentd's router** (not `pkg/server`), on each listener's **origin-root** server,
  using that server's credential. It is **not** public by default — only an explicit
  `publicRoutes` entry makes it anonymous. A dynamic control-API instance's router carries no
  admin doc, so it never serves these.
- **Scope: global** (the whole process config), **not** per-server. In a multi-tenant
  deployment (servers with distinct credentials) any authenticated client can read the whole
  config; the operator should keep agentd single-tenant or expose it on a dedicated server.
  This is documented, not enforced.
- **`/api/config` query**: `?section=models|servers|control|wrap` (omit for all),
  `&server=<name>` (filter `servers[]`). An unknown `section` → `400`.
- **Redaction (fail-closed whitelist)**: the response is a dedicated DTO; unlisted fields are
  omitted. Secrets are **never** returned — `models[].credentials.apiKey`/`aws.*`,
  `servers[].auth.bearer|password`, `servers[].tls`, `servers[].mcp[]` internals,
  `servers[].mediaUpload.header|value|fields`,
  `servers[].storage.password|token|value|presignSecret`, `servers[].storage.s3.accessKey|secretKey`,
  `servers[].storage.cos.secretID|secretKey|sessionToken`, `control.auth.*` become
  `{"configured": bool, "redacted": true}` (or `{"configured": false}`). Expanded `${VAR}`
  values are treated as secret; only the variable **names** are reported. Credential-bearing
  URLs (`references[].repository`, `mediaUpload.url`, `storage.url|presignURL|serveBaseURL`)
  have their userinfo stripped, so an `https://user:token@host/...` cannot leak its secret.
- **Provenance**: `sources[]` reports the config `file` path and the `env` variable names
  expanded while loading (`config.LoadWithEnv`).
- **Errors**: `401` (missing/invalid credential), `400` (bad `section`), `405` (non-GET),
  `404` (no origin-root server), matching the `/api/*` envelope `{error, message}`.
- **Stability**: `schemaVersion` (currently `1`); the endpoints are experimental — fields are
  only added, a removal bumps the version. Read-only (no PATCH) and no hot reload: the config
  is loaded at startup (`--check` validates it); restart to change it.

## 6. Build and lifecycle

Proposed shape (illustrative):

```go
// cmd/agentd
func run(ctx context.Context, cfg config) error {
    var clients []*agent.Client          // every Client ever created
    var targets []serveTarget            // {srv *http.Server, tls *TLS}
    defer func() {                       // closure sees the final slice
        for _, c := range clients { _ = c.Close() }
    }()
    for _, listen := range sortedListens(cfg.Servers) { // deterministic startup order
        root := newRouter()              // host switch / path mux
        for _, s := range cfg.Servers[listen] {
            client, err := config.BuildClient(cfg.Models, s) // one Client per server
            if err != nil { return err } // defer closes any Client created so far
            clients = append(clients, client)
            h, err := buildHandler(ctx, client, s)   // http | a2a (on s.Kind)
            if err != nil { return err }
            mount(root, s, h)            // host / mount; wire root discovery to the default
        }
        targets = append(targets, serveTarget{
            srv: &http.Server{Addr: listen, Handler: root}, tls: cfg.Servers[listen][0].TLS})
    }
    return serveAll(ctx, targets)        // each target carries its own listener TLS
}
```

- **Cleanup is a single deferred close over `clients`.** Because the deferred call is a
  closure over the final slice, a mid-loop `BuildClient`/`buildHandler` failure closes every
  Client created so far. (A plain `defer closeAllClients(targets)` would miss the
  in-progress listener's Clients *and* capture a stale slice.)
- **Entries are grouped by `listen`**: one `http.Server` per distinct address; within a
  listener, `host` / `mount` route to each entry's handler (§5.2), and the origin-root
  discovery (`/.well-known/agent-card.json` = `default`; `/.well-known/agents.json` =
  aggregate) is wired at the top. **TLS is per listener** (§5), carried on the `serveTarget`.
- **Deterministic startup**: iterate listens/entries in a sorted order so logs and tests are
  stable.
- `config.BuildClient` is the **extracted** `buildClient`, now shared by both
  transports (and fixing the §1 drift). It takes the **global `models` catalog plus the
  server entry** (which selects its model subset).
- `buildHandler` switches on the entry's `kind`: `pkg/server.New(...)` or
  `pkg/a2a/server.New(...)`, returning the handler plus a `closeStreams` callback (HTTP
  closes SSE via `srv.Close()`).
- `serveAll` runs one `serve` (in `cmd/agentd/run.go`) per listener under a single `ctx` and
  a single signal handler; on shutdown each listener's `beforeShutdown` runs, then
  `Shutdown` with the command's `shutdownTimeout`.
- A listener bind failure aborts startup and tears down the already-started listeners.

## 7. Constraints (hard)

1. **One `Root` per server.** Session DB / snapshots / context do **not** cross servers
   (`Root` cannot be shared; sqlite and snapshots would collide). A session created on one
   server is invisible to another — this is correct (different front-ends) and must be
   documented.
2. **Sharing "the same project" across servers = one isolated view per server.** Give each
   server its own working copy derived from the same base (`worktree` / `overlay` / `copy`;
   see `DESIGN-IM-BRIDGE.md` §2.9(4)/§3.7 in the Tether repository).
   - **Allowed**: one **read-only** project directory shared by several servers (multiple
     readers are safe).
   - **Forbidden**: sharing one **writable** directory with no copies — concurrent writes
     clobber each other, and each server's own `Root` snapshots `revert` would overwrite
     the other's edits. Copies diverge by design, which is why they can coexist.
3. **Never mount one `Client` on two listeners** — `HoldInteractions` is global and
   interaction ownership would be ambiguous (`DESIGN-HTTP.md` §9).

## 8. Migration

`cmd/agentserve` and `cmd/a2aserve` are **removed** in favor of `agentd`. There is **no
old-config compatibility** to preserve — old flat configs are **not** auto-mapped, and no
compatibility shims are kept.

- A single-server config is the degenerate case (`servers` has one entry), so an existing
  deployment expresses itself directly: the old flat keys become one top-level `models`
  entry plus `servers[0]`.

## 9. Testing

- **Config**: duplicate server `name`, duplicate `listen`, duplicate/missing `root`, unknown
  model ids, `defaultModel` outside the server's subset, a second `default`, `tls` mismatch on
  a shared `listen`, a non-loopback `listen` without `auth` (and `allowInsecure` overriding
  it), a bad `inboundMedia`, an exposed a2a agent without a card (or with a card missing
  `name`/`description`), a card for a non-exposed agent, and `cards` skills missing
  `id`/`name`/`description`.
- **Assembly**: `BuildClient(catalog, server)` produces the expected `ClientOptions` (incl.
  MCP / References / Title / SubagentDepth) for **both** `kind`s — guards against the §1 drift
  returning.
- **Routing**: two servers on one `listen` (host and/or mount) each answer on their own
  selector; the origin-root `/.well-known/agent-card.json` is the `default`, and
  `/.well-known/agents.json` unions all A2A servers.
- **Integration**: start `agentd` with `http` + `a2a`; assert both listeners answer and that
  each `Client` is isolated (separate session DBs, sessions do not cross). Assert graceful
  shutdown closes all listeners and clients; assert bind-failure aborts startup.
- **Regression**: a one-entry `agentd` config (`servers` has one entry) behaves like the old
  single command on the wire.

## 10. Decisions and open questions

| # | Decision | Rationale |
|---|---|---|
| D1 | Name: **`agentd`**; one binary, one config, **N `servers[]` entries on one-or-more listeners**. | Removes the duplicated command layer; "daemon hosting the SDK". |
| D2 | **One `Client` per `servers[i]` entry**; never share a `Client`. | `HoldInteractions` is global; interaction ownership must be unambiguous. |
| D3 | Keep the transport **adapters** (`pkg/server`, `pkg/a2a/server`) unchanged. | Different wire contracts; only the assembly layer is unified. |
| D4 | Extract `buildClient` into `cmd/agentd/config` (or `cmd/serverunner`). | Fixes the current drift; single source of truth. |
| D5 | **Streamlined, config-driven CLI** (§5.1): essentially only `-config` (plus `-check`/`-version`); the config is the single source of truth — servers, agents, models, roots, listeners, auth and tuning all live there. No per-run behavior flags. | The two commands' ~18/13 flags overlap and sprawl; everything agentd does is a config decision, so the CLI only points at the file. |
| D6 | **In-process one-port multiplexing** (§5.2) via a top-level router: `host` (virtual host) is the preferred A2A discriminator (zero-config, origin-built card URLs); `mount` (path prefix) is also supported and **requires** a per-entry `baseURL` including the prefix. | Both adapters return `http.Handler`, so sharing a listener is natural; host routing keeps A2A card URLs correct with no extra config. |
| D7 | **Cross-instance aggregate discovery** at the origin root `/.well-known/agents.json` (union of the A2A entries **on that listener**), served by `agentd` via a new exported `pkg/a2a/server.Server.Cards(r *http.Request)` accessor; the origin-root single card `/.well-known/agent-card.json` is owned by the `default` A2A entry. | Per-instance `/.well-known/agents.json` already exists; the root union lets one URL discover every A2A agent on that origin, and the `default` entry resolves the single-card root. |
| D8 | Config keys are **camelCase** (matching `cmd/agentd/config`). Secrets are inline **or** referenced via `${VAR}`/`${VAR:-default}` (any string value; `$$` escapes a literal `$`); environment is never read implicitly. | Consistency with the existing `agentserve`/`a2aserve` config and `DESIGN-HTTP.md` §8.1; explicit references keep secrets and host-specific facts (paths, endpoints, platform) out of a committed file without giving up the config-driven design. |
| D9 | **Remove `cmd/agentserve` and `cmd/a2aserve`**; `agentd` replaces them, with **no old-config compatibility** (no shims). | The two commands are the duplication being removed; keeping a shim would preserve the very drift and the flag sprawl. |
| D10 | Unify `keepAlive` default to **`15s`** for every server (the HTTP command's default); disable heartbeats explicitly with `keepAlive: 0s`. | The two commands disagreed (HTTP defaulted 15s, A2A disabled when unset); a single default is simpler. |
| D11 | A2A `cards` is **mandatory per exposed agent**, with an explicit `name` and `description` (validated); `version` defaults to `1.0.0`, skills stay optional. | The AgentCard is the discovery contract a client fetches first; deriving name/description from the agent's internal definition yields a cryptic name and leaks internal behavior, so the operator must author it. Enforced at the `agentd` config layer, leaving `pkg/a2a/server`'s defaults for programmatic callers. |

Open questions: **none** — the design is settled and implemented.

## 11. Implementation plan and quality gates

**Staged extraction** (each step a reviewable, green commit):

- **P0** — Extract `BuildClient(catalog, server)` into `cmd/agentd/config` and switch the existing
  commands to it (behavior-preserving; fixes the §1 drift first).
- **P1** — Add `Cards(ctx)` to `pkg/a2a/server.Server` (+ tests).
- **P2** — `cmd/agentd` skeleton: config load/validate, one server, reuse `cmd/agentd/config`.
- **P3** — Multi-server / multi-listener: `groupByListen`, per-listener TLS, `serveAll`,
  graceful shutdown and startup-failure rollback.
- **P4** — Multiplexing (`host`/`mount`/`default`) + origin-root aggregate discovery.
- **P5** — Remove `cmd/agentserve` / `cmd/a2aserve` (D9) + migration examples.

**Correctness.**

- **Characterize first (golden)**: capture the current commands' wire behavior —
  `/api/health`, `/api/agent`, `/api/model`, `/api/tool`, error JSON shapes,
  `/.well-known/agent-card.json`, `/.well-known/agents.json`, and one JSON-RPC round-trip. A
  one-server `agentd` must reproduce them byte-for-byte.
- **Gates**: `go build ./...`, `go vet ./...`, `gofmt -l`, `go test ./...` (with `-race` for
  the multi-listener lifecycle).
- **Manual**: SIGTERM closes all listeners and clients; a second listener that fails to bind
  rolls back the first; missing TLS/credential inputs fail fast.

**Review checklist** (each row is a review question):

| # | Principle | Check |
|---|---|---|
| 1 | Completeness | The per-server field set matches the §5 **fields-by-kind** table (shared / http-only / a2a-only) and is the **union** of both old commands — nothing dropped (incl. `allowInsecure`, `dropErroredTurns`, `mediaUpload`, `configDirs`, `baseURL`). |
| 2 | Error branches | Every §5 validation rule fails fast; a bind failure rolls back started listeners and closes their clients; TLS/credential load errors surface; `client.Close` on all paths; empty `models`, duplicate `default`, unknown model ids. |
| 3 | Duplication | No duplicated `buildClient`; one serve/shutdown/rollback/CLI path; nothing copy-pasted from the two old commands. |
| 4 | Naming | `BuildClient(catalog, server)`, `buildHandler`, `newRouter`, `groupByListen` / `sortedListens`, `serveTarget`, `mountServer` — consistent with the repo. |
| 5 | Comments | Comments match the code and are **self-contained** — no `§N`/cross-references to design docs inside Go code. |
| 6 | Refactoring discipline | Behavior-preserving, small steps, adapters untouched (D3), no unrelated edits, tests green each step. |
| 7 | Readability | `run` / `serveAll` / mount / routing split into small functions; errors carry context (which server/listen). |
| 8 | Cyclomatic complexity | Split `run`, `buildHandler` and routing into `buildTargets` / `mountServer` / `resolveByHost` / `resolveByMount` / `closeAll`; keep functions small even where the problem is inherently complex. |
