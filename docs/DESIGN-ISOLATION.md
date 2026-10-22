# Sandbox and project isolation — implementation design (SDK `pkg/` + command `cmd/`)

> Status: **implemented** — Part 1 (`ClientOptions.Process` seam) and Part 2
> (`cmd/agentd/isolation`, `cmd/agentd/sandbox`, `sandbox.failurePolicy`, the opt-in
> control API) have landed; the **`bwrap` (Linux)**, **`seatbelt` (macOS)** and
> **`appcontainer` (Windows, best-effort)** sandbox wrappers are shipped (§8), including
> **whole-process wrapping** (the top-level `wrap` block launches agentd inside the
> wrapper, §8.1).
> Still design-only: **container/remote provisioning** — those
> run as a separate host-provisioned process, so `agentd`'s isolation config rejects the
> `container`/`remote` modes rather than serving them locally (Part 2 §12, §15).
> Scope: the changes **go-agent-sdk** itself must make so a host (the Tether IM bridge, or any
> embedder) can run agents safely and isolate many conversations from one another. It covers
> two orthogonal layers:
> - **Part 1 — SDK (`pkg/`)**: an in-process command-execution seam (`Sandbox`).
> - **Part 2 — command (`cmd/`, agentd)**: a project/process isolation runner.
>
> Relationship: this document is the implementation companion to the responsibility
> boundary already recorded in `DESIGN-IM-BRIDGE.md` §17 (Tether repository) ("sandbox/isolation responsibility
> boundary and the two designs") and the multi-server model in `DESIGN-AGENTD.md` §7. It
> does **not** change `DESIGN.md` (the SDK's authoritative design); it specifies the concrete
> interfaces, wiring points and configuration for the two proposals.
>
> Convention: Go snippets are **illustrative** (they express type boundaries, not final
> signatures). Field names may change; the layering may not.

## Table of contents

- 1. Two layers, one red line
- 2. Terms
- Part 1 — SDK execution seam (`pkg/`)
  - 3. Problem
  - 4. Proposal: promote `core.AppProcess` into `ClientOptions`
  - 5. Wiring points
  - 6. Failure policy
  - 7. Optional: file-write guard
  - 8. Reference implementations (optional, build-tagged)
  - 9. Non-goals
- Part 2 — agentd project runner (`cmd/`)
  - 10. Problem
  - 11. Interfaces
  - 12. Isolation modes
  - 13. Configuration
  - 14. Integration into `run.go`
  - 15. Dynamic provisioning (supervisor vs control API)
  - 16. Lifecycle, ownership and quotas
- Part 3 — Host contract (how Tether consumes it)
- Part 4 — Cross-platform enforcement reference
- Part 5 — Non-goals and red lines
- Part 6 — Testing
- Part 7 — Milestones
- Part 8 — Decisions

---

## 1. Two layers, one red line

Isolation and sandboxing solve problems at different layers and must not be conflated:

| | Sandbox (Part 1) | Project isolation (Part 2) |
|---|---|---|
| Guards against | agent → host (writes outside the project, egress, credential theft, privilege escalation) | conversation A ↔ conversation B (clobbering each other's files, cross-tenant peeking, snapshot collisions, resource contention) |
| Scope | the boundary of **one agent process** | separation between **many conversations/tenants** |
| Governs | how a **single command** executes | **which directory/process** a session uses |
| Where | **SDK `pkg/`** (the seam) + host/deployment (the enforcement) | **command `cmd/`** (the mechanism) + host (the decision/lease) |

**Red line: do not make project isolation an SDK hook.** The SDK's job is to expose a
*command-execution seam* and to keep the "one `Client` = one project/`Root`" primitive that
isolation is built on. The command layer prepares isolated views and starts processes; the
host decides read-only-share vs writable-isolate and holds the leases.

## 2. Terms

| Term | Meaning |
|---|---|
| `Client` | One SDK instance: one `Root` (session DB/snapshots) and one `WorkingDirectory`. The unit of isolation. |
| `Root` | SDK-managed state directory (`{Root}/data`, `/cache`, `/state`, `/tmp`). Two servers must never share one. |
| `WorkingDirectory` | The project/worktree the agent reads and writes. |
| Sandbox | An OS-enforced restriction applied to an executed command (filesystem view, network, privileges, resources). |
| Isolated view | A per-conversation materialization of the project (worktree/overlay/copy/container) with its own `WorkingDirectory`. |
| Runner | The transport-agnostic command-layer component that prepares an isolated view and serves it. |
| Host | The process embedding/operating agentd (e.g. Tether) or its orchestrator/supervisor. |

## Part 1 — SDK execution seam (`pkg/`)

### 3. Problem

The built-in `bash` tool executes arbitrary shell **inside the host process**, with the
caller's identity and the host user's filesystem, process and network authority. The SDK's
`permission` policy (allow/ask/deny, `external_directory`) only decides **whether** a command
may run; once allowed it runs unrestricted. There is no supported way for an embedder to wrap
the command in `bwrap`/`seatbelt`/`AppContainer`.

The SDK already has the right internal seam:

- `pkg/core/process.go`: `core.AppProcess` with `Run` and `RunStream`, implemented by
  `core.NewAppProcess()`. It is already the single execution path used by `bash`
  (`pkg/core/tool/bash.go` `BashDeps.Process`), `ripgrep` (`pkg/core/ripgrep.go`
  `NewRipgrep(process, …)`) and `git` (`pkg/core/git.go` `NewGit(proc)`).
- `pkg/agent/builtins.go` creates it once: `process := core.NewAppProcess()`, then threads it
  into `BashDeps`/`NewRipgrep`.
- `pkg/agent/runtime.go` (`initRuntimeState`) creates a **second** one for git:
  `git := core.NewGit(core.NewAppProcess())`.

The gap is only that **`agent.ClientOptions` cannot override it**, so an embedder using
`pkg/agent` cannot substitute a sandboxed process without forking.

### 4. Proposal: promote `core.AppProcess` into `ClientOptions`

Add one optional field to `agent.ClientOptions` (`pkg/agent/client.go`):

```go
// Process is the execution seam used by every built-in that spawns a process
// (bash, ripgrep, git). Nil keeps the current behaviour (core.NewAppProcess(),
// i.e. pass-through). A host may supply a wrapper that runs the command inside
// an OS sandbox; the SDK neither provides nor requires one.
Process core.AppProcess
```

- Reuses the **existing** interface; no new abstraction is invented. `DESIGN-IM-BRIDGE.md`
  §17.2 (Tether repository) proposed the same thing under the name "`Sandbox` hook / `ClientOptions.Process`"; the
  `Process` field is that proposal, not a separate mechanism.
- `RunStream` is already part of `core.AppProcess`, so streaming `bash` output is covered too.
- Default (`nil`) preserves the status quo and embeddability (tests, CLI, in-process use).

### 5. Wiring points

Implementation must thread `opts.Process` to **both** creation sites so the seam is complete:

1. `pkg/agent/builtins.go`: source `process` from the injected value instead of
   `core.NewAppProcess()` (the local variable already flows into `BashDeps` and
   `NewRipgrep`); thread `opts.Process` into `registerBuiltins`. Because `bash`, `ripgrep` and
   `git` all receive the same value, **one injection applies everywhere**.
2. `pkg/agent/runtime.go` `initRuntimeState`: accept the process and use
   `core.NewGit(process)` instead of `core.NewGit(core.NewAppProcess())`.
3. Document the "same value reaches every tool" invariant with a test (see Part 6).

The failure to thread it to (2) would silently leave `git` unsandboxed while `bash` is
sandboxed — a subtle hole the test must catch.

### 6. Failure policy

A sandbox wrapper may be unavailable at runtime (e.g. `bwrap` missing). Make the policy
explicit and configurable, defaulting to **fail-closed**:

- `fail-closed` (**default**): refuse to start when a named sandbox cannot be built, so a
  command never runs silently unsandboxed. This is the safe default for any deployment that
  executes agent commands on behalf of **untrusted users** (e.g. an IM bridge).
- `fail-open`: log and fall back to pass-through (the status quo); appropriate only for trusted
  single-user desktop embedding.

The SDK only offers the seam; the wrapper decides, and no probing/capability detection belongs
in core (it would complicate embedding). The policy itself is applied by the **command layer**:
`agentd`'s `sandbox.failurePolicy` resolves `sandbox.process` to an `AppProcess`; a missing
wrapper fails startup (fail-closed) or is logged and skipped (fail-open).

### 7. Optional: file-write guard

`AppProcess` does not cover the file-write tools (`write`/`edit`/`apply_patch`), which write
directly. If a host wants the same project boundary for those, add a second optional seam:

```go
// FileWriteGuard, when set, is consulted before a built-in file mutation.
// Nil disables the guard (current behaviour).
type FileWriteGuard interface {
    Allow(ctx context.Context, path string, op FileOp) error
}
```

This is **secondary**; the OS sandbox that wraps the whole process (Part 2, container) already
covers file writes and is the recommended first line. **Implemented**: `ClientOptions.FileWriteGuard`
is consulted by the `write` / `edit` / `apply_patch` tools before any mutation — `core.FileWriteGuard`
`Allow(ctx, path, op)` where `op` is the tool (`write`/`edit`/`apply_patch`); a non-nil error aborts
the mutation (model-facing `Failure`). Nil disables the guard.

**Also implemented (S5)**: `ClientOptions.FileReadGuard` is consulted by the `read` tool
(`AllowRead(ctx, path)`), so the in-process read path shares the sandbox policy. `grep`/`glob`
run ripgrep through `AppProcess`, so the process sandbox covers them already. `agentd` builds one
`PathGuard` from the server's sandbox policy and installs both interfaces, so reads and writes
cannot drift apart.

### 8. Reference implementations (optional, build-tagged)

Ship **optional, best-effort** wrappers of `core.AppProcess` that `exec` through a sandbox,
each behind a build tag, with **no promise from core**:

- Linux: `bwrap` (`--ro-bind`, `--unshare-net`, `--tmpfs /home`, …), optionally `cgroup`.
- macOS: `seatbelt` (`sandbox-exec -f profile.sb`).
- Windows: `AppContainer` + restricted token/low IL + Job Object + WFP (no single CLI; assemble).

These live outside `pkg/agent` core (e.g. `sandbox/bwrap`, `sandbox/seatbelt`) so the SDK's
public surface stays small and cross-platform CVE/compat debt stays out of core.

Wrappers are named and registered (e.g. `sandbox.Register("bwrap", factory)`); the command
layer's `sandbox.process` config value selects one by name, so the config never imports a
platform-specific package directly. **Shipped (best-effort, S4)**: `bwrap` (Linux),
`sandbox-exec`/seatbelt (macOS) and `appcontainer` (Windows), each behind an OS build tag and
registered on import — so `sandbox.process: bwrap` / `seatbelt` / `appcontainer` works out of
the box on those platforms.

> **S5 policy** (see the `sandbox` block in §13): the wrappers enforce more than writes.
> **Reads** deny broad regions — the user's home (and its parent), `/etc` — and re-allow the
> project plus explicit paths, so A1/A2 and sibling projects are unreadable while the system
> run-time paths keep working (the Part 4 "deny-region + re-allow" model). **Writes** stay
> allow-only. **Network** egress is denied per command (hard no-egress). **Process introspection**
> is denied per command (`(deny process-info*)`/`(deny sysctl-read)` on seatbelt, `--unshare-pid`
> on bwrap, the low-integrity AppContainer on Windows). The child **environment** is scrubbed of
> `*_KEY` / `*_SECRET` / `*_TOKEN` / `*_PASSWORD`. The whole-process profile keeps the network
> and process introspection (it serves listeners, calls the model and manages children) and
> re-allows its own config dirs (FR12). `sandbox.failurePolicy` governs *resolution* at startup
> only; a wrapper that resolves but fails at exec surfaces as a per-command tool error.
> Each command runs with a private temp under `{Root}/data/sandbox-tmp`, so the shared host
> temp (and any socket another process left there) is not exposed; the child's `TMPDIR`/`TEMP`
> point at it.
> Denials are reported as `session.next.sandbox.denied` events: `enforced` from the in-process
> guard (authoritative) and from Linux precise mode (strace), and `suspected` from a command whose
> stderr matched a platform denial message (best-effort). `violations.reportToModel` appends a
> short denial note to the command output.

> The Windows `appcontainer` wrapper creates an AppContainer profile, grants it the run directory
> and the policy's allowed paths (`icacls`), and launches the command with
> `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` (no network capability, no filesystem access
> outside the granted directories). It has been **verified on a real Windows host**. Whole-process
> isolation there remains a Job Object / container deployment concern (Part 4).

#### 8.1 Whole-process wrapping

The per-command wrappers confine each spawned command; the SDK's own file writes
and the process's own authority are untouched. For a stronger boundary, a
top-level `wrap` block re-executes agentd inside the wrapper, so the **whole
process** shares one confinement:

```yaml
wrap:
  process: bwrap          # none (default) | bwrap (Linux) | seatbelt (macOS)
  failurePolicy: fail-closed   # fail-closed (default) | fail-open
```

- On startup (after `-check`), if `wrap.process` names a registered
  whole-process wrapper and the process is not already wrapped (env
  `AGENTD_SANDBOXED`), agentd re-execs itself under the wrapper (`syscall.Exec`),
  passing its own args and the same environment plus the sentinel. The child
  re-runs `runMain`, sees the sentinel, and serves.
- The wrapper allows **writes only** under the computed roots — each server's
  `root`, `workingDirectory` and `isolation.baseDir`, plus the system temp dir
  (config dirs are read-only for the agent, so they are not write roots). Unlike
  the per-command wrapper it **keeps the network up**, because the process serves
  listeners and calls the model.
- Unix replaces the process image (`syscall.Exec`); Windows has no such call, so
  it launches agentd in an AppContainer and waits for it, exiting with the child's
  status (best-effort, §8): the profile gets the `internetClientServer` capability
  (to serve and call the model) and a loopback exemption is attempted for the
  listener ports. Container / Job-Object wrapping remains the stronger deployment
  option (Part 4).
- `wrap.failurePolicy` mirrors `sandbox.failurePolicy`: `fail-closed` (default)
  refuses to start when the wrapper is unavailable; `fail-open` warns and runs
  unwrapped.

### 9. Non-goals (SDK)

- Three-platform sandbox profiles in core; that is long-term security debt.
- Container/VM orchestration, network proxying, or per-command resource policy.
- Creating projects/directories, starting processes, or managing leases (all Part 2 / host).

## Part 2 — agentd project runner (`cmd/`)

### 10. Problem

An SDK `Client` is permanently bound to one `WorkingDirectory`. A single agentd process that
serves many sessions therefore puts them all in one directory: conversations clobber each
other's files and share snapshots. `DESIGN-AGENTD.md` §7 already states the rule — **sharing a
project across servers requires one isolated view per server** — but the code that prepares a
view and starts the corresponding service is not centralized.

### 11. Interfaces

Add a **transport-agnostic** runner under `cmd/` (e.g. `cmd/agentd/isolation`), so `http` and
`a2a` servers share it:

```go
type Transport string // "http" | "a2a"

// ProjectSpec is the project an instance serves.
type ProjectSpec struct {
    ProjectID        string
    Root             string   // SDK state dir (unique per instance)
    WorkingDirectory string   // project/worktree root
    ConfigDirs       []string
}

// IsolationSpec describes how to materialize the working directory.
type IsolationSpec struct {
    Mode         string // none | copy | worktree | overlay
    BaseDir      string // where views are created (required for every mode except none)
    BaseReadOnly bool   // treat the source as read-only; required by overlay
    BaseRef      string // worktree base commit-ish; empty means HEAD
    CowMode      string // overlay CoW: auto | reflink | clonefile
}

// IsolatedProject is a prepared view plus how to tear it down.
type IsolatedProject struct {
    Spec     ProjectSpec
    View     string // the WorkingDirectory to use
    Teardown func(context.Context) error
}

type Runner interface {
    // Prepare materializes the view for iso and returns it (plus a Teardown).
    Prepare(ctx context.Context, p ProjectSpec, iso IsolationSpec) (IsolatedProject, error)
}
```

`container`/`remote` are **external** (host-provisioned, §15); `bwrap` is a **sandbox wrapper**
(§8), not an isolation mode — the local runner accepts only `none | copy | worktree | overlay`.

`Prepare` is pure filesystem/process setup and is the transport-agnostic part;
the actual `Serve` (building the `Client` and starting an `http` or `a2a`
adapter) stays in `run.go`, which already handles both transports. The runner
therefore does not know about transports, and a host can substitute the
`Runner` (for example to delegate `Prepare` to an orchestrator).

`cmd/agentd/config.BuildClient(ClientParams)` already centralizes the
`ClientOptions` assembly, so `run.go` only needs to pass the prepared `View` as
`WorkingDirectory`.

**Local vs external modes.** `none`, `copy`, `worktree`, `overlay` and `bwrap` all materialize a
`View` that *this* process serves (one per `servers[]` entry). `container` and `remote` do
**not**: they run an *external* agentd (in a container/VM/another host) whose listener is not
this process's, so they are provisioned by the host supervisor, which returns a `server_base`
(Part 3). An external instance is still one `Client` = one prepared view; the local runner is
simply not the component that starts it.

### 12. Isolation modes

| Mode | Mechanism | Notes |
|---|---|---|
| `none` | use `WorkingDirectory` as-is | read-only sharing; the degenerate/current case |
| `worktree` | `git worktree add <view> [<commit>\|im/<id>]` | shares the object store; git repos only |
| `overlay` | overlayfs / `cp --reflink` / `clonefile` | "shared read-only base + CoW writable layer"; `BaseReadOnly` required |
| `copy` | `rsync`/copy | universal, linear disk cost |
| `bwrap` | namespaces (+ optional CoW view) | filesystem/process/network isolation without a container |
| `container` | Docker/Podman/K8s; run agentd inside | strongest: fs + process + network + resources |

Only `none`/`copy`/`worktree`/`overlay` are agentd **isolation modes**; `bwrap` is a sandbox
wrapper (§8), and `container`/`remote` are external — host-provisioned and rejected by the local
runner (§11, §15).

Selection is a deployment decision, but the recommended default for a writable agent is
**`overlay` (or `worktree` in a git repo), optionally wrapped in `bwrap`/a container** — one
isolated view per conversation, not "shared directory + separate fs/network sandboxes".

Each view is served by its **own** `Client` (own `Root` and `WorkingDirectory`); a single
agentd can host several local views (one per `servers[]` entry), while `container`/`remote`
views are a separate process (Part 3). agentd never assumes it may control another process
tree.

View layout and hygiene:
- A view is created under `baseDir/<ProjectID>`; `Root`, `WorkingDirectory` and the view name
  must all be unique per instance (see `DESIGN-AGENTD.md` §7 constraint 1).
- For git projects, keep the worktree outside the source tree or add it to exclusions, and
  exclude the view directory (and `.git`) from the agent's `glob`/`grep` and reference scanning
  so it is not read back as project content. **Implemented**: agentd computes, per server, the
  isolation base directories that sit inside the server's working directory and passes them as
  `ClientOptions.SearchExcludes` (ripgrep `--glob=!…` patterns) — so `glob`/`grep` omit a view
  even when it lives under the project. `.git` is always excluded.
- A `bash` agent inside a `worktree` can still reach the shared object store (`git checkout`
  another branch/object); choose `container` when that escape must be closed.

### 13. Configuration

Extend the per-server config (`cmd/agentd/config.Server`) with isolation and a process hook:

```yaml
servers:
  - name: group-a
    kind: http
    listen: 127.0.0.1:8787
    root: /var/lib/agentd/group-a
    workingDirectory: /var/lib/agentd/group-a/view   # the prepared view
    isolation:
      mode: overlay
      baseDir: /var/lib/agentd/views
      baseReadOnly: true
      cow:
        mode: auto            # auto | reflink | clonefile
    sandbox:
      process: seatbelt          # none | seatbelt | bwrap | appcontainer
      failurePolicy: fail-closed # fail-closed (default) | fail-open
      profile: standard          # off | standard | custom (default: standard when enabled)
      filesystem:
        read: allow              # allow | off (no read confinement; Windows rejects off)
        write: allow             # allow | off (no write confinement; Linux needs read off too)
        denyRead: []             # more specific than an allow
        allowRead: []
        denyWrite: []
        allowWrite: []
      network:
        mode: no-egress          # off | no-egress (a domain allow-list needs a proxy, not implemented)
      unixSockets:
        mode: inherit            # inherit (file policy) | allowAll | allowlist
        allow: []
      processInspection: deny    # deny | allow (explicit opt-out)
      env:
        mode: scrub              # scrub | inherit
        scrub: ["*_KEY", "*_SECRET", "*_TOKEN", "*_PASSWORD"]
      violations:
        mode: heuristic          # heuristic | precise (linux) | off
        reportToModel: true      # append a denial note to command output
```

`profile` is the easy path: `off` disables the sandbox (and its guards); `standard` applies the
secure defaults above; `custom` starts from "no confinement" and applies only the explicit fields.
An explicit `read: off` / `write: off` / `network: mode: off` / `processInspection: allow` is a
deliberate opt-out, never a silent downgrade. `agentd` logs when a sandbox block disables itself.

`container`/`remote` are **not** config fields: such an instance runs its own agentd (in a
container/VM/remote host) and is provisioned by the host supervisor (§15), so agentd only
holds the local modes `none | copy | worktree | overlay`.

Validation (fail-fast, like the rest of `config.Validate`):
- `mode != none` requires `baseDir`; `mode == overlay` also requires `baseReadOnly: true`.
- `mode == worktree` requires a git working directory (enforced when the view is prepared).
- `sandbox.process` is required when `sandbox` is set; `sandbox.failurePolicy` is
  `fail-closed` (default) or `fail-open`. A name that matches no registered wrapper fails
  startup (fail-closed) or logs and runs without the sandbox (fail-open).
- `sandbox.profile` is `off | standard | custom`; `filesystem.read`/`write` are `allow | off`;
  `network.mode` is `off | no-egress` (a domain allow-list is rejected until a proxy exists);
  `unixSockets.mode` is `inherit | allowAll | allowlist` (allowlist requires a path; the
  pre-rename `deny` is accepted as an alias for inherit);
  `processInspection` is `deny | allow`; `env.mode` is `scrub | inherit`. Unsupported rules fail
  startup — notably a **write glob on Linux** (bwrap cannot express it), Windows `read/write: off`
  and `processInspection: allow` (the AppContainer cannot express them), and Linux `write: off`
  together with `read: allow` (contradictory) — never a silent allow-all.

Whole-process wrapping is a **top-level** `wrap` block (§8.1), not a per-server
field, because it applies to the whole process:

```yaml
wrap:
  process: seatbelt
  failurePolicy: fail-closed
  profile: standard            # off | standard | custom
  filesystem: { read: allow, write: allow }
  # Only the filesystem policy applies to the wrapped process: it keeps the
  # network and process introspection up (it serves listeners, calls the model
  # and manages children) and inherits its own environment. Its config dirs,
  # state roots and executable are re-allowed (FR12).
```

`wrap.process` is required when `wrap` is set; `wrap.failurePolicy` is
`fail-closed` (default) or `fail-open`. The whole-process wrapper re-allows every server's
state root, working directory, config dirs and the executable, so the process can start and
serve; the policy still confines the commands and tools it spawns.

### 14. Integration into `run.go`

`run()` already builds one `Client` per configured server and groups servers by listener.
Threading the runner in keeps the existing shape:

```
for each server s in cfg.Servers:
    iso, err := runner.Prepare(ctx, projectSpec(s), s.Isolation)   // view + teardown
    defer iso.Teardown(ctx)
    client, err := config.BuildClient(clientParams(s, view=iso.View, process=sandbox))
    built, err := buildServer(ctx, s, client, …)
…  # unchanged: group by listen, bind, serveAll
```

- `isolation.mode: none` makes `Prepare` a no-op returning `WorkingDirectory` — zero behavior
  change for existing single-directory deployments.
- Only the local modes (`none`/`worktree`/`overlay`/`copy`/`bwrap`) run through this loop;
  `container`/`remote` are provisioned as separate processes by the host (see §15), not here.
- `Teardown` (worktree remove / overlay unmount) runs on graceful shutdown; container teardown
  is the host's.
- The `sandbox.process` value is translated to an `agent.ClientOptions.Process` wrapper and
  passed through `BuildClient`.

### 15. Dynamic provisioning (supervisor vs control API)

agentd's config is static (`servers[]` are fixed at startup). Per-conversation isolation needs
provisioning **at runtime**. Two options:

1. **Host supervisor.** The host spawns a dedicated agentd per project:
   `agentd --config <generated>` (or the same binary with `--project/--isolation` flags), then
   registers the resulting `base_url` with the bridge. No new network surface; lifecycle is the
   host's. Tether drives this via its `ProjectProvider`/`supervisor_command`.
2. **Control API (implemented, opt-in).** A long-running agentd exposes a control API when
   `control.listen` is set:

   | Method + path | Body / effect |
   |---|---|
   | `POST /admin/projects` | `{name,kind,root,workingDirectory,configDirs,models,defaultModel,agents,peers,isolation,readonly,auth}` → `201 {project_id,kind,server_base,transport,working_directory,readonly}` (`peers`: remote A2A agents the instance reaches as a client, same shape as a server's; `auth`: `{type: bearer, token}` like a server's) |
   | `GET /admin/projects` | list running instances |
   | `DELETE /admin/projects/{id}` | stop the instance and release its view |

   It prepares the view (Part 1 `Runner`), builds a `Client`, binds a listener under
   `control.projectListen` (default `127.0.0.1:0`), and serves it. `control.auth.token`, when
   set, guards the API; a non-loopback `control.listen` **requires** it, and `control.projectListen`
   must be loopback (dynamic instances are served unauthenticated). `control.maxLeases` caps the
   live instance count (0 = unlimited). **Local modes only**
   (none/copy/worktree/overlay): `container`/`remote` are still rejected as external — a
   same-process agentd does not orchestrate containers.

Either way, the **single-owner rule** holds: a writable `(project, transport)` is leased to
exactly one `Client` at a time; read-only projects may be shared. The control API makes this
explicit with a `readonly` request field: several read-only leases of one project **share a
single instance** (a reader count; the last release tears it down), while a writable lease is
exclusive and is refused while any lease exists (and vice versa). `readonly` is mutually
exclusive with `isolation` — a read-only lease shares the source directory.

> Implementation note: the control API is the "agentd is the orchestrator for local views"
> path. Container/VM orchestration remains a deployment concern; run a containerized agentd and
> reach it directly, or wrap it with a host supervisor.
>
> **Reference supervisor:** `cmd/agentd-supervisor` implements the `supervisor_command` JSON
> protocol (acquire/release) and satisfies it through the control API — a host points
> `supervisor_command` at it and gains option-1 ergonomics (one short-lived command per request)
> over option 2, while the host still spawns no agentd itself.
>
> **Container/remote provisioning (deployment).** A `container`/`remote` view is an external
> agentd (in a container/VM/another host) that this process never orchestrates (§11, §15). Two
> workable host patterns:
> - **Remote / one agentd per deployment.** Run a containerized agentd whose `control.listen` is
>   reachable from the host and point the supervisor's `agentd.baseURL` at it; a remote base URL
>   works unchanged, so no SDK change is needed for `remote`.
> - **One container per project.** The host runs `agentd` in a container **per project**,
>   publishing that instance's server port, and returns the published `server_base`.
>
> **Constraint — the control API cannot hide behind a container.** Dynamic instances bind
> `control.projectListen`, which must be loopback (above); a control-API agentd inside a
> container publishes only its own control port, so the per-project `server_base` it returns
> (`127.0.0.1:<port>`) lives *inside* the container and is unreachable from the host.
> Containerizing the orchestrator therefore only works when the container shares the host network
> namespace (`--network host` on Linux) or a per-project container publishes its own port. The
> reference supervisor targets an agentd reachable as-is; container/VM orchestration stays a
> deployment concern.

### 16. Lifecycle, ownership and quotas

- **Create**: runner `Prepare` (copy/mount/clone into `baseDir`) → `run.go` builds the `Client`
  for the view and starts the transport → host records the lease.
- **Reclaim**: host releases on idle TTL; `Teardown` removes the worktree/overlay/container.
- **Quotas**: cap concurrent leases (disk/memory); one view + one `Root` per instance. The opt-in control API caps the **instance count** via `control.maxLeases` (0 = unlimited); disk/memory quotas remain the host/lease layer's.
- **Ownership**: one writer per project; multiple readers allowed.
- **Isolation of state**: each instance has its own `Root`; the view path is also excluded from
  the agent's own `glob/grep`/references so it is not scanned as project content.

## Part 3 — Host contract (how Tether consumes it)

Tether keeps its `ProjectProvider` for the **decision and lease**, and gains a thin adapter to
the agentd runner instead of implementing filesystems itself:

```
Tether.ProjectProvider.acquire(conv, readonly) →
   supervisor_command(project_id, isolation_spec, transport)   # host side
     → supervisor starts an agentd instance for the view       # local: runner prepares, agentd serves
     → (container/remote mode: supervisor runs agentd in a container/VM instead)
     → returns {project_id, transport:"http", server_base, working_directory}
Tether.ProjectProvider.release(lease) → supervisor tears the instance down
```

- `ProjectLease{project_id, transport, server_base, working_directory, ephemeral}` is unchanged
  (`DESIGN-IM-BRIDGE.md` §3.7, Tether repository).
- `readonly` (from `GET /api/tool?agent=`) selects `mode: none` sharing vs a per-conversation
  view.
- If the runner is **not** present, Tether falls back to its static `server.pool`; it must not
  fork Go processes itself.

## Part 4 — Cross-platform enforcement reference

Dimensions a sandbox wrapper should cover, and representative primitives:

| Dimension | Blocks | Linux | macOS | Windows |
|---|---|---|---|---|
| Filesystem view | reads outside the project (`~/.ssh`, `/etc`) | `bwrap --ro-bind` | `seatbelt` profile | AppContainer (+ low IL) |
| Network | egress / SSRF | `bwrap --unshare-net`, proxy allowlist | `seatbelt` | WFP / AppContainer (no net by default) |
| Privileges | escalation | user namespaces | drop uid | restricted token / low IL |
| Resources | CPU/mem/PIDs | `cgroup` | `ulimit`/`launchd` | Job Object |

For VM-level isolation use a container/VM around the **whole** agentd process — simpler and
stronger than wrapping command-by-command, and it simultaneously provides project isolation
(separate mounts/`Root`) and the sandbox.

## Part 5 — Non-goals and red lines

- **Not** an SDK hook: project/process isolation. The SDK provides only the process seam and
  "one `Client`, one project".
- **Not** in core: maintaining three-platform sandbox profiles, container orchestration,
  network proxying, or lease management.
- **Never**: mounting one `Client` on two transports; sharing one writable directory without a
  copy; sharing one `Root` across servers.

## Part 6 — Testing

- **SDK seam**: with a fake `core.AppProcess` injected via `ClientOptions.Process`, assert
  `bash`, `ripgrep` **and** `git` all route through it (guards the `runtime.go` wiring hole).
- **Failure policy**: `fail-open` falls back to pass-through (logged); `fail-closed` errors.
- **Runner**: `Prepare` for each mode produces an independent view; two views of the same base
  do not observe each other's writes; `Teardown` removes the view.
- **Config**: each new field's invalid values fail validation (missing `baseDir`, overlay
  without `baseReadOnly`, unknown runtime/process).
- **Integration**: two `servers[]` entries sharing a base but with distinct views do not cross
  sessions. A shared read-only directory is allowed; a shared **writable** directory is rejected
  at the **lease layer**, not by static config — like Kubernetes `ReadWriteOnce` vs
  `ReadWriteMany`, the access mode is a runtime decision (the host knows `readonly`; the static
  config does not). The control API enforces it by rejecting a duplicate project name / shared
  root, and static validation only enforces one `Root` per `servers[]` entry (D8).
- **Sandbox (optional)**: on Linux/macOS CI, a `bwrap`/`seatbelt` wrapper blocks a write to a
  path outside the view and blocks network egress; skipped where the sandbox is absent.

## Part 7 — Milestones

Status: S0–S2 and the local half of S3 are **implemented** (see the code); S4 and container
orchestration remain open.

| Phase | SDK (`pkg/`) | Command (`cmd/`) |
|---|---|---|
| S0 ✅ | `ClientOptions.Process` wired into builtins + runtime git; seam test | — |
| S1 ✅ | (wrappers still optional) | `Runner` (`none`/`copy`/`worktree`) + `isolation` config/validation + `run.go` integration |
| S2 ✅ | `FileWriteGuard` wired into write/edit/apply_patch | `overlay` (reflink/clonefile) + `baseRef`; teardown on shutdown; `sandbox.process` wiring |
| S3 ◐ | — | **control API implemented for local modes**; `container`/`remote` remain host-provisioned |
| S4 ✅ | `bwrap` (Linux) + `seatbelt` (macOS) shipped; `appcontainer` (Windows, best-effort) | whole-process `bwrap`/`seatbelt`/`appcontainer` wrapping shipped (top-level `wrap`, §8.1) |

> The Windows `appcontainer` wrapper (per-command and whole-process) is best-effort and
> unverified on a real host. `container`/`remote` are never local — host-provisioned (§15).

## Part 8 — Decisions

- **D1** Reuse `core.AppProcess` as the seam (no new `CommandRunner` interface).
- **D2** Default `Process` is nil → `core.NewAppProcess()` (pass-through). When a named sandbox
  *is* configured but cannot be built, the default is **`fail-closed`** (refuse to start);
  `fail-open` is the opt-out for trusted desktop embedding.
- **D3** The runner lives in `cmd/`, transport-agnostic; the SDK contributes only the seam and
  the "one `Client` = one project" primitive.
- **D4** A writable `(project, transport)` has a single owner; read-only may be shared.
- **D5** Provisioning supports both a host supervisor *and* an opt-in in-process control API;
  the control API covers local modes only, container/remote stay host-provisioned.
- **D6** Whole-process wrapping is a **top-level `wrap` block** that re-execs agentd inside the
  wrapper (§8.1); `Prepare` does not apply the sandbox per view. The recommended strongest form
  for a fully untrusted tenant remains a container/VM around the whole process (deployment).
- **D7** `git` runs through the **same** process seam as `bash`/`ripgrep` — one injection applies
  everywhere: `runtime.go` builds `Git` from `ClientOptions.Process`, and a git-repo-backed
  client exercises it at construction (`pkg/agent/process_test.go`
  `TestClientUsesInjectedProcess`). Sandboxing `git` is intended; a deployment that needs `git`
  to reach a `.git` outside the view must grant it explicitly (whole-process roots, §8.1).
- **D8** The "one writer per project" and root-uniqueness constraints are enforced at the
  **runtime/lease layer**, not by static config (industry norm: K8s `ReadWriteOnce`/`ReadWriteMany`
  access modes, enforced at mount). Static validation enforces one `Root` per `servers[]` entry;
  the control API enforces one instance per project name / root. A shared *writable* working
  directory is therefore a host/lease decision, not a config error — `readonly` is unknown to the
  static config.
