# Sandbox verification runbook (Linux / Windows)

Companion to `docs/REQUIREMENTS-SANDBOX.md` §9. macOS is verified in this
repository (read/write confinement, UNIX-socket allowlist, per-run private temp,
violation events); Linux and Windows need a host/CI pass. This document is the
checklist to run there, the expected results, and the likely fix when an item
fails.

Record every run:

| Field | Value |
|---|---|
| OS / version | |
| Arch | |
| Go version | |
| Kernel (Linux) / Windows build | |
| `bwrap --version` / `strace --version` | |
| Date / commit | |
| Overall result | |

## 0. Prerequisites

**Linux**
- `bwrap` installed and user namespaces enabled: `unshare -Ur true` succeeds.
- `strace` installed only when `sandbox.violations.mode: precise` is tested.
- `rg` on `PATH` for the grep/glob tools (the live checks use them).

**Windows**
- AppContainer profiles can be created by the running account.
- `icacls` and `CheckNetIsolation.exe` are available.
- Whole-process wrap needs a loopback exemption (the wrapper attempts it).

## 1. Build and static gates (both platforms)

```sh
gofmt -l cmd pkg internal examples        # expected: no output
go build ./...
go vet ./...
go test ./... -short
make fmt-check
make check-boundary
GOOS=linux   GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go test -c -o /dev/null ./cmd/agentd/sandbox
GOOS=windows GOARCH=amd64 go test -c -o /dev/null ./cmd/agentd/sandbox
```

> On Windows without `make`, run the underlying `go` commands and
> `go run testdata/boundarycheck.go` (plus the external-module build) directly.

## 2. Automated sandbox tests (run on the host)

```sh
go test ./cmd/agentd/sandbox/ -count=1 -v
go test ./cmd/agentd/ -run 'TestResolveSandbox|TestSandboxGuards' -count=1 -v
go test ./cmd/agentd/config/ -run 'Sandbox|Peer|Project' -count=1 -v
go test ./pkg/core/tool/ -run 'TestRead_GuardDenies' -count=1 -v
```

Expected:

| Test | Expected |
|---|---|
| `TestRequirementsReadWriteEnv` | read/write inside the workdir work; an outside write is denied and reported; a secret env var is scrubbed |
| `TestRequirementsPrivateTemp` | `TMPDIR` is under `TempRoot`; the command can read/write it; the run dir is removed afterwards |
| `TestPreciseViolationPlatform` | Linux: skipped (tested via strace); Windows: **rejects** `precise` |
| `TestReportingProcess*` | heuristic and precise detection, trace stripping, `reportToModel` note on stderr |
| `TestSocketAllowlist` | an allow-listed socket is readable/writable through the guard; an unlisted one is denied |
| `TestValidateSandboxPolicy` etc. | unsupported/disabled combinations fail validation |
| `TestValidateSandboxPlatformOff` | Windows rejects read/write `off` and `processInspection: allow`; Linux rejects `write: off` with `read: allow` |
| `TestSandboxCustomDefaults` | `custom` starts from read/write/network off and socket allowAll |
| Linux `TestBwrapWriteOffWithReadOff` | both off binds `/` read-write |
| `TestSandboxedUsesPrivateTemp` | each run gets a temp under `TempRoot`, `TMPDIR` points at it, and it is removed |
| Linux `TestBwrap*` | read/write/args/precise/socket arguments are correct |
| Windows `TestAppContainer*` | the wrapper is registered; writes inside the run dir work, outside is denied |

> Live warnings: `TestBwrap*` skips when `bwrap` is missing; the AppContainer test
> requires a real Windows host.

## 3. Live acceptance (§9) with a model

Use `cmd/agentd` with a model (e.g. vLLM) and a sandbox enabled. Minimal server:

```yaml
models:
  - id: test
    model: <wire model name>
    protocol: openai-compatible-chat
    modelBaseURL: http://127.0.0.1:8000/v1
    credentials: { apiKey: EMPTY }
servers:
  - name: sbx
    kind: http
    listen: 127.0.0.1:8787
    root: /tmp/agentd-sbx
    workingDirectory: /tmp/agentd-work
    agents: [build]
    sandbox:
      process: bwrap            # linux; use appcontainer on windows
      failurePolicy: fail-closed
      filesystem: { read: allow, write: allow }
      network: { mode: no-egress }
      unixSockets: { mode: inherit }
      violations: { mode: heuristic, reportToModel: true }
```

Create a session (`POST /api/session {"agent":"build"}`), subscribe to
`GET /api/session/{id}/event`, and prompt the agent to run each scenario. Record
the tool output and the `session.next.sandbox.denied` events (`action`, `path`,
`surface`, `certainty`).

