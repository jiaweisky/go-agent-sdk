# Sandbox enforcement — requirements & gap analysis

> **Companion to `docs/DESIGN-ISOLATION.md`.** That document is the *implementation design*
> (the `ClientOptions.Process` seam, the agentd runner, the wiring, and the shipped
> `bwrap`/`seatbelt`/`appcontainer` wrappers). **This document is its enforcement companion**:
> it states the **security properties a sandbox must guarantee** (the requirements), maps them
> across macOS / Linux / Windows, and records the **gap between the design's claims and the
> shipped profiles** — so the *native* support can be completed to a professional level.
>
> It is **not** a competing design and it changes no interface. Where it disagrees with a
> sentence in `DESIGN-ISOLATION.md`, the disagreement is called out explicitly (§4) and the
> design's own §8 caveat is the tie-breaker.
>
> **Positioning (the goal): completing the *native* sandbox.** `DESIGN-ISOLATION.md` already
> ships a `Sandbox` seam and best-effort wrappers (`S4 ✅`). The requirements below (FR1–FR12)
> are what remains for the native support to actually hold. `srt`
> (`github.com/anthropics/sandbox-runtime`) is used here only as the **reference semantics** and
> an optional accelerator; layering it on top does **not** replace native support, and native
> support does **not** require it — the two compose (§0).

## 0. Conclusion and roadmap (read this first)

**Native-first: the SDK's own sandbox is the delivery target; `srt` is an optional
accelerator/reference, not a dependency.**

`srt` does not add a new security boundary — it drives the same OS primitives this SDK already
uses (`seatbelt` on macOS, `bwrap` on Linux, `AppContainer` on Windows), plus a network proxy.
The SDK can therefore reach equivalent protection on its own, and its architecture is in fact
more favourable: it also owns the **in-process** tools (`read`/`write`/`edit`) that an OS
process sandbox cannot see (FR7 / D11). `srt` remains useful as a **reference and optional
accelerator** for the network-domain-allow-list phase and for its accumulated hardening/CI, but
it is **not required** for professional protection under the §2 threat model.

| Layer | Owner | Content |
|---|---|---|
| **Native core** (self-built, required for delivery) | go-agent-sdk | FR1 read allow-list, FR2 write (done), FR4 no process introspection, FR5 env scrub, FR6 UNIX-socket path policy (partial; seccomp later), FR7 the in-process tools share the policy, FR8 fail-closed (done), FR10 validation, FR11/FR12; three platforms (Windows `AppContainer` is implemented and real-host tested). **Same primitives as `srt` (`seatbelt`/`bwrap`/`AppContainer`) → reachable at parity.** |
| **Optional product layer** (deferred; reuse `srt` or a self-built/host proxy) | `srt` layered outside (`wrap`) or the host | Network **proxy allow-list** (domain allow-list + resolved-address check + optional TLS termination + credential masking), **violation store / audit**, `--control-fd` live config, Windows WFP (only needed for a domain allow-list), cross-platform CI/release. **Not needed now** — FR3's minimum is hard no-egress, already provided per-command. Adding it later is a **cost/maintenance** decision, not a capability limit. |

**Why this split.** The native core is the **execution seam** (`AppProcess` / `wrap`); a proxy
layer composes on top and is **optional**. "Professional" is not "build everything at once"; it
is **a self-built core that meets the §9 bar**, with the network-domain-allow-list capability
added only when a deployment needs it.

**Acceptance, per layer.**
- **Native core** — pass this document's §9 items 1–9, with the network case expressed as **hard
  no-egress** (item 3a).
- **Domain-allow-list layer (only if/when added)** — additionally pass the allow-listed-host case
  (item 3b) and the violation-event checks.

## 1. Scope, and where this sits in the design

`DESIGN-ISOLATION.md` splits the work into **two orthogonal layers** (§1):

| | Part 1 sandbox (SDK seam) | Part 2 project isolation (agentd runner) |
|---|---|---|
| Guards | **agent → host** | conversation ↔ conversation |
| This document | **requirements + gap analysis for the sandbox** | out of scope (see the design) |

In the design's terms, this document specifies the **contract of the `Sandbox`** (the thing
`sandbox.process` / `wrap.process` resolves to, §8 / §8.1) and of `ClientOptions.FileWriteGuard`
(§7): what they must *enforce*, not how they are wired.

## 2. Threat model

**Assumed deployment.** Tether (the IM bridge) and agentd run on the **same host, same user**
(typical single-operator setup). The agent runs with the built-in `bash`/`ripgrep`/`git` and the
in-process `read`/`write`/`edit`/`apply_patch` tools, driven by **untrusted user input** via IM.

**Adversary.** A user (or a prompt-injected model) inducing the agent to run tools that reach
outside the project. The adversary does **not** have OS-level or root access; it acts **through
the agent's tools**.

**Assets to protect.**
- **A1 — bridge/agentd configuration** (`tether/config.yaml`, `agentd.yaml`): e.g. `group.admins`.
- **A2 — credentials**: `tether/.env`, `agentd` env (`DEEPSEEK_API_KEY`, control token), `~/.ssh`,
  `~/.aws`, cloud/git credentials.
- **A3 — the host at large**: any file outside the project, and other processes' state.

**Attack paths (must be blocked).**
- **P1 privilege escalation** — write A1 (e.g. add oneself to `group.admins`) → hot-reload.
- **P2 secret exfiltration** — read A2, then send it out via IM (or the network).
- **P3 lateral** — read another process's environment/memory, or a sibling project's files.

**Why "permission" and "deny" are insufficient (design §3).** `DESIGN-ISOLATION.md` §3 states the
core fact this document builds on: the SDK's `permission` policy only decides **whether** a
command runs; once allowed it runs with the host user's full authority. `cat`, `sed -i`,
`python -c "open(...)"`, `env`, `ps eww`, `sysctl` all bypass any path-level `allow`/`deny`
expressed in agentd's own logic. **Only OS-enforced restrictions are load-bearing.**

## 3. Requirements

Each requirement is stated as a property, with the design reference, the **shipped state**, and
the level. "Level" = required for the native sandbox to be considered complete.