| # | Scenario | Expected |
|---|---|---|
| 1 | `cat /etc/passwd`, `cat ~/.ssh/id_rsa`, `cat <agentd config>` via `bash` | denied (not "file not found"); a violation event |
| 2 | append to the agentd config; write under the workdir | outside denied, inside succeeds |
| 3 | `curl`/`getent` a public host | no egress (`no-egress`) |
| 4 | `env` in a command; `ps eww <agentd pid>` / `sysctl` (Linux) or the Windows equivalent | no secret env vars; cannot read agentd's env |
| 5 | `read` tool on `/etc/passwd` and on a workdir file | outside denied + `enforced` event; inside works |
| 6 | `echo hi > "$TMPDIR/x" && cat "$TMPDIR/x"` | works; the temp is under `{Root}/data/sandbox-tmp`; removed afterwards |
| 7 | host temp: write a file/socket under the host temp, then try to reach it from a command | not visible (private temp) |
| 8 | `unixSockets.mode: allowlist` with a test socket | reachable; an unlisted socket is not |
| 9 | whole-process `wrap` (`bwrap` on Linux, `appcontainer` on Windows) | agentd starts and serves; scenarios 1–8 still hold |
| 10 | remove/rename the wrapper binary | fail-closed refuses to start; `fail-open` warns and keeps the in-process guards |
| 11 | invalid config: `network.mode: allowlist`, `allowedDomains`, a Linux deny glob, `precise` on Windows, policy fields with `process: none` | startup error (no silent fallback) |
| 12 | no `sandbox` block at all | commands run unsandboxed; an info line is logged; no `{Root}/data/sandbox-tmp`; no guards |
| 13 | `read: off` / `write: off` (macOS/Linux) | macOS: no read/write rules; Linux: `read: off` = `--ro-bind / /`, `write: off` requires `read: off` and binds `/` rw |
| 14 | Windows `read/write: off`, `processInspection: allow`, and `network: mode: off` | the first three are startup errors; `network: off` grants `internetClientServer` (verify egress) |

Also check the events on the SSE stream / `Client.On`:
- `enforced` for the in-process guard and Linux precise;
- `suspected` for the heuristic stderr scan;
- `reportToModel` adds `[sandbox] blocked ...` to the command output (stderr for
  stdout-parsing tools such as ripgrep).

## 4. Platform-specific gotchas to record

**Linux**
- `bwrap` missing: fail-closed must refuse; fail-open must warn.
- `strace` missing with `violations.mode: precise`: startup error.
- bwrap's private `/tmp` must not be overwritten by a host temp bind; verify the
  host temp is not visible.
- `--unshare-pid` + `strace` (precise): record whether tracing works.

**Windows**
- AppContainer profile creation and loopback exemption.
- Temp ACL/traverse: if `TestRequirementsPrivateTemp` fails with access denied,
  record it. The intended fallback is to place the per-run temp under the
  working directory on Windows (the workdir grant is already known to work).
- Paths with spaces: the tests quote the outside path; manual checks should too.
- Whole-process `appcontainer`: `internetClientServer` + loopback exemption.
- Per-command `network: off` now adds `internetClientServer` and a best-effort loopback exemption;
  verify egress and loopback. `read/write: off` and `processInspection: allow` are rejected.

## 5. Conditional fixes after a failure

| Failure | Likely fix |
|---|---|
| Linux cannot read system paths | Add the missing path to `platformReadRoots` |
| Linux command sees the host temp / sockets | Check `--tmpfs /tmp` and the temp binds; the host temp must not be bound |
| Linux `precise` cannot trace | Move `strace` outside bwrap, or adjust `--unshare-pid`/the trace set |
| Linux `write: off + read: off` still denies writes | Check that `--bind / /` comes after the read-only/tmpfs mounts |
| Windows temp access denied | Use a per-run temp under the working directory (platform-specific base) and/or grant traverse on the missing ancestor |
| Windows `network: off` still has no egress | Check the `internetClientServer` capability is attached in `networkSecurity`; loopback needs the exemption |
| Windows write outside the run dir succeeds | Check the `icacls` grants and the policy's write roots |
| Violation events missing | Check `RunOptions.OnViolation` → `ctx.ReportViolation` → the session event; for ripgrep check the `OnViolation` pass-through |
| Disabled sandbox still confines | Check `Enabled()` and the `Profile: off` early return |

## 6. Results

| Item | Linux | Windows | Notes / commit |
|---|---|---|---|
| Build + static gates | | | |
| Automated sandbox tests | | | |
| Read/write confinement | | | |
| Network no-egress | | | |
| Process introspection / env | | | |
| Private temp | | | |
| UNIX sockets | | | |
| Precise violations (Linux) | | | |
| Violation events + reportToModel | | | |
| Whole-process wrap | | | |
| fail-closed / fail-open | | | |
| Validation (no silent downgrade) | | | |
| `off` semantics (read/write/network/processInspection) | | | |
| `profile: custom` defaults | | | |
| Windows `network: off` egress | | | |
| Default (sandbox off) path | | | |