| # | Property | DESIGN ref | Shipped state | Level |
|---|---|---|---|---|
| **FR1 Read confinement** | Reads are **allow-only** (or deny-region + re-allow): by default the process may read **the project's working directory + the system run-time paths**; everything else (home, `/etc`, sibling projects, A1/A2) is denied. A more specific deny beats a broader allow (`allowRead:["."]` + `denyRead:["**/.env"]` ⇒ the `.env` stays denied). | Part 4 table ("Filesystem view · reads outside the project"); §8 note (a *denylist*) | ✅ **implemented (S5)**: seatbelt denies the home parent/`/etc` and re-allows the project + explicit paths; bwrap renders an allow-only mount view (literal deny paths masked); the in-process `read` guard shares the policy. | **Required** |
| **FR2 Write confinement** | Writes are **allow-only**: only the working directory + system temp; a more specific deny beats (`denyWrite:["**/.env"]`). | §8, §8.1, §13 | ✅ implemented (`(deny file-write*)(allow file-write* <roots>)`) | Required (done) |
| **FR3 Network confinement** | Two levels: **(3a)** a hard **no-egress** mode for sandboxed commands (required now); **(3b)** an optional **allow-only by domain** mode (via a proxy), for the whole process tree. | Part 4 ("Network · egress/SSRF": `--unshare-net` / proxy allowlist) | ⚠️ per-command `(deny network* (remote ip))` (3a) exists; whole-process wrapping **keeps the network up** (§8.1); a domain allow-list (3b) is **deferred**, not delegated. | Required: **3a**; **3b deferred (S6+)** |
| **FR4 Process introspection** | A sandboxed command may not read **other processes'** environment or memory (`process-info*`, `sysctl`, `task_for_pid`/`ptrace`). Without this, `env`/`ps eww` still yield agentd's secrets (P2/P3). | — (not covered) | ✅ **implemented (S5)**: per-command `(deny process-info*)`/`(deny sysctl-read)` on seatbelt, `--unshare-pid` on bwrap, the low-integrity AppContainer on Windows. | Required |
| **FR5 Child environment hygiene** | Commands are spawned with a **sanitized environment**: secret variables (A2) are not inherited. | — | ✅ **implemented (S5)**: `*_KEY` / `*_SECRET` / `*_TOKEN` / `*_PASSWORD` are scrubbed for every wrapper (`env.mode: scrub`, overridable with `inherit`). | Required |
| **FR6 UNIX sockets** | Blocked by default; allow-listed explicitly (e.g. `docker.sock`). | Part 4 (implied) | ⚠️ **partial**: `unixSockets.mode` is `inherit` (default: follow the file policy), `allowAll` (no socket-specific policy; the file policy still applies) or `allowlist` (explicit paths; granted on Windows). A **per-run private temp** (`{Root}/data/sandbox-tmp`) hides host temp sockets; a socket inside the project's writable root is still reachable unless excluded with `filesystem.denyWrite`. macOS denies remote IP only, so AF_UNIX follows the file policy and allowlist works. A true socket-wide deny needs seccomp/LSM. `deny` is a legacy alias for `inherit`. | Should |
| **FR7 Uniform scope** | The **same** path/network policy governs (a) **per-command** spawns (`sandbox.process`), (b) **whole-process** wrapping (`wrap`), **and** (c) the **in-process tools** (`read`/`grep`/`write`/`edit`/`apply_patch`). A hole in any one is a hole. | §4/§5 (seam) + §7 (`FileWriteGuard`) + §8.1 (`wrap`) | ✅ **implemented (S5)**: one `sandbox.Policy` drives the wrapper profile and a `PathGuard` installed as `FileReadGuard` + `FileWriteGuard` (grep/glob go through the sandboxed `AppProcess`). | Required |
| **FR8 Failure policy** | A named sandbox that cannot be built ⇒ **fail-closed** (refuse to start / refuse the command). `fail-open` is an explicit opt-out. | §6, §13, **D2** | ✅ `sandbox.failurePolicy` / `wrap.failurePolicy` (fail-closed default) | Required (done) |
| **FR9 Violation observability** | Denied reads/writes/connects/introspection are **reported** as structured events (audit + can be fed back to the model: "blocked by sandbox"). | — | ✅ **implemented (S5)**: `session.next.sandbox.denied` reaches `Client.On`/SSE. `heuristic` (cross-platform stderr scan) is the default, `precise` (Linux syscall tracing via strace) is opt-in, and `reportToModel` appends a denial note to command output; file tools also say "blocked by sandbox". | Should |
| **FR10 Config validation / no silent downgrade** | A rule the platform cannot honor (e.g. a write glob on Linux) **fails validation**; it is never silently ignored into "allow-all". | §13 (validation) | ✅ **implemented (S5)**: modes are validated, a Linux deny glob and a domain allow-list are rejected, and an unknown wrapper still fails closed (or warns under `fail-open`). | Required |
| **FR11 Cross-platform Consistency** | One `sandbox` config means the same *intent* on all three OSes; capability differences are **documented + validated** at startup. | Part 4 | ✅ **implemented**: startup validation rejects unsupported combinations (precise outside Linux, Linux deny globs, domain allow-list, and `off` modes the platform cannot express), and a portable acceptance test covers read/write confinement, env scrub and violation reporting on the host OS. The Linux/Windows CI lanes still need to execute it on those hosts. | Required |
| **FR12 Bootstrap exception** | Whole-process wrapping must let the process read **its own** config before confinement (or exempt it), while commands/tools still cannot. | §8.1 | ✅ **implemented (S5)**: `wrap` re-allows the executable, state roots and config dirs; config is loaded before the re-exec. | Required |

## 4. Gap analysis: design claims vs. shipped code (macOS `seatbelt`)

This is the historical gap record (before S5): it reconciles three places in
`DESIGN-ISOLATION.md` with what `cmd/agentd/sandbox/seatbelt_darwin.go` actually
enforced at the time. The gaps below are closed by S5 (see §3 and §12).

| Source | Says | Reality in shipped code | Verdict |
|---|---|---|---|
| §8 note (lines ~195–201) | the wrappers block writes + net + **reads of a denylist**; "use a container or whole-process wrapping for a strict view" | before S5, accurate **for reads** (denylist) | resolved: S5 emits a deny-region + re-allow read policy; `wrap` re-uses it |
| Part 4 table | "Filesystem view · reads outside the project (`~/.ssh`, `/etc`) | `seatbelt` profile" | before S5, the profile denied only a small home denylist; `/etc` was not masked | resolved: S5 denies the home parent/`/etc` by default |
| Part 6 Testing | "a `bwrap`/`seatbelt` wrapper blocks a write ... and blocks network egress" | matches (writes + net tested) | **correct**; **reads are not tested** (because they are not enforced) |

**Conclusion (before S5).** The then-shipped sandbox was **write + (per-command) network + a small
read denylist**. It did **not** confine reads, did not touch process introspection, and whole-process
wrapping was **not** the "strict view" the §8 note implies. This was a deliberate *best-effort*
posture (§8/S4),
**not** a platform limitation: `seatbelt`/`bwrap` can confine reads (below), and the SDK can emit
the same rules itself.

## 5. Cross-platform enforcement reference

Extends `DESIGN-ISOLATION.md` Part 4. Each row is the primitive that can satisfy the FR.

| Dimension | Requirement | macOS (`sandbox-exec`/SBPL) | Linux (`bwrap`) | Windows |
|---|---|---|---|---|
| Read view (FR1) | allow-only reads | `(deny file-read*)` + `(allow file-read* (subpath …))` (or deny broad regions + re-allow) | **mount** only the allow-listed paths (`--ro-bind`), nothing else visible | dedicated user + per-dir ACL (or AppContainer) |
| Write (FR2) | allow-only writes | `(deny file-write*)` + `(allow file-write* …)` *(shipped)* | writable `--bind` allow-list | write ACE on the working tree only |
| Network (FR3) | **3a** no egress now; **3b** allow-only egress later | 3a: `(deny network* (remote ip))`; 3b: allow only the local proxy port + domain allow-list | 3a: `--unshare-net`; 3b: proxy over a bind-mounted UDS | 3a: AppContainer with no `internetClientServer` capability (already the per-command shape); 3b: WFP fence keyed on the profile SID |
| Process info (FR4) | no cross-process env/memory | `(deny process-info*)` `(deny sysctl-read)` | PID namespace + seccomp (`ptrace`/`process_vm_readv` denied) | AppContainer low-integrity isolates other users'/sessions' processes; a dedicated user is the stronger shape |
| Env (FR5) | sanitized child env | launch with a scrubbed env | same | same |
| UNIX sockets (FR6) | deny by default | `(deny …)` + `(allow … (subpath …))` | seccomp (path-agnostic → default-deny; `allowAll` to opt out) | ACL / AppContainer |
| Fail policy (FR8) | fail-closed | wrapper resolution error ⇒ refuse (§6/§13) | same | same |

Notes:
- **macOS is not the limit.** Seatbelt's SBPL supports both `(deny default)` profiles and
  `(deny file-read*)/(allow file-read*)` region rules; `srt` uses the latter (read = deny broad
  regions, then `allowRead` the project). The shipped profile simply never *emits* a read rule
  beyond the credential denylist.
- **Linux is the cleanest** for FR1: `bwrap`'s mount namespace makes "reads outside the view"
  structurally impossible — no profile to get wrong. `read: off` falls back to `--ro-bind / /`
  (no masks); `write: off` with `read: off` binds `/` read-write.
- **Windows is implemented, not just planned.** `cmd/agentd/sandbox/appcontainer_windows.go`
  already creates an AppContainer profile and runs each command inside it: a per-command child
  gets **no capability** (hence no network) and can reach only the granted run directory;
  whole-process re-exec adds `internetClientServer` plus a best-effort loopback exemption. The
  path has been **verified on real Windows**. What remains is policy completeness and
  documentation of exactly which FRs hold (FR4/FR5/FR9/FR10), not a new primitive. A domain
  allow-list (3b) would need a proxy (or WFP) later.

## 6. Proposed config surface (extends `DESIGN-ISOLATION.md` §13)

`sandbox.process` / `sandbox.failurePolicy` stay as designed. Add a policy block, named to match
`srt` (low migration cost), applied by **every** wrapper (per-command, whole-process, and the
in-process guards):

```yaml
sandbox:
  process: seatbelt            # none | bwrap | seatbelt | appcontainer  (unchanged)
  failurePolicy: fail-closed   # (unchanged)
  filesystem:
    denyRead:  ["~", "/etc"]                       # broad regions to deny
    allowRead: ["{workingDirectory}", "/usr", "/System", "/Library", "/dev"]
    allowWrite: ["{workingDirectory}"]   # the per-run temp is added automatically
    denyWrite: ["**/.env", "**/.yaml", "**/.git/config"]
  network:
    mode: no-egress                                # off | no-egress; a domain allow-list is not implemented
  unixSockets:
    mode: allowlist                                # inherit | allowAll | allowlist
    allow: ["/var/run/docker.sock"]                # allowlist: explicitly reachable sockets
  allowProcessInspection: false                    # FR4
  scrubEnv: ["*_KEY", "*_SECRET", "*_TOKEN", "*_PASSWORD"]   # FR5
  violations:
    mode: heuristic                                # heuristic | precise (linux) | off (FR9)
    reportToModel: true                            # append "blocked by sandbox" to command output
```

Validation (FR10): unknown keys, unsupported patterns for the platform (Linux write globs),
and `allowProcessInspection: true` under `fail-closed` intent ⇒ **startup error**, never a silent
fallback. `{workingDirectory}` is the prepared view (Part 2 / §8.1 roots).

The `network.allowedDomains` block belongs to the **optional** domain-allow-list layer (FR3 3b,
S6+); the native core implements only the no-egress mode (an empty/absent block).

## 7. Scope matrix — one policy, three enforcement points (FR7)

| | per-command (`sandbox.process`) | whole-process (`wrap`, §8.1) | in-process tools (`read`/`write`/`edit`) |
|---|---|---|---|
| Read (FR1) | profile `file-read*` | profile `file-read*` | path policy in the tool (new; today only `FileWriteGuard` for writes, §7) |
| Write (FR2) | profile `file-write*` | profile `file-write*` | `FileWriteGuard` **(done, §7)** |
| Network (FR3) | 3a: profile no-egress; 3b (deferred): proxy | 3a n/a (process needs listeners/model); 3b: proxy | n/a (tools don't dial) |
| Process info (FR4) | profile | profile | n/a |
| Bootstrap (FR12) | n/a | **exempt own config**, or read before entering | n/a |

**Key point:** the OS sandbox does **not** cover the in-process tools. Today only *writes* are
guarded (§7 `FileWriteGuard`); **reads through `read`/`grep` are not** — they lean on
`permission`, which §3 shows is bypassable by `bash`. A complete native sandbox must route the
in-process **read** tools through the same path policy.

## 8. Failure policy and observability

- **FR8** is met by `failurePolicy` (D2). Keep **fail-closed** as the default (the design already
  does); `none` must be an *explicit* choice and should log a prominent warning.
- **FR9 (new)**: emit a structured event for every denial (`file-read`/`file-write`/`network`/
  `process-info`), carrying the path/host and the command id. Two consumers: the operator (audit)
  and the model (`<sandbox_violations>`-style, so it learns what is blocked rather than retrying).
  `srt`'s violation store is the reference.

## 9. Acceptance criteria

Extends `DESIGN-ISOLATION.md` Part 6 ("Sandbox (optional)"). Run on each platform where the
wrapper exists; **each must hold**:

> The runnable Linux/Windows checklist — commands, scenarios, expected results and
> conditional fixes — is in **`docs/SANDBOX-VERIFICATION.md`**.

1. **A1/A2 read** — `cat` of `tether/config.yaml`, of agentd's own config, and of `~/.ssh/id_rsa`
   ⇒ denied (not "file not found").
2. **Write** — appending to A1 ⇒ denied; writing under the working directory ⇒ allowed.
3. **Network** — **3a (native core):** any outbound connection from a sandboxed command is denied
   (hard no-egress). **3b (deferred; only when a domain allow-list is added):** a non-allow-listed
   host ⇒ denied, an allow-listed host ⇒ allowed.
4. **Process introspection (FR4)** — `env` in a command does not contain A2; `ps eww <agentd-pid>`
   / `sysctl` cannot read agentd's env.
5. **In-process reads (FR7)** — the `read`/`grep` tools are denied for a path outside the view
   (not merely hidden from `bash`).
6. **Whole-process (FR12)** — under `wrap`, agentd still starts (reads its own config) and serves,
   yet (1)–(5) still hold for commands/tools.
7. **fail-closed (FR8)** — remove the wrapper ⇒ startup/command **refuses** (never silent pass-through).
8. **Validation (FR10)** — an unsupported rule ⇒ **startup error**, never silent allow-all.
9. **Both sides** — every case holds for **Tether's** config and **agentd's** config alike.

## 10. Milestones and decisions (extends Part 7 / Part 8)

| Phase | Scope |
|---|---|
| **S5 (implemented)** | FR1 read confinement, FR4 process-info denial, FR5 env scrub and the FR6 path policy (partial) in the shipped `seatbelt`/`bwrap` profiles and the Windows `AppContainer`; FR7 in-process read+write guard; FR9 violation events; FR10 validation; FR12 wrap bootstrap. Violation reporting is layered: the in-process guard is authoritative (`enforced`); command denials are detected from stderr (`suspected`, because Linux allow-only views return ENOENT and macOS has no per-process denial trace). Real-host verification: macOS (unit + integration, run here); Linux/Windows compile and are covered by unit tests, but need a CI/host pass. |
| **S6 (optional, when needed)** | **(3b) network domain allow-list** — reuse `srt` or a self-built/host proxy (not required for the §2 threat model). |

Proposed decisions (continuation of D1–D8):

- **D9** *Read confinement is a first-class requirement.* The shipped profiles' read **denylist**
  is superseded by an **allow-list** (deny-region + re-allow), per FR1. The `DESIGN-ISOLATION.md`
  §8 note and Part 4 table are reconciled to this (Part 4 reads as the **target**, §8 as the
  **shipped** state until S5).
- **D10** *Whole-process wrapping is not a "strict view" by itself.* `wrap` re-uses the command
  profile; a strict view requires FR1. (Corrects the implication in §8's note.)
- **D11** *The in-process tools share the sandbox policy.* `FileWriteGuard` extends to a read
  guard (or a unified path policy) so `read`/`grep` cannot exceed FR1 — closing the §3 bypass for
  the one tool family the OS sandbox cannot see.
- **D12** *Native support does not depend on `srt`.* The SDK already drives the same primitives
  (`seatbelt`/`bwrap`/`AppContainer`), so parity is reachable natively; `srt` is an optional
  reference/accelerator. The network domain allow-list (FR3 3b) is **deferred, not delegated**:
  FR3's native minimum is hard no-egress, and 3b is added only when a deployment needs it.

## 11. References

| Source | Use | URL |
|---|---|---|
| **Anthropic Sandbox Runtime (`srt`)** | reference semantics + optional accelerator (not a dependency): read deny-then-allow, write allow-only, network proxy allow-list, UNIX-socket deny. Drives `sandbox-exec`/`bwrap`/`AppContainer`-class primitives | `https://github.com/anthropics/sandbox-runtime` |
| Claude Code sandboxing docs | product-side behavior and defaults | `https://docs.claude.com/en/docs/claude-code/sandboxing` |
| Anthropic engineering | design rationale (secure-by-default, dual isolation) | `https://www.anthropic.com/engineering/claude-code-sandboxing` |
| **bubblewrap** (Linux) | mount/net namespace + seccomp primitive | `https://github.com/containers/bubblewrap` |
| Apple **Sandbox Guide** (SBPL) | profile language: `file-read*`/`file-write*`/`process-info*`/`network*` | `https://reverse.put.as/wp-content/uploads/2011/09/Apple-Sandbox-Guide-v1.0.pdf` |
| This repository | shipped state to close the gaps against | `cmd/agentd/sandbox/seatbelt_darwin.go`, `cmd/agentd/sandbox/bwrap_linux.go`, `cmd/agentd/sandbox/appcontainer_windows.go`, `docs/DESIGN-ISOLATION.md` |

## 12. S5 implementation notes (native core) — minimal change points

> Goal: lift the shipped `seatbelt`/`bwrap` profiles from "write + network + a read denylist" to
> the §3 core requirements. Listed as *where / to what / how verified* (Go, `cmd/agentd/sandbox`).

### 12.1 macOS `seatbelt` (`seatbelt_darwin.go`)
- **`writeSeatbeltProfile(roots, denyNetwork, masks)`** → take a **policy** (`denyRead` regions,
  `allowRead`, `denyWrite`, `denyProcessInfo`) and emit SBPL **in precedence order** (later rules
  win, matching `srt`'s "a more specific deny wins"):
  1. `(allow default)` (kept — avoids enumerating every system path);
  2. writes: `(deny file-write*)` + `(allow file-write* <roots+tmp>)` *(shipped)* + `(deny
     file-write* <denyWrite>)`;
  3. **reads (new)**: `(deny file-read* (subpath <denyRead, e.g. "~", "/etc">))` + `(allow
     file-read* (subpath <allowRead: workingDir + system>))` + `(deny file-read* <more-specific,
     e.g. "**/.env">)`;
  4. **process introspection (new, FR4)** `(deny process-info*)` `(deny sysctl-read)` — **per-command
     profile only**; the whole-process profile must **not** copy this (agentd introspects its own
     children) — there, cross-session isolation is by user/session, not by denying all
     `process-info`;
  5. network: per-command `(deny network* (remote ip))` *(shipped, FR3 3a; AF_UNIX stays on the file policy)*; whole-process keeps it (it
     serves listeners and calls the model). A domain allow-list (3b) is a deferred, optional
     layer (§0), not part of the native core.
- **`seatbeltProfile` / `seatbeltProfileWith`** → thread the policy. **`seatbeltProcessProfile`**
  → the whole-process variant (no FR4 hard-deny; **must** `allow file-read* (subpath <its own
  config dirs>)`, FR12).
- **Home denylist** — the old `readmask.go` list was removed in S5; the standard read policy denies
  the user's home (and its parent) as a region, so the specific credential directories are covered
  without a separate denylist.

### 12.2 Linux `bwrap` (`bwrap*.go`)
- Reads: bind **only** the allow-listed paths (`--ro-bind` / `--bind`) — FR1 is **structural**
  (nothing else exists in the namespace), no profile to get wrong. `read: off` uses
  `--ro-bind / /`; `write: off` (only with `read: off`, validation-enforced) adds `--bind / /`.
- Network: `--unshare-net` (FR3 3a, hard no-egress); a domain allow-list (3b) is a deferred,
  optional layer (§0), not part of the native core.
- Process introspection: PID namespace (+ seccomp denying `ptrace`/`process_vm_readv`).
- Writes: writable `--bind` allow-list (shipped).

### 12.3 Config (`cmd/agentd/config`)
- Add `sandbox.filesystem.{denyRead,allowRead,allowWrite,denyWrite}`, `sandbox.network`,
  `sandbox.unixSockets`, `sandbox.allowProcessInspection`, `sandbox.scrubEnv` (§6).
- **Validation (FR10)**: a rule the platform cannot honor (a write glob on Linux) ⇒ **startup
  error**; unknown keys ⇒ error; **never a silent fallback to allow-all**.

### 12.4 In-process tools (FR7)
- The `read` tool is in-process → attach a **read guard**: promote `ClientOptions.FileWriteGuard`
  (§7, done) to a **single path policy (read + write)**, or reuse the same policy object inside
  `read`.
- `grep` runs via `ripgrep` (a **command**, already covered by the sandbox); `write`/`edit`/
  `apply_patch` keep `FileWriteGuard`.

### 12.5 Tests (extends `DESIGN-ISOLATION.md` Part 6 / §9)
- Unit: the profile string is generated correctly for a policy (read deny/allow order,
  `process-info` deny).
- Integration (Linux/macOS CI): the §9 items 1–9.
- **Regression**: `bash` **and** the `read` tool are denied the **same** path (closes the FR7
  seam).

### 12.6 FR9 violation events (implemented)
- **Event**: `session.next.sandbox.denied` (durable, aggregate `sessionID`), carrying
  `action`/`path`/`host`/`rule`/`surface`/`certainty` plus the assistant message and call id.
  It is mirrored as `agent.SandboxDenied`, so `Client.On` and SSE consumers see it.
- **In-process guard (authoritative)**: `PathGuard` returns `*core.PolicyDeniedError`; the tool
  registry reports it through the execution's `ReportViolation` hook; the failure message says
  "blocked by sandbox (<reason>)" so the model stops retrying. `certainty: enforced`.
- **Command surface — heuristic (default)**: the wrapper scans a non-zero command's
  stdout/stderr for `Operation not permitted`, `Permission denied`, `Read-only file system`,
  `Network is unreachable`, `No route to host` and `Access is denied`, extracts a path when
  present, and reports `certainty: suspected`. It is intentionally **not** authoritative: bwrap
  allow-only views return `ENOENT` (indistinguishable from a missing path) and macOS exposes no
  per-process denial trace without elevated log access.
- **Command surface — precise (Linux, opt-in)**: `violations.mode: precise` runs the command
  under `strace -f -e trace=openat,connect` (openat-only keeps older strace versions working);
  failed `EACCES`/`EPERM`/`EROFS`/`ENETUNREACH` syscalls become `certainty: enforced` violations
  with the syscall's path or address. Startup fails if `strace` is missing. All trace lines are
  stripped from the tool output. macOS/Windows reject `precise` at startup.
- **grep/glob**: ripgrep runs through the same process seam, so its denied reads are reported
  through the same hook (`read` additionally has the authoritative in-process guard).
- **Model feedback**: `violations.reportToModel: true` (default) appends
  `[sandbox] blocked <action> <path>` to the command output (stderr for stdout-parsing tools);
  the file tools already include the reason in their failure message.
- **Debug/audit**: `violations.mode: off` disables the command-side scan; the in-process guard
  events are always emitted (they are authoritative).
