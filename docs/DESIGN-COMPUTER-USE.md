# go-agent-sdk computer-use agents — design

> Status: **v2 draft**. This document designs agents that operate **web, Android and PC**
> applications (Windows, Linux, macOS) and super-app programs (WeChat/Alipay mini-programs and
> official accounts). The **MVP is macOS desktop 企业微信 (WeCom) only, read-only**; personal
> WeChat and write/send are **post-MVP**. Later phases extend the same model to web, Android and
> other PC OSes.
>
> **v2 thesis:** app control is **per app × platform × build**. There is no universal chat or UI
> adapter. Each app/platform is enabled through a two-stage pipeline: (1) generate a versioned
> **App Control Model** from runtime evidence with developer review; (2) compile that model into
> **capabilities and an adapter runtime** the agent can call. Maintenance is a first-class product
> requirement.
>
> Relationship: computer use has **no counterpart** in opencode TS. It is a Go-specific
> capability; the TS porting rules do not apply. It is a sub-volume of `docs/DESIGN.md` and uses
> the existing `Client` / tools / `task` / permission / A2A seams without changing the core loop.

**Document map.**

| Part | Sections | Question answered |
|---|---|---|
| Thesis and system model | 1–2 | Why per-app/per-platform? What are the layers? |
| Stage 1: App Control Model | 3 | How do we discover, model and version an app? |
| Stage 2: Capability compilation and runtime | 4 | How do we compile the model and implement agent capabilities? |
| Platform frameworks | 5 | What does each OS provide? |
| Agent integration | 6 | How does the agent call capabilities, and how are apps/tools configured? |
| MVP | 7 | WeCom first: scope, plan, acceptance |
| Safety, testing, operations | 8–10 | How is it safe, tested and maintained? |
| Planning | 11–14, appendices | Roadmap, risks, decisions, open items, examples |

**Glossary.**

| Term | Meaning |
|---|---|
| **ACM / App Control Model** | Versioned model of one app × platform × build/UI hash: interactions, identity, capabilities, coverage, fixtures, provenance. |
| **Bundle** | The machine-readable ACM handoff (`apps/<app>/<platform>/<build>/<variant>/`). |
| **Capability ID** | Stable semantic operation ID (`chat.find`); used by manifest, policy and presets. |
| **Tool name** | Agent-facing projection of a capability (`chat_find`). |
| **Pack** | Optional capability family (`core`, `workspace`, `channel`, `thread`, `message`, `attachment`, `file`, `unread`, `send`). |
| **Adapter** | Per app × platform code implementing capabilities on the platform framework. |
| **Controller** | Narrow platform driver surface (`observe/act/screenshot/OCR/close`). |
| **Profile** | UI data for an adapter (roles/labels/bounds/clips/variants); not behavior. |
| **Platform framework** | Per-OS mechanics (AX/UIA/AT-SPI/CDP/ADB, input, capture, permissions). |
| **Capability runtime** | Stage 2 compilation (2a), adapter implementation (2b) and execution (2c). |
| **Host** | agentd/custom Go agent/CLI that owns config, policy, registry, sessions and exposure. |
| **Model registry** | Registry of supported app/platform/build/UI-hash models and lifecycle states. |
| **Design source** | Optional design input (Figma/Penpot/files/screenshots/MCP); hypotheses only. |
| **Capability card** | Agent-facing description: summary, when-to-use/not, schema, examples, errors. |
| **HITL** | Human-in-the-loop confirm/input/takeover with routing, pending store and audit. |
| **Drift** | Design/map/runtime or registry/runtime mismatch requiring reconciliation. |
| **Postcondition** | Checkable success condition for a capability (identity, content, boundary, send state). |
| **Effect-verified** | A write outcome whose effect contract was confirmed at evidence tier 1/2; distinct from a registry/model `verified` state. |

## 0. Change log

| Date | Change |
|---|---|
| 2026-10-18 | **v2 restructure**: two-stage app enablement; the App Control Model is keyed by app × platform × build/UI hash; per-app adapters on per-platform control frameworks; developer-in-the-loop gates; continuous maintenance. |
| 2026-10-18 | Stage 1 artifacts: dossier, interaction map, UI map, identity rules, capability manifest, coverage, scenarios, fixtures, drift, provenance. |
| 2026-10-18 | Stage 2: capability compiler + adapter runtime; semantic tools are capability-filtered; the model never sees AX ids, coordinates or OCR clips for normal operation. |
| 2026-10-18 | No universal chat adapter: WeCom/WeChat are separate adapters; platform mechanics are shared only where proven. |
| 2026-10-18 | Add the discovery harness design (snapshot/probe/diff/fixtures/coverage/drift) and the developer review gates G0–G7. |
| 2026-10-18 | Make maintenance first-class: model registry, compatibility policy, drift/re-discovery workflow, canary E2E, support window/SLA, rollout/rollback. |
| 2026-10-18 | Live WeCom findings: sidebar search is an `AXTextField`; the chat list is a virtualized `AXTable`; a selected row identifies the current chat; the message pane is custom-rendered and OCR-first; in-chat search/back are still unknown. |
| 2026-10-18 | Computer use remains an optional capability package + preset; no core-loop changes; Guard/Policy/Audit stay the execution choke point. |
| 2026-10-18 | MVP = macOS WeCom reference adapter, read-only by default; WeChat is a separate vision/OCR adapter; send requires permission + HITL. |
| 2026-10-18 | Earlier decisions retained: access-source/execution-mode routing, explicit permissions, JIT/HITL, QR login, session vault, playbooks, DeviceProvider/lease, and the platform matrix. |
| 2026-10-18 | Review fixes: split Stage 2 into 2a compile / 2b adapter implementation / 2c runtime; add Appendix D types; mark planned packages; require bundle schema/validation; tighten compatibility; align C23 with the WeCom-first MVP; add gate→test→metric matrix and semantic policy example. |
| 2026-10-18 | Clarify non-goals and write scope: no security/permission bypass; no universal/zero-maintenance support; read-only MVP with send/delete/forward/bulk export out of scope; P3 is post-MVP write enablement. |
| 2026-10-18 | Add §6.5 app tool reference: routing (`app_list`/`app_delegate`/`app_status`), semantic tools (`chat_*`), parameters, responses, error codes, post-MVP `chat_send`, and primitive fallback. |
| 2026-10-18 | Correct the tool-layer model: the platform-agnostic contract is the capability shape, not `chat_*`; semantic tools are a common core + optional capability packs (workspace/channel/thread/message/search/file); manifest/policy drive host-side projection. |
| 2026-10-18 | Add §6.6 configuration model: host inventory → model registry → policy → agent preset → session projection; formula `verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role`; read-only vs assistant vs multi-app presets; no model-controlled expansion. |
| 2026-10-18 | Clarify the Controller contract: platform vs semantic capabilities, optional Focus/Identity/Health/ObserveWith extensions, stale observation, activation before input, screenshot/OCR coordinate/format, typed errors, Close semantics. |
| 2026-10-18 | Review/fix §2.2: bundle path keyed by UI-hash/variant; add `model.yaml`; separate 2a/2b/2c; generated projections pinned to bundle/generator; AppIdentity handoff; tool exposure formula; verification levels. |
| 2026-10-18 | Review/fix §2.3–2.6: platform matrix split into observe/act/capture/placement/fallback; capability-plan grant model; helper/MCP trust boundaries and invariants; host responsibilities/config/identity projection; external MCP opaque-tool and server-projection rules. |
| 2026-10-18 | Expand §3.0: generation architecture, step-by-step I/O table, draft-map synthesis, probe planner, capability synthesis, operational semantics (why ACM controls the app), automation boundary, validation gates, and a concrete WeCom `chat_open` end-to-end example. |
| 2026-10-18 | Rewrite §4: runtime request flow; core adapter + capability packs; hierarchical state model; core/pack operation contracts; 2a input/output/versioning; 2b package layout; 2c verification/retry/error handling; pack-aware metrics. |
| 2026-10-18 | Rewrite §5 with a common platform contract: observe/act/capture/OCR/focus/launch/permissions/placement/health/identity/errors/fallback/tests; detail macOS/Windows/Linux/Android/Web; add OCR/focus mapping, version/hard-stop matrix, and condense chrome-devtools-mcp to a reference. |
| 2026-10-18 | Fix §6 consistency: capability-ID/tool-name/policy/preset mapping; no-match/boundary/low-confidence as results/warnings; unified search scope; EntityRef/FileRef pack types; playbook semantics; task lifecycle/app_cancel; CapabilityCard and tool-selection metrics; login/session state machine; config schema/version. |
| 2026-10-18 | Rewrite §7 MVP: WeCom-only/read-only, fixed build/UI-hash/account/locale/window; explicit unknowns and gates; Stage 1 commands/artifacts/exit; Stage 2 core-only adapter; acceptance + budgets/privacy/tool-selection; milestones with exit criteria; rollout/rollback. |
| 2026-10-18 | Rewrite §8–§11: capability/plan permissions and audit; expanded threat/data/HITL; test matrix + metric definitions + gates; operations runbook (registry/canary/rollback/incidents/security/governance/DR); roadmap aligned with §7 phases and exit criteria. |
| 2026-10-18 | Fix §12–§13: risk table with impact/likelihood/mitigation/owner and missing categories; decisions aligned with capability/tool mapping, playbooks, read-only MVP, fallback; add C33–C40 and Scope column. |
| 2026-10-18 | Rewrite §14 open items: type/scope/owner/blocks/exit columns; split WeCom unknowns (scroll/leave/search/identity/taxonomy/modals/build matrix); split WeChat post-MVP; add tool projection/config/registry/HITL/data/exposure/pack items; phase blocking summary. |
| 2026-10-18 | Holistic review fixes: header MVP alignment; canonical phases P0–P8 (P2.5/P4/P3/P8); read-only vs post-MVP write policy split; glossary; Go SDK host sketch; traceability matrix; type references; threshold approval. |
| 2026-10-18 | Add research-driven mechanisms: effect/read-back outcomes; window-scoped/background automation; bounded observation/find; human-demo inputs; runtime repair/admission; effect fault injection; hybrid GUI+API; secret substitution/idle TTL; optional planner/reflector; decisions C41–C45 and open items O23–O27. |
| 2026-10-19 | OSS deep-dive (OpenAdapt, Peekaboo, Playwright MCP, Stagehand, UFO): add verification tiers + three-valued effect verdicts, exact evidence coverage/no-effect proof, observation truncation semantics, tool kind/group metadata, secret redaction vs substitution, the repair/admission lifecycle, and Appendix E. |
| 2026-10-19 | Review pass: package-safe effect/outcome type names; observed/absence state; oracle/verifier isolation and idempotent replay; clarified verdict, count and collateral-loss semantics; added `delivery_indeterminate`; fixed metric denominators and tool metadata; aligned O27 with the post-MVP write phase; clarified canary, read-only vs write admission and signer/revocation wording. |

## 1. Thesis and scope

**Goal.** Let an SDK agent observe and operate apps on any platform, so information and actions
locked in vertical apps become usable for automated decisions.

**What v2 changes.**

- The unit of design is **app × platform × build**, not a generic “chat app” or “desktop app”.
- The unit of delivery is a versioned **App Control Model** plus a **capability runtime**.
- The agent calls **semantic capabilities** (`chat_find`, `chat_open`, `chat_read`, `chat_scroll`,
  `chat_leave`) with business inputs (`query`, `ChatRef`, `cursor`) and structured outputs
  (`messages`, `AtTop`, `confidence`, typed errors). It does **not** call `click`/`type`/`scroll`,
  pass element ids (`e88`), choose coordinates, or pick OCR clips for normal app operation.
- The boundary is explicit. The table is about **visibility and usage**, not a conversion pipeline:

  | Surface | Examples | Visible to the agent? | Used by the adapter? |
  |---|---|---|---|
  | Semantic capability | `chat_find`, `chat_open`, `chat_read`, `chat_scroll` | yes (calls) | yes (implements) |
  | Structured state/result | `ChatRef`, `messages`, `cursor`, `AtTop`, `confidence`, typed errors | yes (receives) | yes (produces) |
  | UI primitives | `click`, `type`, `key`, `scroll`, `observe` | fallback only, policy-gated | yes (normal implementation) |
  | UI handles/geometry | `e88`, AX/DOM paths, x/y, OCR `Rect`, scroll container | no | yes (internal) |

  The agent calls semantic capabilities and receives structured results. The adapter implements
  each capability using UI primitives and handles internally: it finds the search field, types,
  waits, selects the row, OCRs the pane, scrolls, dedupes and verifies. UI handles/geometry never
  cross the adapter boundary; UI primitives are fallback-only **for the agent**, not for the
  adapter. The model expresses intent; the adapter owns the mechanics.
- Runtime evidence is the ground truth for **control behavior** (what actually happens when the
  app is observed/clicked/typed/scrolled/searched). Design artifacts, prose and recordings are
  optional hypothesis sources for **flows and coverage**; they are reconciled, not authoritative.
- **Design sources are optional, not just Figma.** Stage 1 is runtime-first by default: it can
  generate the model from goals, a test account, live AX/DOM/OCR probes, menus, shortcuts,
  deeplinks and known-answer data alone. Figma, Penpot, Excalidraw/tldraw, exported files,
  screenshots/recordings, developer walkthroughs and MCP/REST design servers are optional **design
  sources**. They seed hypotheses and coverage, never block model generation, and are never
  authoritative for control behavior. Every design-derived entry carries provenance/confidence and
  must be reconciled with runtime evidence and developer review; mismatches become `drift`. MCP is
  an access mechanism, not the model.
- Developers are in the loop where they matter: goals, semantics, identity rules, scope, risk and
  sign-off. The AI does discovery, modeling, implementation and regression support.
- **Maintenance is first-class.** A model that cannot be maintained should not be shipped as a
  supported capability. “Maintainable” means: a named owner per app × platform; build/UI-hash
  detection; drift reports and a re-discovery path; fixtures/contract tests and canary E2E; a
  support window/SLA; rollout/rollback; and a maintenance budget. Without these, an adapter may
  ship only as experimental/best-effort, or not at all.

**Safe baseline.** Imitate a human: observe the UI, act on it, verify the result, never bypass
authentication or platform permissions. Prefer structured channels (API/DOM/AX/UIA/AT-SPI) and use
vision as the fallback.

**Access sources, execution modes and escalation.** These are independent axes; do not model them
as one L0–L5 ladder. Route per step, not per task.

**Access source** (where the signal/action comes from):

| Source | Mechanism | Use when | Limits |
|---|---|---|---|
| **S0** | Official API / CLI / feed | the owner exposes it | may be data-only; not a substitute for desktop UI control; auth/scope |
| **S1** | Structured UI (DOM/AX/UIA/AT-SPI/CDP) | accessibility exists and the app model matches | UI drift; needs a versioned model; writes need policy |
| **S2** | Region OCR/UI detection | custom-rendered panes / missing AX | lossy; layout/scroll; confidence |
| **S3** | Full-screen vision | unknown/opaque screens | cost/latency/grounding |
| **S4** | Human takeover (HITL) | hard stops/high risk/low confidence | manual; audit |

**Execution mode** (how the step is carried out):

| Mode | Use when |
|---|---|
| Semantic capability/adapter | normal high-value flows; verified model |
| Verified playbook | repeated stable flow; cheap runtime |
| Exploratory agent | unsupported/long-tail; primitives + vision; lower reliability |
| HITL | hard stops/irreversible actions |

**Verification is independent of the action source.** Use the cheapest reliable signal: structured
state, expected text, OCR/anchor, or human confirmation. A structured action may need visual
verification; a visual action may be verified by a structured state change.

Routing rules:

- Prefer S0 for data and S1 for actions when they meet confidence and policy; use S2/S3 only when
  structure is missing; S4 for hard stops/high risk.
- Playbooks are an execution mode, not a fallback tier. They are authored/reviewed/versioned and may
  use S1 or S2 at runtime.
- Fall back on: missing access, model/version mismatch, low confidence, repeated no-op,
  verification failure, unsupported capability.
- Do not silently downgrade. Record source, execution mode, confidence, fallback reason and
  verification result.
- “Cheapest” means lowest total cost (latency/tokens/maintenance/risk) that still meets confidence
  and policy; it is not a fixed ordering.
- APIs may be data-only; they do not replace UI control for actions the API does not expose.

**Non-goals.**

- **No security bypass.** No protocol reverse engineering, code injection/process hooking,
  anti-bot/CAPTCHA evasion, credential/session theft, or QR/biometric/2FA/lock-screen bypass.
  Documented APIs/CDP/AX/UIA/AT-SPI and QR login via `QRConsumer`/manual are in scope; bypass is
  not.
- **No universal or zero-maintenance app support.** No single adapter or agent prompt operates
  every app; every app × platform × build needs its own model/adapter and owner. Reuse is limited
  to platform mechanics and proven helpers.
- **No write/destructive actions in the read-only MVP.** Send/delete/forward/pay and bulk export
  are out of scope for the MVP. When enabled later, they require explicit permission + HITL; bulk
  export remains a host policy/product decision, not a default capability.
- **No completeness/accuracy guarantee.** Coverage is goal-driven and explicit; unknown/unsupported
  states must be declared. We do not guarantee every UI event/state, 100% OCR/vision accuracy, or
  stability across unmaintained app builds; maintenance and metrics are required.
- **No platform-control bypass.** No bypassing TCC/UAC/portal/Android permissions, lock screen,
  secure input, or headless control where the platform requires a GUI session.

## 2. System model

### 2.1 Layers

```text
Host / session
  model bundle + capability manifest + policy
        │ projection + filtering
        │ (verified ∩ policy ∩ app/model)
        ▼
Agent / tool layer
  routing:  app_list / app_delegate / app_status
  semantic: per-app tools generated from the manifest
            core:  chat_find / chat_current / chat_open / chat_read /
                   chat_scroll / chat_search / chat_leave / ...
            packs: workspace_* / channel_* / thread_* / message_* /
                   mail_* / doc_* / file_* / ...
        │ capability calls
        ▼
Capability runtime (Stage 2)
  2a schemas/policy/skeleton | 2b adapter code | 2c execution
        │ guarded primitives
        ▼
App adapter (per app × platform)
  UI map, state machine, search/read/scroll/leave, postconditions
        │ platform primitives
        ▼
Platform control framework (one per OS)
  macOS / Windows / Linux / Android / Web
  observe, act, input, capture, OCR, focus, permissions, health
        │
        ▼
Target app
```

The **platform-agnostic contract** is the capability shape (inputs, outputs, pre/postconditions,
typed errors, `EntityRef`, `Page`, `Cursor`), not the literal `chat_*` names. `chat_*` is the
common core for chat-like apps; capability packs add domain/app-specific verbs. The manifest and
policy are consumed by the host/session projection, not by the agent directly.

- **Platform control framework** is per OS and owns permissions, windowing, focus, input, capture,
  OCR, accessibility and health. It does not know app semantics.
- **App adapter** is per app × platform. It owns the UI map, state machine, identity, semantic
  operations and verification.
- **Capability runtime** has three parts: (a) **compilation** of tool schemas, policy mappings,
  adapter skeleton and fixture/test wiring; (b) the **handwritten app adapter** (state machine,
  identity, search/read/scroll algorithms, verification, recovery); (c) **runtime execution** of
  capability calls.
- **Agent** composes advertised capabilities under a prompt. It never receives raw handles.

The platform framework boundary is a narrow primitive interface:

```go
type Controller interface {
    // Platform capabilities (AX/DOM/vision/OCR/coordinates/...), not app Capability.
    Capabilities() Capabilities
    Observe(ctx context.Context) (Observation, error)
    Act(ctx context.Context, action Action) error
    Screenshot(ctx context.Context, clip *Rect) ([]byte, error)
    Close() error
}

// Optional extensions; keep Controller narrow.
type OCRController interface {
    OCR(ctx context.Context, clip *Rect) (OCRResult, error)
}
type ActivationController interface {
    Focus(ctx context.Context) error
}
type IdentityProvider interface {
    Identity(ctx context.Context) (AppIdentity, error)
}
type HealthController interface {
    Health(ctx context.Context) (Health, error)
}
type ObserveWithOptions interface {
    ObserveWith(ctx context.Context, opts ObserveOptions) (Observation, error)
}
```

**Controller contract.**

- `Capabilities()` reports **platform** capabilities. It is not the semantic `Capability` set and
  must not be confused with the app model. If an API rename is possible, prefer
  `PlatformCapabilities()`.
- `Observe` produces a new observation and element namespace. `Observation.ID` binds subsequent
  actions; `Act` with a stale/missing `ObservationID` must return `ErrStaleObservation`. Element
  ids are not guaranteed to be stable across observations.
- Observations are bounded. `ObserveWithOptions` supports depth, max elements, boxes, image and a
  query/find; prefer find + local snapshot over a full-tree dump.
- A bounded observation must report **why** it is incomplete: depth, element/child count, deadline
  or incomplete AX read. The adapter must not infer focus, selection or completeness from a
  truncated tree; it must narrow the query, re-observe, or fail closed. A cache hit does not clear a
  truncation warning.
- A window-scoped observation binds `WindowRef` plus the owner PID, window ID, bounds and layer it
  was captured against (and process generation when the platform exposes it). If any of these
  changes, return `ErrStaleObservation`; never retarget the same action to a different window.
- `Act` executes exactly one primitive. A `Session` binds one app/account/window (`WindowRef`);
  AX semantic actions may run in the background, while global input (`CGEvent`) requires foreground
  activation and policy/consent. If activation fails, return an error instead of sending input to
  another app. Revalidate the pinned window identity before delivery.
- Background input distinguishes **not delivered** from **delivery indeterminate**. If any part of a
  click/key sequence was posted but the final state cannot be confirmed, return a typed
  indeterminate-delivery result and do not blind-retry; observe and reconcile first.
- A `Session` serializes `Act`; a `Controller` does not need to be safe for concurrent `Act`.
- `Screenshot` returns PNG bytes by default. `clip` is normalized `(0..1)` relative to the captured
  window, top-left origin; the controller converts to device pixels and handles Retina/multi-monitor.
  If capture is unavailable, return `ErrUnsupported`.
- `OCRController.OCR` uses the same clip coordinate space; `OCRResult` bounds are normalized to the
  captured image. Implementing `OCRController` implies `Capabilities().OCR == true`; if OCR is not
  available, `Session.OCR` returns `ErrUnsupported`.
- `Close` is idempotent and has no context parameter; implementations must not block indefinitely.
  After `Close`, other methods return `ErrControllerUnavailable`.
- Methods return typed errors where applicable: `ErrTCCDenied`, `ErrSecureInput`,
  `ErrAppNotRunning`, `ErrWindowNotFound`, `ErrStaleObservation`, `ErrDeliveryIndeterminate`,
  `ErrUnsupported`, `ErrTimeout`, `ErrControllerUnavailable`.
- The controller does not choose the app model, capability set or policy; identity/model selection
  happens in the host/session via `IdentityProvider` and the registry.
- Redaction, permission/audit and postcondition verification live in `Session`/`Guard`/the adapter,
  not in the controller.

The app adapter consumes this interface; the model does not. Platform frameworks may add focus,
health, window and event APIs, but the adapter-facing surface stays narrow and guarded. The
platform types above are defined in `pkg/apps`; app-semantic types are summarized in Appendix D.

### 2.2 Two-stage pipeline

**Stage 1 — App Control Model (ACM) generation.** Tools + developer discover, probe and author a
versioned bundle. Runtime-first is the default; design sources are optional hypotheses (§3.0).
Developer ground truth is required for semantics, identity and scope.

```text
apps/<app>/<platform>/<build>/<variant>/
  model.yaml            # schema_version, model_version, identity key
  dossier.yaml
  interaction-map.yaml
  ui-map.yaml
  identity.yaml
  capabilities.yaml
  coverage.yaml
  scenarios/
  fixtures/
  drift/
  provenance.yaml
```

The bundle key is `(app_id, platform, app_build, ui_hash, variants)`, where variants include
locale, theme, account role, layout and A/B cohort. The `<variant>` directory is the UI hash or a
named variant that includes it. A model for one platform/build/UI hash is not
valid for another unless it explicitly declares compatibility (§3.10). Platform mismatch means no
model; capability is `Unsupported` unless a configured fallback exists.

**Stage 2 — Capability compilation and runtime.** It consumes the Stage 1 bundle as the
authoritative semantic model, plus the platform control framework and handwritten adapter code. It
has three parts:

- **2a — compilation**: deterministic projections of the bundle — tool schemas/descriptions,
  policy-class metadata, adapter skeleton, fixture/test wiring and contract-test scaffolding.
  Generated outputs carry the bundle hash and generator version; they are not hand-edited.
- **2b — adapter implementation**: handwritten per app × platform. Shared wait/retry/OCR/scroll
  helpers are allowed; interaction semantics are not. This part implements the state machine,
  identity, search classification/ranking, read reconstruction, scroll/cursor/dedupe, verification
  and recovery.
- **2c — runtime**: tool call → schema validation → capability/model/policy check → adapter →
  guarded primitives → postcondition → structured result → audit/metrics.

Capability filtering/exposure happens at session time (2c), not in 2a. Drift detection belongs to
the maintenance workflow (§3.10), not to 2c.

**Handoff contract.**

- The Stage 2 generator/runtime never depends on design sources, prose or ad-hoc probe output.
  Handwritten adapter code may consult them for understanding, but the bundle remains authoritative.
- The bundle is selected by `AppIdentity`/the model registry before Stage 2 runs; no verified
  bundle means fail closed or a configured fallback.
- If the model is wrong, fix the bundle and re-run Stage 2. If the implementation is wrong, fix the
  adapter and update fixtures. If the platform/build changed, update the registry/profile. If the
  generator is wrong, fix the generator/templates and regenerate.
- Generated projections are pinned to the bundle hash + generator version and are not edited by
  hand; adapter semantics are code-reviewed and versioned.
- A capability is advertised only if:
  `verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role` (§6.6).
- Verification levels: contract/fixture replay at G4; live canary E2E at G5; only capabilities that
  pass their gates are registered as tools.
- “Arbitrary control under a prompt” means within the advertised, verified and authorized
  capability set, not arbitrary UI actions.
- Stage 1 needs developer ground truth (G0–G2); Stage 2 is engineering/automation plus adapter
  implementation (G3–G6).

### 2.3 Platform matrix

This matrix describes **platform capabilities**, not app capabilities. App capabilities come from
the App Control Model. `Placement` is deployment form (in-process/helper/sidecar/operator), not a
control API.

| Platform | Observe | Act | Capture | Placement | Permissions / hard stops | Fallback |
|---|---|---|---|---|---|---|
| macOS | AX (`AXUIElement`) | AX actions first; `CGEvent` for custom controls | ScreenCaptureKit (target) / `CGWindowListCreateImage` (current helper) | signed helper/operator | Accessibility + Screen Recording; Input Monitoring only for event taps/listening (verify per feature); secure input/lock screen hard stop | OCR/vision/HITL |
| Windows | UIA tree/patterns | UIA patterns first; `SendInput` fallback | Win32/UIA | sidecar first; native CGo later | UAC/secure desktop/elevated windows hard stop; DPI/multi-monitor/focus stealing | OCR/vision/HITL |
| Linux | AT-SPI2 over D-Bus (toolkit coverage varies) | AT-SPI actions; X11 XTest; Wayland via portal/libei/`ydotool`/`wtype` | X11 / PipeWire portal | in-process or sidecar | no universal Wayland input API; portal consent; no root/uinput default | OCR/vision/HITL |
| Android | AccessibilityService tree | a11y actions; ADB input fallback | MediaProjection/screencap | operator APK + host ADB | device/emulator permissions; foreground service; OEM differences; WebView/Canvas | OCR/vision/HITL |
| Web/Electron | DOM/accessibility/CDP/DevTools | CDP input; DOM focus/fill | CDP screenshot | in-process CDP controller | Chromium/Electron with remote debugging; iframes/shadow DOM; anti-automation/ToS | OCR/vision/HITL |

Platform mechanics are never assumed to be shared. App adapters may share capability contracts and
domain types, but not interaction implementations. A platform that lacks a structured API uses its
declared fallback; it does not silently claim the same capability as another platform.

### 2.4 Trust boundaries and invariants

```text
Untrusted model
  │ schema-validated capability call
  ▼
Capability runtime
  │ identity/model/capability check + target resolution
  ▼
Guarded Session: permission/HITL -> capability grant (bounded primitive plan) -> audit
  │ grant + plan
  ▼
App adapter (may execute only granted plan steps; every step audited)
  │ guarded primitives
  ▼
Platform driver: AX / UIA / AT-SPI / CDP / ADB
  │
  ▼
Untrusted app/content (data, not instructions)
```

**Grant model.** A capability-level grant covers a **bounded primitive plan** (capability + target
+ step list + bound observation/model). The adapter may execute only steps in that plan and cannot
expand the target or step list. If the target/observation changes, or a step needs a new target, it
must re-authorize. High-risk capabilities may require per-primitive grants via policy. Retries are
bounded and audited; a consumed grant is not silently reused.

**Trust boundaries.**

- Local helpers (macOS operator, Windows sidecar, Android operator APK) are privileged processes:
  signed where possible, least privilege, token-authenticated local IPC, no remote exposure.
- External MCP tools are opaque sources: allowlist by server/tool, validate arguments, redact
  outputs, do not expose raw handles, and do not assume they have postconditions.
- Host configuration (app inventory, model registry, policy, presets) is trusted; the model is not.
- App content is untrusted data, never instructions.

**Invariants.**

- The model cannot add apps, models, permissions, grants, target fields or capability packs; it
  proposes capability calls only.
- Denied calls never reach the platform driver.
- Raw handles, PIDs, AX/DOM nodes, coordinates and OCR clips are not exposed for normal operation.
  Normalized element ids/coordinates may appear only in the explicit `app_*` fallback path.
- Writes require explicit permission and HITL where configured.
- Every capability has a postcondition and a typed error surface.
- App content is untrusted; no cross-app/session data leakage (subagents are isolated).
- No silent fallback/downgrade: record source, execution mode, confidence and fallback reason.
- A stale model, wrong entity or wrong account stops the operation; do not read/write after
  detection.
- Actions are serialized per app/account; no concurrent input to the same session.
- Credentials, QR content, tokens and secrets never enter the model context or logs.
- External MCP tools and MCP tool descriptions are untrusted; Guard/Policy remain in-process and
  are not replaced by MCP.

**Responsibility split.**

- Capability runtime: schema/identity/model/capability checks and target/plan construction.
- Session/Guard: permission, HITL, grant issuance, audit and the execution choke point.
- Adapter: executes the granted plan via guarded primitives; verifies the postcondition.
- Platform driver: performs the primitive action; it does not choose model/policy.

### 2.5 Hosts and exposure

The SDK is a library; there is no required daemon.

- **agentd**: optional reference host exposing HTTP/SSE and/or A2A; loads app inventory, model
  registry, policy, agent presets and servers.
- **Custom Go agent**: embeds the SDK and exposes its own API; most flexible.
- **CLI/test harness**: fake controllers and `DevBypass` for tests.

**Host responsibilities.**

| Responsibility | Required |
|---|---|
| app/account inventory (platform, account, model/policy refs) | yes |
| model registry + `AppIdentity` resolution before tools are exposed | yes |
| Guard/Policy/Audit initialization | yes |
| tool projection/filtering per §6.6 | yes |
| session lifecycle, app/account binding and action serialization | yes |
| HITL routing and operator ownership | if configured |
| exposure authentication/authorization (HTTP/SSE/A2A/MCP) | if exposed |
| fail closed when no verified model/policy exists | yes |

Rules:

- A host must resolve the app identity/model before registering semantic tools. No verified model
  means no semantic tools (or an explicit fallback under policy).
- One session is bound to one app/account; multi-app goals use host orchestration or subagents.
- The host owns configuration, credentials and policy references; the agent cannot change them.
- Hosts must not bypass Guard/Policy. `DevBypass` is test-only and audited.
- Remote/A2A/MCP exposures enforce policy on both sides; MCP is not a trust boundary.

HTTP/SSE, A2A and MCP are transports/adapters, not hosts. All hosts use the same Guard/Policy/Audit.

### 2.6 SDK-first, MCP as an edge

go-agent-sdk is the product. It is a Go library that owns:

- platform controllers and app adapters;
- the capability runtime and model registry;
- Guard/Policy/Audit/HITL;
- tool registration and agent integration.

MCP is an **optional edge**:

- `pkg/mcp/server` **(planned)** can expose registered capabilities as MCP tools. The current
  `pkg/mcp` package provides the client/adapter side, not a server package.
- `pkg/mcp` (client/adapter) can consume external MCP tools.
- An external MCP server is not the internal bus, not the capability runtime, and not the
  Guard/Policy path.

**External MCP tools are opaque sources, not verified capabilities.**

- Allowlist by server/tool; do not auto-register everything from `tools/list`.
- Validate arguments; redact outputs; never pass raw handles, PIDs or credentials through.
- Do not assume identity, postconditions or stable schemas. Unless wrapped by an adapter and
  verified, an external MCP tool is an exploratory/primitive source, not a supported capability.
- MCP tool descriptions are untrusted input (prompt injection); review/pin versions.
- Route calls through a policy proxy; they must not bypass in-process Guard/Policy.
- External MCP tools are not the core app-control path. Optional Web/Electron sources may use them
  only via a policy proxy and must be marked experimental.

**MCP server projection.**

- Expose only advertised capability tools, using the same
  `verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role` formula (§6.6).
- Require authentication/tokens; never expose raw UI handles, coordinates or OCR clips.
- Bind MCP schema versions to capability/model versions; refresh `tools/list` when the session
  model changes.
- MCP is a transport, not a security boundary; Guard/Policy/Audit stay in-process.

Rules:

- The in-process Go interfaces (`Controller`, `AppAdapter`, `Capability`, `Guard`) are the source of
  truth; MCP schemas are projections.
- The **core** control path must not require Node/Puppeteer/MCP. Optional external sources are
  allowed only under explicit policy and with a proxy/wrapper.
- External MCP tools are wrapped as an access/tool source and still go through Guard/Policy.
- MCP server/client is opt-in; SDK users and hosts can choose not to use it.
- MCP does not replace the App Control Model, versioning, verification or maintenance.

## 3. Stage 1: App Control Model

Stage 1 answers two questions:

1. **How do we obtain the app's interactions?**
2. **How do we turn them into capabilities the agent can call safely?**

### 3.0 How the App Control Model is generated

The App Control Model is **not** a converted Figma file and **not** a raw AX/DOM dump. It is a
reconciled, versioned model with provenance and verification status. Generation is a loop.

There are two seeding modes; they converge on the same probe/reconcile/author/review pipeline:

| Mode | Default? | Seeds |
|---|---|---|
| **Runtime-first** | **yes** | goals, test account, developer brief, live AX/DOM/OCR snapshots, menus, shortcuts, deeplinks, known-answer data |
| **Demo-seeded** | optional | human demonstration/recording/event trace or agent exploration trace; reconciled with probes |
| **Design-seeded** | optional | Figma/Penpot/Excalidraw/file exports/screenshots/recordings/MCP servers, in addition to the runtime-first inputs |

```text
runtime snapshots ─┐
goals + test acct ─┼─> draft interaction map ─> targeted probes ─> evidence
known answers ─────┤          ▲                                  │
design sources ────┘          └──────── reconcile/drift ◄─────────┘
                              │
                              ▼
              capability manifest + identity + coverage
                              │
                              ▼
              developer review (G1/G2) ─> bundle
```

1. **Seed hypotheses (design sources optional).** The default is runtime-first: start from goals, a
   test account, the developer brief and the app itself (AX/DOM/OCR snapshots, menus, shortcuts,
   deeplinks, known-answer data). If design sources exist — Figma/Penpot/Excalidraw, exported
   files, screenshots/recordings, walkthroughs, MCP/REST design servers — use them as additional
   hypotheses and coverage hints. Produce a draft interaction map with explicit unknowns. Design
   sources are never required, may be reverse-engineered, and may be stale.
2. **Extract surfaces and controls.** Run targeted AX/DOM traversal and OCR/screenshots. Record
   roles, labels, bounds, actions, states, menus, shortcuts and virtualization.
3. **Probe behavior.** Perform read-only actions on a test account. Capture before/after snapshots,
   transitions, timing, scroll behavior, search debounce and modal interference.
4. **Reconcile.** Runtime evidence decides **control behavior**. Design intent is kept as
   `design-intent`; mismatches become `drift`; unsafe/unprobeable items become `unknown`. Never
   “fix” the runtime to match a design artifact.
5. **Author capabilities.** Group interactions by user intent. Define input/output, pre/post,
   errors, policy class, adapter-internal steps and verification.
6. **Derive coverage and fixtures.** Map every in-scope goal to a path/capability/test; record
   fixtures and scenarios; list unsupported/out-of-scope.
7. **Developer review.** The app owner corrects semantics, identity/ambiguity rules, scope and
   risk; QA reviews coverage. Gates G1/G2.
8. **Publish.** Write the versioned bundle
   (`apps/<app>/<platform>/<build>/<variant>/`) and register it with lifecycle status and provenance.

Without design sources, step 2 extracts the first draft directly from the running app; steps 3–8
are unchanged. Design sources only change where the initial hypotheses come from, never how they
are verified.

Every entry carries a status:

| Status | Meaning |
|---|---|
| `design-intent` | from any design source/walkthrough; not yet observed |
| `hypothesis` | inferred from docs/snapshots; needs probing |
| `probed` | observed on a running app; not fully verified |
| `verified` | reproducible probe + postcondition + developer review |
| `drift` | design/map/runtime disagree; action pending |
| `unknown` | cannot safely probe or not yet discovered |

**What each source can decide.**

| Question | Design source | Runtime probe | Developer |
|---|---|---|---|
| What flows/states are intended? | strong hint | confirms what exists | final scope |
| What happens on click/type/scroll/search? | no | **yes** | confirms semantics |
| AX/DOM roles, labels, bounds, actions | approximate | **yes** | no |
| Identity/ambiguity rules | hint | evidence | **final decision** |
| Search result taxonomy | hint | **yes** | confirms meaning |
| Async/debounce/virtualization/timing | no | **yes** | hints |
| Supported builds/platforms | hint | build/UI hash + probe | **SLA/risk decision** |

This is why Stage 1 can use design sources as accelerators without treating them as truth: design
artifacts seed the map; runtime probes and developer review turn it into a model.

#### 3.0.1 Generation architecture

ACM generation is a tool-assisted pipeline, not a single “dump the AX tree” call:

```text
inputs
  goals, test account, developer brief
  AX/DOM/CDP/AT-SPI/UIA/Android a11y
  screenshots + OCR
  menus, shortcuts, deeplinks, URL schemes
  design sources (optional)
        │
        ▼
collectors + normalizers
  raw snapshots, metadata, redaction, UI hash
        │
        ▼
surface / control / entity extractor
  app-family templates + generic fallback
        │
        ▼
draft interaction map
  surfaces, controls, entities, states, transition candidates, unknowns
        │
        ▼
probe planner
  safe read-only actions, state reset, one variable at a time
        │
        ▼
probe runner + diff + state-graph builder
  evidence, stable transitions, timing, ambiguity
        │
        ▼
capability synthesizer
  intent grouping, steps, pre/post, errors, schemas, confidence
        │
        ▼
coverage + fixture generator
  goals × paths × capabilities × tests; snapshots/OCR/screenshots
        │
        ▼
validator + review gates (G1/G2)
  schema, referential integrity, reproducibility, dry-run
        │
        ▼
apps/<app>/<platform>/<build>/<variant>/ bundle
```

| Component | Input | Output | Automation |
|---|---|---|---|
| Collector | app/OS/design sources | raw snapshots + metadata | deterministic |
| Normalizer | raw snapshots | normalized tree, OCR, redaction, UI hash | deterministic |
| Extractor | normalized tree + templates | surfaces, controls, entities, states | deterministic + templates |
| Draft map builder | extracted facts | interaction-map candidates + unknowns | deterministic + LLM hints |
| Probe planner | draft map + goals | safe probe plan | rules + policy |
| Probe runner | probe plan | before/after evidence, timing | deterministic |
| Diff/state graph | probe evidence | stable transitions + ambiguity | deterministic |
| Capability synthesizer | transitions + domain template | capability manifest draft | templates + LLM hints |
| Coverage/fixture | goals + manifest | coverage matrix + fixtures | deterministic |
| Validator | bundle | pass/fail + diagnostics | deterministic |
| Reviewer | bundle + evidence | approved/rejected + corrections | human (G1/G2) |

**Step-by-step input/output.**

| Step | Input | Process | Output |
|---|---|---|---|
| 0. Prepare | app, test account, permissions, goals, optional design sources | launch/attach, record build/UI hash, permissions | dossier seed, goal list |
| 1. Capture | live app | AX/DOM/CDP/AT-SPI traversal, screenshots, OCR, menus/shortcuts/deeplinks | raw snapshots, OCR, menu/shortcut inventory |
| 2. Extract | snapshots | app-family templates + generic fallback: surfaces, controls, entities, states | extracted facts + confidence |
| 3. Draft map | extracted facts | candidate states/transitions, explicit unknowns | `interaction-map.yaml` draft |
| 4. Probe | draft map + safe policy | read-only actions, state reset, before/after capture | probe log, evidence, timing |
| 5. Reconcile | probe evidence | diff + state graph, filter noise, repeat unstable steps | stable transitions + pre/post conditions |
| 6. Synthesize | transitions + templates | intent grouping, steps, verify, errors, schema | `capabilities.yaml` draft |
| 7. Cover | goals + capabilities | goal × path × capability × test mapping | `coverage.yaml`, scenarios |
| 8. Fixture | snapshots + probes | record deterministic snapshots/OCR/screenshots | `fixtures/` |
| 9. Validate | bundle | schema, referential integrity, dry-run, replay | diagnostics + pass/fail |
| 10. Review | bundle + evidence | developer/QA review G1/G2 | verified bundle + registry entry |

The LLM may propose labels, intent groupings and capability names; **it does not decide control
semantics**. Extraction, probing, diffing and validation are deterministic and evidence-bearing.

#### 3.0.2 Draft interaction map synthesis

1. **Surface classification.** Walk top-level windows/split groups/scroll areas/tables/dialogs;
   use role, relative bounds, parent structure, scrollability and row patterns to classify
   navigation/list/detail/composer/dialog/menu.
2. **Control inventory.** Normalize nodes into controls with role, label, bounds, actions, state
   (enabled/visible/focused/selected) and clickable/editable/scrollable flags. For virtualized
   lists, identify the row template instead of enumerating every row.
3. **Entity extraction.** Use an app-family template (chat/mail/doc/channel); for chat, detect
   repeated rows with `name/preview/time` text; filter headers, group labels and empty rows. For
   custom-rendered panes, use OCR blocks with bounds and confidence.
4. **State identification.** Current state comes from selected rows, header/title, visible controls,
   focus, modals, loading indicators and empty/error markers.
5. **Transition candidates.** Any control with an action becomes a candidate; expected `from/to`
   and postcondition are hypotheses until probed.
6. **Confidence/provenance.** Every candidate records source, method, confidence and status
   (`design-intent`/`hypothesis`/`probed`/`verified`/`drift`/`unknown`).
7. **No template = generic fallback.** Without an app-family template, the extractor produces a
   generic control inventory and marks entity/state candidates low-confidence for developer review.

#### 3.0.3 Probe planner and evidence

The planner selects **read-only** probes that maximize coverage while minimizing risk:

- navigation: open/leave a list/detail, open/close a search panel;
- search: type a safe known query, confirm, clear, restore;
- read/scroll: page up/down, top/latest, boundary checks;
- controls: click only non-destructive navigation controls;
- never send/delete/pay/forward; never probe destructive menus.

Rules:

- One variable at a time; reset to a known state between probes.
- Serialize probes per app/account; never run two controllers on one account.
- Capture before/after snapshots, screenshots, OCR, selected elements, timing and failures.
- Repeat critical transitions (for example 3×) to detect non-determinism.
- If a transition cannot be probed safely, mark it `unknown`; do not infer control from a design
  source.
- Probe evidence includes build/UI hash and the exact action/target used.

#### 3.0.4 Capability synthesis

Capabilities are grouped by **user intent**, not by control:

| Intent | Typical source |
|---|---|
| find | search field, list rows, global search results |
| open | row/result selection, detail transition |
| current | selected row + header |
| read | visible pane, OCR blocks/AX rows |
| scroll | message/list scroll container, boundary |
| search | in-context search control |
| leave | close/back/clear selection |

For each capability the synthesizer records:

- input/output schema and domain types (`EntityRef`, `Page`, `Cursor`, ...);
- adapter-internal steps: `observe` → `resolve` → `act` → `wait` → `observe` → `verify`;
- postconditions derived from observed stable signals (header/selected row, content anchors, query
  echo, overlay closed);
- typed errors derived from probe failures (`not_found`, `ambiguous`, `not_switched`,
  `at_boundary`, `stale_observation`, `unsupported`);
- policy class and confidence;
- fixture references.

Low-confidence or unstable transitions do not become `verified` capabilities. The developer owns
semantic naming, identity/ambiguity rules and unsupported decisions.

#### 3.0.5 Why the ACM can operate the app

The ACM is an **executable contract**, not a description:

- **Locators are runtime-resolved.** The adapter resolves role/label/actions/bounds/path against
  the live platform tree at execution time; it does not cache element handles across observations.
- **Steps are executable primitives.** Each capability records the primitive sequence
  (`observe/act/wait/verify`) that produced the probed transition.
- **Postconditions are observed, not guessed.** `Current == ref`, header/selected match, content
  anchors, query echo and overlay closure come from probes and fixtures.
- **Identity is bound.** `EntityRef` is resolved to the live target; ambiguity fails closed.
- **Staleness is checked.** A changed observation/window/model invalidates the step; the adapter
  re-observes or returns `stale_observation`.
- **Verification is mandatory.** The capability is not advertised unless the postcondition passes
  contract/fixture replay (G4) and live canary E2E (G5).
- **Versioning is enforced.** The bundle is pinned to app/platform/build/UI hash; mismatch means
  no model or an explicit fallback.
- **Only verified capabilities are exposed.** Unsupported/unknown capabilities are omitted or
  return `unsupported`; the agent cannot make the adapter guess.

In short: the ACM controls the app because it contains **observed locators + probed transitions +
executable steps + checked postconditions + version identity**, and the adapter re-resolves and
verifies every step at runtime.

#### 3.0.6 Automation boundary

| Step | Deterministic tools | LLM-assisted | Developer |
|---|---|---|---|
| capture/normalize | yes | no | no |
| surface/control extraction | yes + templates | optional label hints | review |
| entity extraction | yes + templates | optional | review |
| identity candidates | yes | optional | **rules/decision** |
| probe planning | rules | optional | **safety scope** |
| transition inference | yes (diff) | optional | review |
| capability synthesis | templates | yes (intent/naming) | **semantics/postconditions** |
| coverage/fixtures | yes | no | review |
| schema/replay validation | yes | no | sign-off |
| unsupported/unknown | evidence-based | no | **decision** |

The LLM is a **proposal engine**, not the source of control truth. Any LLM-generated label or
capability must be backed by extracted facts, probe evidence and developer review.

#### 3.0.7 Generation validation

A bundle is accepted only if:

1. every artifact passes schema validation;
2. referential integrity holds: capability → interaction/state/transition → coverage → fixture;
3. each advertised capability has a postcondition, typed errors and a policy class;
4. critical transitions are reproducible across repeated probes;
5. fixtures replay deterministically;
6. a capability dry-run produces the expected postcondition;
7. live canary E2E passes for in-scope golden goals;
8. unknown/unsupported/out-of-scope lists are explicit;
9. reviewer sign-off (G1/G2) is recorded with provenance.

#### 3.0.8 Worked example: WeCom `chat_open`

**Input.** Goals from the developer:

```text
1. Find the chat "POE Website 接入登录互联".
2. Open it.
3. Read recent messages.
```

**Step 1 — snapshot.** Target the main window. The extractor sees:

```text
AXSplitGroup
  AXTextField (sidebar search)
  AXTable (chat list, virtualized)
    AXRow
      AXStaticText: "POE Website 接入登录互联"
      AXStaticText: "zhaofanfeng 请 guoniuzhang..."
      AXStaticText: "16:30"
message pane: custom-rendered, OCR required
```

**Step 2 — extract.**

```text
surface: sidebar/navigation
control: search_field (AXTextField, set_value/confirm)
control: chat_table (AXTable, select_row/scroll)
entity: chat {name, preview, time, selected}
entity: message {sender, time, text, direction}
state candidates: ChatList, SearchResults, Chat
```

**Step 3 — draft interaction map.**

```yaml
schema_version: 1
states: [ChatList, SearchResults, Chat]
transitions:
  - {from: ChatList, action: search, to: SearchResults, status: hypothesis}
  - {from: SearchResults, action: select_chat, to: Chat, status: hypothesis}
  - {from: Chat, action: leave, to: ChatList, status: hypothesis}
unknown:
  - in_chat_search
  - native_back
```

**Step 4 — probe.** State reset to ChatList; click a non-current row:

```text
before: ChatList, selected=another_chat
action: click "POE Website 接入登录互联"
after:  header="POE Website 接入登录互联", selected=POE Website
repeat: 3/3 same result
```

**Step 5 — reconcile.** The transition is stable:

```yaml
- from: SearchResults
  action: select_chat
  to: Chat
  post:
    - header == ref.name
    - selected_row == ref
  status: verified
```

**Step 6 — synthesize capability.**

```yaml
schema_version: 1
capabilities:
  - name: chat_open
    class: navigate
    input: {ref: ChatRef}
    output: {chat: ChatRef, state: AppState, confidence: number}
    pre: [ref.resolved, no_blocking_modal]
    post: [current_chat == ref, pane.loaded]
    steps:
      - observe: {expect: chat_list_or_search_results}
      - resolve: {ref: ref, target: row}
      - act: {kind: click, target: ref.row}
      - wait: {signal: header == ref.name, timeout: 3s}
      - observe: {expect: chat_loaded}
    verify:
      - selected_row == ref
      - header == ref.name
    errors: [not_found, not_switched, stale_observation, unsupported]
```

**Step 7 — coverage/fixtures.**

```yaml
coverage:
  goal: find and open the POE Website chat
  path: search -> result -> open
  capabilities: [chat_find, chat_open]
  tests: [fixture/open-poe-before, fixture/open-poe-after]
  status: verified
```

Fixtures record the before/after AX snapshots, screenshots, OCR and probe logs for this build/UI
hash.

**Step 8 — bundle.** The output is:

```text
apps/wecom/macos/5.0.10/default/
  model.yaml
  dossier.yaml
  interaction-map.yaml
  ui-map.yaml
  identity.yaml
  capabilities.yaml
  coverage.yaml
  scenarios/
  fixtures/
  provenance.yaml
```

Only after schema validation, fixture replay and review is `chat_open` marked `verified`.

**Stage 2 runtime.** The adapter resolves `ref` against the live row, executes guarded primitives,
re-observes, verifies `Current == ref`, and returns a structured result. A mismatch returns
`not_switched`/`stale_observation`; it never reads the wrong chat.

This is the concrete link from observation to an executable capability: **observed locators +
probed transition + synthesized steps + checked postcondition + versioned bundle**.

### 3.1 Artifacts and bundle

| Artifact | Contents | Owner |
|---|---|---|
| Model metadata | `schema_version`, `model_version`, identity key, generator compatibility | adapter author |
| App dossier | identity, builds, permissions, channels, account/session assumptions | adapter author |
| Interaction map | surfaces, controls, entities, states, transitions, shortcuts, async, failures | adapter author |
| UI map | locators/roles/bounds/clips, platform variants | adapter author |
| Identity rules | ChatRef strategy, confidence, ambiguity policy | adapter author + app owner |
| Capability manifest | agent verbs, schemas, pre/post, errors, policy class, versions | adapter author + host policy |
| Coverage matrix | user goals × paths × capabilities × status | adapter author + QA |
| Scenarios | golden flows and read-only probes | adapter author + QA |
| Fixtures | recorded AX/screenshot/OCR per build | adapter author + QA |
| Drift reports | design/map/runtime discrepancies with evidence | adapter author |
| Provenance | source, method, confidence per interaction | adapter author |

The machine-readable bundle is the handoff; the table is the human review surface.

Every artifact carries `schema_version`. `appmodel validate` enforces:

- required fields, enum values and schema versions;
- referential integrity: capability → interaction/state/transition → coverage → fixture;
- every advertised capability has a postcondition, typed errors and a policy class;
- every fixture references a supported build/UI hash;
- no `verified` status without probe evidence and reviewer sign-off.

Stage 2 rejects a bundle that fails validation. A bundle without schema validation is not a valid
handoff.

### 3.2 What “all interactions” means

“All interactions” is not the exhaustive event space. It is a **bounded, goal-driven coverage
claim** with four dimensions:

- **Surfaces**: windows, menus, toolbars, sidebars, lists, detail panes, composers, tabs, threads,
  context menus, dialogs, notifications.
- **States**: navigation, selection, search, filters, sort, pagination, compose, modal, loading,
  empty, error.
- **Data**: entities, identities, attributes, content types, relationships.
- **Behavior**: async loading, debounce, virtualization, incoming events, animations, retries,
  rate limits, focus stealing.

**Inventory checklist.** Mark each category present/absent/unknown:

- launch / attach / activate / quit;
- window, tab, space, account switch;
- menus, command palette, context menus, keyboard shortcuts;
- navigation: lists, folders, channels, threads, back/close/dismiss;
- lists: filters, sort, pagination, virtualized loading;
- search: global vs scoped, query syntax, result types, history;
- open/detail: selection, loading, empty/error state;
- read/scroll: visible page, history, jump-to-latest, anchors;
- compose/edit: draft, attachments, mentions, validation, send/confirm;
- destructive: delete/forward/withdraw/mass actions;
- dialogs: login, permissions, updates, confirmations, file pickers;
- notifications, unread badges, mentions;
- deeplinks/URL schemes, APIs/bots/CLI, AppleScript/local IPC.

Anything not probed is `unknown`; anything not in scope is declared out of scope. An app is never
“fully mapped” without that list.

### 3.3 Discovery inputs and tooling

Prefer structured channels; vision is evidence, not the first mapping.

| Channel | What to collect | Typical method |
|---|---|---|
| App/process | bundle ID, version/build, executable, signed identity, windows, account | process/window APIs |
| Accessibility | role/subrole/title/description/value/placeholder/bounds/actions/state, menus, notifications | AX tree dumps, targeted traversal, observers |
| DOM/web | DOM, labels, selectors, focus, virtualized lists, shortcuts | CDP/DevTools, accessibility tree |
| Vision | layout, text, icons, custom panes, OCR confidence | screenshots, OCR, region capture |
| Input | mouse/keyboard/IME, shortcuts, modifiers | probes, event logs, menu key equivalents |
| App channels | URL schemes/deeplinks, official API, bot/CLI, AppleScript, local IPC | app docs, `Info.plist`, menus |
| OS | TCC/UAC/portal permissions, Spaces, displays/Retina, secure input, lock state | helper health + manual checks |
| Data/content | exported files, clipboard (policy-gated), network (optional) | app features, policy-gated reads |
| Human demonstration | recorded/annotated task run, event trace | screen/event recorder, live walkthrough |
| Human | undocumented flows, quirks, version changes | walkthrough, recording, release notes |

**Design sources are optional.** They are reverse-engineered or hand-made, not specifications. They
can seed hypotheses and coverage; they never replace runtime probes.

| Design source | Examples | Access | Authority |
|---|---|---|---|
| Design app + API/MCP | Figma, Penpot | MCP/REST | design-intent |
| Whiteboard/flow | Excalidraw, tldraw | file/API/MCP | flow hint |
| File export | JSON/SVG/PNG | file adapter | design-intent |
| Screenshot/recording | images/video | vision/OCR | hypothesis |
| Developer walkthrough | live/recorded | notes | domain hint |

A `DesignSource` adapter normalizes these into a neutral `DesignModel`:

```go
type DesignSource interface {
    ListDocuments(ctx) ([]DesignDocument, error)
    GetDocument(ctx, id string) (DesignDocument, error)
    GetNode(ctx, docID, nodeID string) (DesignNode, error)
    GetComponents(ctx, docID string) ([]Component, error)
    GetPrototype(ctx, docID string) ([]Flow, error)
    GetStyles(ctx, docID string) ([]DesignToken, error)
}

type DesignModel struct {
    Source     string
    DocumentID string
    Version    string
    Nodes      []DesignNode
    Components []Component
    Flows      []Flow
    Tokens     []DesignToken
    Provenance Provenance
}

type DesignDocument struct {
    ID, Name, Version string
    Nodes             []DesignNode
}

type DesignNode struct {
    ID       string
    Kind     string // frame/screen/group/text/component
    Name     string
    Text     string
    Bounds   Rect
    Children []DesignNode
}

type Component struct {
    ID, Name string
    Variants []string
    NodeID   string
}

type Flow struct {
    From, To, Trigger string
    Notes             string
}

type DesignToken struct {
    Name, Value string
}

type Provenance struct {
    Source      string // figma | penpot | excalidraw | file | screenshot | walkthrough | human_demo | agent_exploration
    Method      string // official | reverse_engineered | hand_made
    Author      string
    DerivedFrom string // app build/version when reverse-engineered
    Date        time.Time
    Confidence  string // low | medium | high
}
```

MCP is an access mechanism, not the model. Every design-derived entry is
`design-intent`/`hypothesis`; only runtime probes + developer review can mark it `verified`. If a
design source and the runtime disagree, record `drift`; do not “fix” the runtime to match the
design.

Discovery tooling by platform:

| Platform | Structured discovery | Vision/OCR | Menus and shortcuts |
|---|---|---|---|
| macOS | `axdump`/Accessibility Inspector, AX observers, targeted traversal | `screencapture`, Vision OCR | AX menu bar, key equivalents, AppleScript |
| Electron/web | CDP/DOM, accessibility tree, DevTools | screenshots, OCR | DevTools command menu, DOM/URL inspection |
| Windows | UIA Inspect, UIA tree/patterns | screenshots, OCR | UIA menus, accelerator tables |
| Linux | AT-SPI (Accerciser/`godbus`) | screenshots, OCR | AT-SPI actions, desktop shortcuts |
| Android | `uiautomator dump`, AccessibilityService | `screencap`, OCR | `dumpsys`, keyevents, intents |

Rules:

- Read-only first; never probe send/delete/pay/forward on a production account.
- Use a dedicated test account and a disposable window state.
- Record build/UI hash with every snapshot.
- If AX/DOM coverage is poor, vision is the source of truth; mark OCR confidence per field.
- Enumerate deeplinks/menus/command palettes before coordinate automation.

### 3.4 Discovery procedure

| Step | Method | Artifact | Exit criterion |
|---|---|---|---|
| 0. Prepare | test account, read-only policy, permissions, version, window snapshot | dossier seed | reproducible session |
| 1. Enumerate surfaces | windows/menus/toolbars/sidebars/dialogs; shortcuts | surface inventory | every visible surface listed |
| 2. Inventory controls | targeted AX/DOM traversal; roles, labels, bounds, actions, states | control inventory | primary controls mapped; virtualization noted |
| 3. Map data model | entities, identity fields, attributes, content types | entity model | app entities and identities defined |
| 4. Map navigation | entries, transitions, back/close, search, filters, pagination | state machine | every in-scope path has a transition |
| 5. Map actions | click/type/key/context menu/drag/scroll/upload; effects | action map | each action has a target and observable effect |
| 6. Map async behavior | loading, debounce, virtualization, incoming updates, animations | async map | wait signals and timeouts identified |
| 7. Map failures | login, permissions, modals, network, rate limits, empty/error | failure matrix | detection + handling for each |
| 8. Probe | read-only probes with before/after snapshots, screenshots, OCR; diff | probe log | non-determinism and timing recorded |
| 9. Derive capabilities | group interactions by user intent; write contracts | capability manifest | every in-scope goal has a capability or `Unsupported` |
| 10. Validate coverage | goals × paths × capabilities; fixtures | coverage matrix | gaps explicit; acceptance tests defined |

When a human demonstration/recording or agent exploration trace is available, align its events to
surfaces/controls/states before probing. Demo traces are hypotheses (`source: human_demo` /
`agent_exploration`); they do not become `verified` without runtime probes and developer review.

**Probe harness.** For every step capture:

```text
before: state/observation hash, screenshot, OCR text, selected elements
action: primitive + resolved target (test account only)
after:  state/observation hash, screenshot, OCR text, changed elements
timing: time-to-stable, loading indicators, delayed updates
result: transition, postcondition, ambiguity, failure
```

Probes are serialized per app/account and must not run destructive actions. If an interaction cannot
be probed safely, mark it `unknown` instead of guessing.

### 3.5 Interaction map schema

A minimal shape:

```go
type AppDossier struct {
    ID          string
    BundleID    string
    DisplayName string
    Platform    string
    Builds      []string
    Permissions []string
    Channels    []string // ax | dom | url_scheme | api | applescript | ipc | vision
    AccountNote string
}

type Locator struct {
    AXRole, AXSubrole, AXTitle, AXDescription, AXIdentifier string
    DOMSelector                                             string
    Path                                                    string
    BoundsHint                                              *Rect
}

type Control struct {
    ID      string
    Locator Locator
    Actions []string
    States  []string
    Notes   string
}

type Surface struct {
    ID          string
    Kind        string // navigation | list | detail | composer | dialog | menu | notification
    Locator     Locator
    Controls    []Control
    Virtualized bool
}

type Entity struct {
    ID           string
    Identity     []string // native id, name+avatar, name+preview+time, position
    Attributes   []string
    ContentTypes []string
}

type State struct {
    ID       string
    Surface  string
    Visible  []string
    Blocking bool
}

type Transition struct {
    From, Action, To string
    Pre, Post        []string
    Recovery         []string
}

type InteractionMap struct {
    Dossier     AppDossier
    Surfaces    []Surface
    Entities    []Entity
    States      []State
    Transitions []Transition
    Shortcuts   []string
    Async       []string
    Failures    []string
}
```

Types such as `Rect`, `Element`, `Observation` and `Action` are the normalized platform types from
`pkg/apps`; app-semantic types are summarized in Appendix D.

Excerpt (WeCom; locators marked unknown are still to be discovered):

```yaml
schema_version: 1
app:
  id: wecom
  bundle: com.tencent.WeWorkMac
  platform: macos
  builds: ["5.0.x"]
  permissions: [accessibility, screen_recording, input_monitoring]
  channels: [ax, vision]
surfaces:
  - id: sidebar
    kind: navigation
    locator: {ax_role: AXSplitGroup}
    controls:
      - id: search_field
        locator: {ax_role: AXTextField, bounds_hint: "175,48 180x34"}
        actions: [focus, set_value, confirm]
      - id: chat_table
        locator: {ax_role: AXTable}
        actions: [select_row, scroll]
        notes: virtualized; may report ~28k px height
  - id: chat
    kind: detail
    controls:
      - id: message_scroll
        locator: {}                # discovery pending
        actions: [scroll_page, scroll_to_top, scroll_to_latest]
      - id: in_chat_search
        locator: {}                # discovery pending
        actions: [open, query, next_hit, close]
      - id: back
        locator: {}                # discovery pending
        actions: [leave]
entities:
  chat:
    identity: [native_id?, name+avatar, name+preview+time, position]
    attributes: [name, preview, time, selected, unread]
  message:
    attributes: [sender, time, text, direction, attachment]
states: [Attached, ChatList, SearchResults, Chat, MessageSearch, Modal]
transitions:
  - {from: ChatList, action: search, to: SearchResults, post: [query_echo, results_stable]}
  - {from: SearchResults, action: select_chat, to: Chat, post: [current_chat_matches]}
  - {from: Chat, action: leave, to: ChatList, post: [overlays_closed]}
async:
  - search_debounce_and_late_results
  - message_pane_paint_after_selection
  - virtualized_list_loading_more
failures:
  - duplicate_or_renamed_chat
  - stale_search_results
  - modal_or_login_window
  - wrong_window_or_account
```

### 3.6 From interactions to capabilities

Group interactions by **user intent**, not by control. A capability is a stable, verifiable
operation, not a screen or a click.

A capability is ready to expose only if:

- it has a stable contract (inputs, outputs, errors);
- it has a postcondition checkable without asking the model;
- it is useful to the agent without exposing UI mechanics;
- it can be policy-mapped (read/write, target, HITL);
- it is versioned to app build/UI hash;
- it has a fixture and an integration probe.

```go
type Capability struct {
    Name       string
    Class      ActionClass // observe | navigate | pointer | input | ...
    Input      jsonschema.Schema
    Output     jsonschema.Schema
    Pre, Post  []Condition
    Errors     []ErrorCode
    Idempotent bool
    Timeout    time.Duration
    Versions   []string
    Steps      []Step      // adapter-internal primitive steps
    Verify     []Check     // postcondition checks
}
```

`Condition`, `Check`, `Step` and `ErrorCode` are summarized in Appendix D.

The manifest records agent-facing contracts and adapter-internal steps:

```yaml
schema_version: 1
capabilities:
  - name: chat_open
    class: navigate
    input: {ref: ChatRef}
    output: {chat: ChatRef, state: AppState}
    pre: [ref.resolved, no_blocking_modal]
    post: [current_chat == ref, pane.loaded]
    errors: [not_switched, stale_observation, modal]
    steps:
      - observe: {expect: search_results_or_chat_list}
      - act: {kind: click, target: ref.row}
      - wait: {signal: header == ref.name, timeout: 3s}
      - observe: {expect: chat_loaded}
    verify:
      - selected_row == ref
      - header == ref.name
```

Capability categories:

| Category | Examples | Default policy |
|---|---|---|
| Observe | current state, current entity, capabilities | read |
| Query | find/list/search | read |
| Navigate | open, leave, back, close overlay | navigate/pointer |
| Read | visible page, context, older pages | read |
| Mutate | compose, send, reply, forward, delete | input + HITL |
| Session | login, account switch, reconnect | challenge/HITL |

Anti-patterns:

- exposing `click_row` / `type_text` as the default agent surface;
- one capability per screen or per control;
- a capability with no postcondition;
- hiding unsupported interactions behind a prompt;
- making the model choose among duplicate low-level tools.

### 3.7 Identity, search, read and scroll

These are the recurring hard problems. They must be designed per app, not generalized away.

**Identity.** Prefer, in order: native id exposed by AX/DOM; composite fingerprint (name +
avatar/member count/pinned); name + volatile preview/time fingerprint; list position (last resort,
low confidence). On conflict return candidates + `Ambiguous`; never silently open a likely-wrong
chat. Positional element ids are internal and must never become the model-facing ref.

**Search.** Search is not list filtering. Results may mix chats, contacts, message snippets,
documents and apps; queries can be remote and debounced; old results can arrive late. The adapter
must classify hit types, discard stale responses, restore the previous list on cancel/failure, and
return candidates with evidence. Use the native search for recall; local AX filtering is only a
fallback/ranking aid.

**Read and scroll.** For custom-rendered panes, OCR is lossy: sender/time association, line breaks,
quotes, cards, emoji, links and system messages must be reconstructed. Keep block bounds and
confidence; dedupe across scrolls using overlapping anchors; mark `AtTop`/`AtBottom`; handle
“load more”, new incoming messages and virtualized lists. Prefer structured AX/DOM message rows
when a build exposes them; otherwise OCR is approximate and must say so.

**Timing and interference.** Fixed sleeps are not a contract. Wait for stable UI (repeated hash
samples, loading indicators gone, expected element present, message count stable). Serialize actions
per app/account. Detect and handle modals, login/update prompts, file pickers and focus stealing
before acting.

**Verification.** Verify per operation: header/title plus selected row for identity; content
anchors for scroll; query echo for search; overlay closed for leave. Return typed errors
(`ErrNotFound`, `ErrAmbiguous`, `ErrNotSwitched`, `ErrAtBoundary`, `ErrUnsupported`,
`ErrStaleObservation`) and audit the postcondition result.

### 3.8 Developer-in-the-loop delivery process

The AI can do most discovery and implementation, but the developer owns **ground truth, scope,
semantics, policy and sign-off**.

**Roles.**

| Role | Owns | Not responsible for |
|---|---|---|
| App owner / domain expert | goals, identity/ambiguity rules, “what does return/search mean”, golden data, sign-off | writing adapter code |
| Adapter engineer (AI-assisted) | dossier, interaction map, capability manifest, adapter code, fixtures, drift reports | deciding business semantics or risk acceptance |
| QA / test owner | scenario coverage, live E2E, metrics, regression matrix | probing destructive actions |
| Security / privacy | PII handling, redaction, retention, ToS/legal risk | UI implementation |
| Operator / HITL owner | approvals, sensitive-action policy, incident handling | normal read-only flows |
| Platform owner | OS framework, permissions/TCC/UAC/portal, signing | app semantics |

**Phases and gates.**

| Phase | AI output | Developer action | Gate / exit |
|---|---|---|---|
| 0. Intake (both) | draft scope, target builds, required permissions | provide app, test account, permissions, 5–10 golden goals, known-answer data, safe/non-goal areas | **G0 scope approved** |
| 1. Discovery (Stage 1) | dossier, interaction map, probe logs, unknowns | walk through the app, correct the map, mark hidden flows/shortcuts/deeplinks | **G1 map approved**; every in-scope goal has a path or explicit `unknown` |
| 2. Capability design (Stage 1) | capability manifest, coverage matrix, policy classes | approve names, inputs/outputs, pre/postconditions, error semantics, read/write/HITL | **G2 capability contract approved** |
| 3. Adapter design (Stage 2) | state machine, operations, algorithms, verification, recovery | review identity/ambiguity, scroll/search/leave semantics, no raw UI leaks | **G3 design review passed** |
| 4. Implementation (Stage 2) | adapter code, fixtures, contract tests | review code paths, fixtures, redaction, version pins | **G4 code + fixtures approved** |
| 5. Validation (Stage 2) | live E2E report, metrics, unsupported list | run/observe golden scenarios, confirm known answers, sign off thresholds | **G5 acceptance passed** |
| 6. Release (Stage 2) | tool registration, policy wiring, monitoring | enable capabilities, approve policy/HITL, notify operators | **G6 release approved** |
| 7. Maintenance (both) | drift reports, version matrix, regression | triage drift, update design/map/adapter, trigger re-discovery | **G7 version update accepted** |

**What the developer must provide.**

- A logged-in test account, a disposable window state and the required permissions.
- 5–10 golden goals with expected outcomes.
- A known-answer dataset: known entities, duplicate names, empty states, long histories, renamed
  entities, attachments/quotes, different accounts.
- One walkthrough (live or recorded) of normal and edge flows, including shortcuts, deeplinks,
  context menus and hidden entry points; optionally a recorded demonstration/event trace.
- Explicit identity/ambiguity rules and the semantics of return/search/read scope.
- Explicit non-goals and destructive areas.
- App release cadence and a contact for drift triage.
- Optional design source (Figma/Penpot/files/screenshots/walkthrough); it is reconciled, not treated as specification.

**What the AI must produce.**

- Machine-readable artifacts, not prose-only claims.
- Evidence per interaction: build/UI hash, before/after snapshot, screenshot/OCR, AX/DOM path,
  timing and failure notes.
- Explicit `unknown`/`unsupported`/`out-of-scope` lists.
- A traceable coverage matrix: goal → path → capability → test → metric.
- Fixtures and contract tests; no capability without a postcondition.
- Drift reports when design, map or runtime disagree.

**Definition of done for an app adapter.**

1. Every in-scope golden goal maps to a verified path and capability, or is explicitly
   `unsupported`/`out-of-scope`.
2. Every advertised capability has a contract, postcondition, typed errors and policy class.
3. Fixtures cover identity ambiguity, empty/error/loading, long history, scroll boundaries and
   version drift; fixtures are redacted.
4. Live E2E passes on a test account: wrong-entity rate 0, postcondition pass rate ≥ 99% (100% for
   read-only where feasible), read completeness ≥ 95%, scroll duplicate rate < 1%.
5. Unsupported capabilities are not registered as tools; the model cannot reach raw element ids,
   coordinates or OCR clips for normal operations.
6. The adapter/UI map is pinned to app build/UI hash; the version matrix and re-discovery trigger
   are documented.
7. Policy/HITL is wired; no read of a wrong entity after detection; incident/rollback path exists.

**Suggested cadence (read-only app, one platform).** Kickoff 1 day; discovery 2–3 days; map review
1 day; capability design 1–2 days; implementation 3–5 days; fixtures/E2E 2 days; sign-off 1 day.
Ongoing: re-run on app updates and after any drift report. Vision-only apps take longer.

### 3.9 Discovery harness design

The harness is a thin layer over the platform control framework and guarded `Session`. It is **not**
a crawler that blindly clicks everything. It captures evidence, probes read-only flows and produces
the Stage 1 bundle.

**Components.**

| Component | Stage | Responsibility |
|---|---|---|
| Snapshot store | 1 | AX/DOM tree, screenshot, OCR, window/account/build/UI hash, timestamp |
| Targeted observer | 1 | surface/subtree traversal with role/bounds filters and element budgets |
| Probe runner | 1 | ordered read-only steps: observe, focus, click, type, key, scroll, wait, assert |
| Diff engine | 1 | before/after element/text/state changes; separates hover/focus from real transitions |
| State-graph builder | 1 | states and transitions from probe results; marks ambiguity/non-determinism |
| Fixture recorder/replayer | 1 → 2 | record deterministic fixtures; replay them in Stage 2 tests |
| Coverage reporter | 1 | goal × path × capability × test; explicit unknown/unsupported |
| Drift reporter | 1 ↔ 2 | design/map/runtime mismatch with evidence and proposed action |
| Capability generator | 2 | manifest → tool schemas + policy classes + error mapping |
| Metrics/audit | 2 | postcondition pass rate, wrong-entity rate, read/scroll coverage, latency, denials |

**Minimal interfaces.**

```go
type Snapshot struct {
    ID        string
    Platform  string
    App       string
    Build     string
    UIHash    string
    Timestamp time.Time
    Elements  []apps.Element
    Image     []byte
    OCR       apps.OCRResult
    Meta      map[string]string
}

type ProbeStep struct {
    Name   string
    Action apps.Action
    Wait   []WaitCondition
    Assert []Assertion
}

type ProbeResult struct {
    Scenario string
    Steps    []StepResult
    State    string
    Drift    []Drift
}
```

The harness consumes the existing platform driver; it does not add a second control path:

```text
Probe runner -> apps.Session -> Guard -> platform driver -> app
Snapshot store <- observe/screenshot/OCR
Capability generator -> agent.Tool + Permission
```

**CLI shape (illustrative).**

```text
# Stage 1: App Control Model (development-time model builder)
appmodel discover  --app wecom --platform macos --build 5.0.10 \
                   --goals goals.yaml --out apps/wecom/macos/5.0.10/default
appmodel probe     --app wecom --platform macos --build 5.0.10 \
                   --scenario scenarios/open-chat.yaml --record
appmodel diff      --before snapshot-a --after snapshot-b
appmodel fixtures  record --app wecom --platform macos --build 5.0.10
appmodel coverage  --map apps/wecom/macos/5.0.10/default/interaction-map.yaml
appmodel drift     --map apps/wecom/macos/5.0.10/default/interaction-map.yaml --live
appmodel validate  --bundle apps/wecom/macos/5.0.10/default

# Stage 2: capability runtime (compile + replay)
capgen generate --bundle apps/wecom/macos/5.0.10/default \
                --out generated/wecom/macos/5.0.10/default
capgen replay   --bundle apps/wecom/macos/5.0.10/default --fixtures apps/wecom/macos/5.0.10/default/fixtures
```

**Data layout.**

```text
apps/<app>/<platform>/<build>/<variant>/ # Stage 1 handoff bundle (versioned)
  model.yaml
  dossier.yaml
  interaction-map.yaml
  ui-map.yaml
  identity.yaml
  capabilities.yaml
  coverage.yaml
  scenarios/
  drift/
  fixtures/

generated/<app>/<platform>/<build>/<variant>/  # Stage 2 outputs (not the source of truth)
  tools.yaml
  policy.yaml

apps/<app>/models.yaml         # registry: supported builds, status, model/adapter links
```

**Implementation order.**

Stage 1: read-only snapshot store + targeted observer (reuse operator/`axdump`); probe runner;
diff/state-graph; fixture recorder and coverage/drift reports.

Stage 2: capability generator + adapter skeleton + tool registration + policy mapping; fixture
replay/contract tests/live E2E; metrics, version matrix, re-discovery triggers.

**Safety.**

- Read-only first; no send/delete/pay/forward probes on production accounts.
- Serialize probes per app/account; never run two controllers on one account.
- Record build/UI hash and redact PII before storing snapshots.
- If a flow cannot be probed safely, mark it `unknown`; do not infer control from a design source.
- The model never receives probe artifacts or raw UI handles; only verified capabilities.

**Automation boundary.**

| Automated | Human-owned |
|---|---|
| capture, targeted traversal, diff, repeat probes, fixture replay, hypothesis generation, coverage report | goals, identity/ambiguity rules, semantics of return/search/read scope, unsupported decisions, risk acceptance, sign-off |

### 3.10 Model registry, versioning and maintenance

The App Control Model is **not a one-time artifact**. It is keyed by app, platform, build and UI
hash, and it must be maintained for as long as that app/platform is supported.

**Model key.**

```text
(app_id, platform, app_build, ui_hash)
```

Add variant dimensions only when they change UI/behavior: locale, theme, account role/type, window
layout, server-driven UI/A-B cohort.

**Identity detection and model selection.** After launch/attach and before exposing capabilities,
the runtime resolves the app identity:

```go
type AppIdentity struct {
    App            string
    Platform       string
    Build          string
    BuildNumber    string
    ExecutableHash string
    UIHash         string
    Variants       map[string]string // locale, theme, account role, layout, cohort
}

type ModelRef struct {
    App            string
    Platform       string
    Build          string
    UIHash         string
    ModelVersion   string
    AdapterVersion string
}
```

Detection:

- **Build**: platform metadata — macOS `CFBundleShortVersionString`/`CFBundleVersion`, Windows
  `FileVersionInfo`, Android `versionName`/`versionCode`, Linux package version, Electron/web build
  id.
- **Executable/package hash and signature** when build metadata is missing, spoofed or ambiguous.
- **UI hash**: a canonical structural fingerprint of the main window — roles, subroles, labels,
  actions, relative bounds and hierarchy — **excluding volatile content** such as message text,
  previews, timestamps, unread counts and avatars. Hash a stable screen (for example the main
  window/chat list), not the currently open conversation.
- **Variants**: locale/theme/account role/window layout/feature-flag state when they change the UI.

Version is necessary but **not sufficient**: the same build may have A/B or server-driven UI
variants. The UI hash and declared variants disambiguate them.

Selection order:

1. exact `(app, platform, build, UI hash, variants)` with a `verified` model;
2. a model that explicitly declares compatibility (same UI hash or a verified build range/variant);
3. configured fallback (generic primitives/vision + HITL) if policy allows;
4. fail closed with `ErrModelNotFound`/`ErrProfileMismatch`.

Rules:

- Never silently use a model for a different platform, build, UI hash or variant.
- Audit the detected identity and the selected model id with the session.
- If the app updates or restarts mid-session, invalidate the session and re-resolve; do not continue
  with a stale model.
- The UI-hash algorithm is versioned; changing it invalidates models until they are re-registered.

**Model registry.** Each app keeps a registry of supported models:

```yaml
schema_version: 1
app: wecom
platform: macos
os_versions: [">=14"]
models:
  - build: "5.0.10"
    build_number: "500010"
    executable_hash: "sha256:..."
    ui_hash: "sha256:..."
    hash_algorithm: "structural-v1"
    variants: {locale: "zh-CN", theme: "light", account: "enterprise"}
    status: verified          # draft | probed | verified | drift | deprecated | unsupported
    compatible_with: []       # explicit build/UI-hash compatibility declarations
    model: apps/wecom/macos/5.0.10/default
    adapter: pkg/apps/macos/wecom
    fixtures: apps/wecom/macos/5.0.10/default/fixtures
    last_verified: 2026-10-18
    deprecates: "5.0.9"
    notes: "in-chat search unverified"
  - build: "5.0.9"
    status: deprecated
    ...
```

**UI-hash requirements.**

- Hash a canonical stable screen (main window or chat list), not the current conversation.
- Include structural fields: role, subrole, label/description, actions, relative bounds, hierarchy.
- Exclude volatile content: message text, previews, timestamps, unread counts, avatars, media.
- Scope: per app instance/account; multi-window apps need a declared canonical window.
- The hash algorithm is versioned (`hash_algorithm`); changing it invalidates old models.
- Variants (locale/theme/account/layout/A-B) are matched before a hash-only fallback.

**Compatibility policy.**

- Exact build + UI hash + variants with a `verified` model: use it.
- Unknown build but same UI hash: **probe-only by default**. Enabling capabilities requires an
  explicit `compatible_with` declaration or developer/policy approval; record and audit it.
- Unknown build/UI hash mismatch: `ErrProfileMismatch`; vision fallback/HITL or refuse. Never
  silently use a stale model.
- Platform/OS mismatch: no model; capability is `Unsupported` unless a fallback adapter is
  configured.
- Variant mismatch: the model must declare supported variants; otherwise fail closed.

**Maintenance workflow.**

1. Detect a new build: release feed/manual/CI/user report.
2. Compare build and UI hash; identify changed surfaces.
3. Run Stage 1 discovery/diff on changed surfaces (full re-discovery on major UI changes).
4. Produce a drift report with evidence and proposed action.
5. Update the model bundle, coverage, provenance and fixtures; bump the model version.
6. Re-run Stage 2 capability generation, fixture replay and contract tests.
7. Live E2E on a canary account; compare metrics to the previous build.
8. Publish the registry entry; deprecate the old model; roll out gradually.
9. Monitor; roll back if wrong-entity/read/scroll metrics degrade.
10. Runtime repair: if a live session detects drift before an effect, halt before the effect, capture
    evidence, propose a fix and require approval; retry bounded.
11. Admission: record bundle/model/adapter + test evidence + approver; mark registry `verified` only
    after qualification/admission.

**Test matrix.**

- platform × build × account/role × locale/theme × window layout;
- A/B cohorts and server-driven UI variants when detectable;
- fixtures per build; fixture replay in CI;
- live E2E on a canary account per platform;
- compatibility tests for old builds inside the support window.

**Ownership and SLA.**

- Each `app × platform` has an owner and escalation path.
- Define a support window (for example: latest N builds) and a time-to-update target.
- Maintenance is budgeted: discovery/diff automation, fixture refresh, E2E and on-call.
- High-value apps get dedicated adapters; long-tail apps use primitives/vision + HITL and are
  explicitly marked lower reliability.
- A model that cannot be maintained should not be shipped as a supported capability.

**Metrics.** Model/version coverage, drift age, time-to-update, wrong-entity rate, read
completeness, scroll coverage, unsupported rate, and traffic on verified vs fallback/HITL paths.

**Cost and strategy.** Initial adapter development is only part of the cost; maintenance may
dominate over the product lifetime. Automate drift detection and fixture replay; keep capability
contracts stable across builds/platforms; prefer a small set of high-value supported builds over
broad but stale coverage.

### 3.11 Repository layout and ownership

Stage 1 is **development-time tooling**. It must not be a runtime dependency of the SDK or the
agent. The layout below is the **target layout**; not all packages exist yet. The current repo
already has `pkg/apps`, `pkg/mcp` (client/adapter) and `tools/macos-operator`.

```text
cmd/appmodel/                  # Stage 1 CLI: discover/probe/diff/fixtures/coverage/drift/validate
cmd/capgen/                    # Stage 2 CLI: generate/replay/verify
internal/apps/discovery/       # harness: snapshot/probe/diff/state graph/coverage/drift
internal/apps/designsource/    # MCP/Figma/Penpot/file/screenshot adapters
internal/apps/model/           # bundle schema, validation, registry (internal by default)
internal/apps/capgen/          # Stage 2 compiler: bundle -> tools/policy/adapter skeleton
pkg/apps/                      # platform-neutral Session/Guard/Policy/Audit
pkg/apps/<platform>/           # platform control framework/driver
pkg/apps/<platform>/<app>/     # Stage 2 runtime adapter (checked-in code)
apps/<app>/<platform>/<build>/<variant>/ # Stage 1 model bundle (private registry/example)
tools/macos-operator/          # signed macOS helper
```

Rules:

- `pkg/apps` must not import `internal/apps/discovery`, `internal/apps/designsource`, `cmd/appmodel`
  or `cmd/capgen`.
- Stage 1 imports `pkg/apps` (Session/Guard/platform drivers) and design sources; it is not imported
  by the runtime.
- Stage 2 consumes the bundle, compiles the tool/policy/skeleton projections, and guides the
  handwritten adapter implementation; generated tool schemas are not the source of truth and
  semantic algorithms are not generated from YAML.
- `internal/apps/model` may be promoted to `pkg/apps/model` only when an external host or third-party
  app author must name the types.
- `cmd/appmodel` can be split into a separate Go module (`tools/appmodel`) if MCP/design dependencies
  grow; keep the bundle schema in a small shared module so Stage 1 and Stage 2 stay compatible.
- Model bundles and fixtures may contain proprietary UI maps or PII. Keep them in a private artifact
  store/registry; the public repo ships schemas and the WeCom reference example only.
- If a model cannot be maintained, it should not be registered as a supported capability.

Ownership:

| Area | Owner |
|---|---|
| `cmd/appmodel`, `internal/apps/discovery`, `internal/apps/designsource` | SDK/toolchain team |
| `cmd/capgen`, `internal/apps/capgen` | adapter engineer + CI/platform team |
| `apps/<app>/<platform>/<build>/<variant>/` (model bundle, scenarios, fixtures) | app owner + QA |
| `pkg/apps/<platform>/` and `tools/macos-operator` | platform owner |
| `pkg/apps/<platform>/<app>/` (runtime adapter) | app owner + adapter engineer |
| Registry/lifecycle/SLA | release owner + app owner |

CI:

- schema validation for every bundle;
- harness unit tests with fake controllers;
- fixture replay per supported build/platform;
- live canary E2E (env-gated);
- compatibility policy checks (build/UI hash/platform).

## 4. Stage 2: Capability compilation and runtime

Stage 2 consumes the Stage 1 bundle as the authoritative semantic model, plus the platform control
framework and handwritten adapter code. It has three parts: **2a compilation** (tool schemas,
policy mappings, adapter skeleton, fixture/test wiring), **2b adapter implementation** (handwritten
state machine, identity, search/read/scroll/verification/recovery), and **2c runtime execution**.
The generator/runtime does not re-read design sources or prose; 2b may consult them for
understanding, but the bundle remains authoritative.

### 4.1 Runtime architecture and request flow

```text
Agent tool call
  → schema validation
  → Session: app/model/capability check
  → Guard: permission/HITL → capability grant (bounded plan)
  → Capability runtime: descriptor lookup + core/pack dispatch
  → adapter/pack: resolve ref, execute steps via guarded primitives
  → wait/observe/verify postcondition
  → structured result / typed error
  → audit + metrics
```

Rules: actions are serialized per app/account; timeouts/cancellation are explicit; retries are
bounded; stale model/wrong entity stops the operation; no raw handles reach the model. Each stage
maps failures to the typed errors in §6.5.

### 4.2 Adapter core and capability packs

Every app adapter implements the **core shape** and the optional packs it supports:

```go
type CoreAdapter interface {
    Capabilities() []CapabilityDescriptor
    Find(ctx, FindQuery) ([]SearchHit, error)
    Current(ctx) (Entity, error)
    Open(ctx, EntityRef) (Entity, error)
    Leave(ctx, LeaveOptions) (Location, error)
    Read(ctx, ReadOptions) (Page, error)
    Scroll(ctx, ScrollOptions) (Page, error)
    Search(ctx, SearchOptions) ([]SearchHit, error)
}

type CapabilityDescriptor struct {
    Name        string // chat.open, channel.open, thread.read, ...
    Class       ActionClass
    Pack        string // core | workspace | channel | thread | message | attachment | file | unread | send
    Input, Output jsonschema.Schema
    Pre, Post   []Condition
    Errors      []ErrorCode
}
```

Optional pack interfaces (implemented only when supported):

```go
type WorkspacePack interface { ListWorkspaces(...); SwitchWorkspace(...) }
type ChannelPack interface { ListChannels(...); OpenChannel(...); JoinChannel(...); LeaveChannel(...) }
type ThreadPack interface { ListThreads(...); OpenThread(...); ReadThread(...); ReplyThread(...) }
type MessagePack interface { React(...); Reply(...); Edit(...); Delete(...); Pin(...); Forward(...) }
type AttachmentPack interface { ListAttachments(...); OpenAttachment(...); DownloadAttachment(...) }
type FilePack interface { ListFiles(...); OpenFile(...); UploadFile(...); DownloadFile(...) }
type UnreadPack interface { ListUnread(...); MarkRead(...) }
type SendPack interface { Send(...) } // post-MVP, HITL only
```

Rules:

- Core methods cover conversation-level operations; packs add containers/threads/messages/files.
- An adapter implements only the packs it supports; `Capabilities()` describes them.
- The capability runtime binds `Descriptor.Name`/`Pack` to a core method or pack handler; unsupported
  packs are not advertised.
- Shared wait/retry/OCR/scroll helpers are allowed; interaction semantics are not shared.

`FindQuery`, `SearchHit`, `Entity`, `EntityRef`, `Location`, `ReadOptions`, `Page`,
`ScrollOptions`, `SearchOptions` and `LeaveOptions` are summarized in Appendix D.

### 4.3 State model and hierarchy

```text
Attached
  └── Container(kind=workspace|list)
        ├── SearchResults(scope, query)
        └── Container(kind=channel|list)
              ├── Entity(ref)          # chat/channel
              │     └── Thread(ref)
              │           └── ScopedSearch(query, scope=thread)
              └── ScopedSearch(query, scope=channel)
Any state -> Modal(kind) -> previous state
```

Each state records: window/account, container stack, current entity, search scope/results, page
cursor, loading/blocking state and open modal. An operation is valid only if the current state and
its preconditions hold.

- `Current()` returns the current entity plus its container stack.
- `Open(ref)` is kind-aware: chat/channel/thread.
- `Leave(options)` pops to a parent/level; default parent, optional explicit target.
- `Search(scope, query, filters)` is scoped: current/container/workspace/global.
- Packs may add states, but must declare transitions and postconditions.

### 4.4 Operation contracts

Core:

| Operation | Preconditions | Steps | Postconditions | Main failures |
|---|---|---|---|---|
| `Find(query)` | foreground, no blocking modal, search/list entry | focus/clear/type/confirm, wait for debounce/results | results belong to query; result set stable; query echo | IME not committed, stale results, heterogeneous hits |
| `Open(ref)` | ref resolvable, target visible | select/open kind-aware target, wait for header/selected, wait for load | `Current == ref`; pane loaded | wrong row, no switch, load timeout, modal |
| `Current()` | foreground | read selected row + header/container (cross-check) | entity + container + confidence | header/row disagree, ambiguous |
| `Read(opts)` | current entity, pane visible | capture pane, OCR/AX, reconstruct | page belongs to current entity; cursor set | obscured pane, loading; low confidence is a warning |
| `Scroll(dir)` | current entity, cursor known | target scroll container, page/wheel, wait stable, dedupe | content advanced or boundary; cursor updated | wrong pane, no movement, loading-more stuck |
| `Search(opts)` | capability present, scope valid | open scoped search, query/filters, wait, parse/jump/close | hits belong to requested scope; overlay closed | unsupported scope, no hits, overlay stuck |
| `Leave(opts)` | any state | close top overlay/search, then parent/target | location == parent; no draft lost | unsupported, modal blocking |

Pack operations (examples; same response/error envelope):

| Pack | Operation | Preconditions | Postconditions | Errors |
|---|---|---|---|---|
| workspace | `ListWorkspaces`, `SwitchWorkspace` | app foreground | current workspace matches | ambiguous, unsupported |
| channel | `ListChannels`, `OpenChannel`, `JoinChannel`, `LeaveChannel` | workspace/channel context | `Current == channel`; membership updated | not_found, no_permission, modal |
| thread | `ListThreads`, `OpenThread`, `ReadThread`, `ReplyThread` | channel open | `Current == thread`; reply visible | not_found, unsupported, hitl_required |
| message | `React`, `Reply`, `Edit`, `Delete`, `Pin`, `Forward` | current message | action reflected; permissions enforced | no_permission, hitl_required, conflict |
| attachment | `ListAttachments`, `OpenAttachment`, `DownloadAttachment` | current message | attachment state/URI returned | unsupported, no_permission, timeout |
| file | `ListFiles`, `OpenFile`, `UploadFile`, `DownloadFile` | current context | file state/URI returned | unsupported, no_permission, timeout |
| unread | `ListUnread`, `MarkRead` | foreground | unread state updated | unsupported, conflict |
| send | `Send` | ref resolved | message appears; sent-state verified | no_permission, hitl_required, timeout |

**Cursor semantics.** A `Cursor` is an opaque adapter token (message anchor + offset), not a raw
element id or universal scroll position. It is valid only for the same app/model/entity/session; it
expires on model/app change. Pagination dedupes with overlapping anchors and must report
`at_boundary` rather than looping.

**Per-step source selection.** Each operation chooses the most reliable channel: official API →
structured UI (AX/DOM/UIA/AT-SPI) → OCR/vision. An API action may replace a UI click when the app
exposes it; the adapter still verifies the result, preferably by read-back. The chosen source and
fallback reason are recorded.

### 4.5 Capability compilation and tool projection

**2a inputs.** A validated Stage 1 bundle (`capabilities.yaml`, `interaction-map.yaml`, `ui-map.yaml`,
`identity.yaml`, `coverage.yaml`) plus generator templates.

**2a outputs.** Generated tool schemas/descriptions, policy-class metadata, core/pack adapter
skeleton, fixture/test wiring and a capability-descriptor table. Each output is pinned to the
bundle hash + generator version.

**2a steps.**

1. Validate the bundle (schema + referential integrity).
2. Load the capability manifest and descriptors.
3. Project tool schema/description/examples per capability.
4. Project policy class, target type and capability/pack name.
5. Generate core/pack adapter skeleton and fixture/test wiring.
6. Validate generated output against the manifest.
7. Write generated artifacts; do not hand-edit them.

The tool schema is generated from the capability manifest; do not hand-write UI instructions.

For each capability:

- **Name**: a verb the model can reason about (`chat_find`, `chat_open`, `chat_read`, ...).
- **Input**: structured JSON only; no element ids, coordinates, OCR clips or AX paths.
- **Output**: a stable envelope with data, evidence, state and cursor.
- **Errors**: typed (`ErrNotFound`, `ErrAmbiguous`, `ErrUnsupported`, `ErrNotSwitched`,
  `ErrAtBoundary`, `ErrStaleObservation`, ...).
- **Description**: semantics and limits; no app-specific click sequence.
- **Examples**: one success and one ambiguity/failure case.
- **Filtering**: only capabilities satisfying
  `verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role` are advertised (§6.6).
- **Pack**: core vs workspace/channel/thread/message/file/unread/send; generated from the
  descriptor table.

Suggested result envelope:

```json
{
  "ok": true,
  "state": {"location": "entity", "entity": {"name": "..."}},
  "data": {"messages": []},
  "evidence": [{"kind": "header", "value": "..."}],
  "confidence": 0.92,
  "next_cursor": "...",
  "warnings": [],
  "next": ["chat_read", "chat_scroll", "chat_leave"]
}
```

Projected tool schema example:

```json
{
  "name": "chat_open",
  "description": "Open a chat by ref returned by chat_find/chat_current. Verifies the active chat before returning.",
  "input": {
    "type": "object",
    "properties": {"ref": {"$ref": "#/$defs/ChatRef"}},
    "required": ["ref"]
  },
  "output": {
    "type": "object",
    "properties": {
      "chat": {"$ref": "#/$defs/ChatRef"},
      "state": {"type": "object"},
      "confidence": {"type": "number"}
    }
  }
}
```

The model sees `ChatRef`, not element ids. The same call at three levels:

| Level | Example |
|---|---|
| Agent tool call | `chat_open {"ref":"chat:..."}` |
| Adapter internals | focus search → type → wait → select row → wait for header → verify `Current == ref` |
| Never in the call | `e88`, AX/DOM path, x/y, OCR `Rect`, scroll container |

The adapter maps `chat_open(ref)` to internal primitives and verifies the postcondition.

### 4.6 Adapter implementation (2b)

A typical adapter package:

```text
pkg/apps/<platform>/<app>/
  adapter.go        # CoreAdapter + CapabilityDescriptor list
  state.go          # state machine/hierarchy
  find.go           # search/list recall, classification, ranking, ambiguity
  open.go           # kind-aware open + current verification
  read.go           # OCR/AX extraction, reconstruction, dedupe
  scroll.go         # container targeting, cursor, boundary, latest
  search.go         # scoped search, filters, hit navigation
  leave.go          # overlay close, parent/container navigation
  verify.go         # shared postcondition checks
  packs/
    workspace.go
    channel.go
    thread.go
    message.go
    attachment.go
    file.go
    unread.go
    send.go         # post-MVP/HITL
  fixtures/
  adapter_test.go
```

Implementation responsibilities:

- state machine and legal transitions (including hierarchy);
- identity/ambiguity resolution;
- search result classification/ranking and stale-result handling;
- read reconstruction and scroll dedupe/cursor;
- verification and recovery per capability;
- pack methods for containers/threads/messages/files/unread;
- descriptor list for everything implemented.

Rules:

- Core methods and pack interfaces are the unit of implementation; generated skeletons are a
  starting point, not the final adapter.
- Shared wait/retry/OCR/scroll helpers are allowed; interaction semantics remain app-specific.
- A pack that is not implemented is not listed in `Capabilities()` and gets no tools.
- Contract tests, fixture replay, pack tests and live E2E are required before `verified`.

### 4.7 Guard, policy and HITL mapping

- A capability declares its class and target type; the host policy authorizes capability names
  (`chat.read`, `chat.scroll`, `chat.send`) and/or their primitive steps.
- Authorization uses the capability-level bounded-plan grant from §2.4; high-risk capabilities may
  require per-primitive grants via policy.
- The adapter maps the capability to guarded primitives; it must not bypass `Guard`.
- Write capabilities require explicit permission and HITL; read capabilities default to read-only.
- The model cannot see or invent target ids; it passes a `ChatRef`/cursor returned by the adapter.
- Unsupported/unauthorized capabilities are not registered as tools; the projection formula is in
  §6.6.
- The primitive `app_*` tools are available only under explicit policy for opaque/exploratory
  flows; they are not the normal path.

### 4.8 Runtime verification, errors and recovery

- Verify per operation, not with a generic UI-hash change (hover, focus, incoming messages also
  change it). Checks include identity/selected row/header, content anchors, query echo, overlay
  closure, cursor advance/boundary and sent-state.
- Return typed errors and audit the postcondition result. Map engine errors to the tool error
  envelope in §6.5 (code, message, details, retryable, suggested_next).
- Re-observe/retry with bounded attempts; reattach on PID/app restart; never continue after a
  stale model, wrong entity or wrong account.
- Detect modals, login/update prompts, file pickers and focus stealing before acting.
- Never resend a write blindly; use idempotency and sent-state verification.
- On version/UI-hash mismatch, fail closed (`ErrProfileMismatch`) or use a configured fallback.
- Respect context cancellation/timeouts; serialize actions per app/account; record latency and
  retries.
- Distinguish:
  - empty result (success + `warnings`/empty data) from `not_found`;
  - low OCR confidence/partial page (warning) from `unsupported`/failure;
  - `at_boundary` (expected) from `stale_observation` (retryable);
  - `unsupported` (do not retry) from `timeout`/`busy` (bounded retry).

**Effect verification (writes; post-MVP).** Read-only capabilities verify UI state. Writes need a
typed, machine-checkable effect contract plus read-back at a declared evidence tier; a success
screen is never proof.

```go
type EffectKind string

const (
    EffectRecordWritten EffectKind = "record_written"
    EffectFieldEquals   EffectKind = "field_equals"
)

type EffectValue struct {
    Literal *string // exactly one of Literal/Param
    Param   *string // resolve against this run's inputs; unresolved -> indeterminate
}

type Effect struct {
    Kind                 EffectKind
    Match                map[string]EffectValue // identifies the intended record
    Field                *string                // field_equals only
    Value                *EffectValue           // field_equals only
    ExpectedCount        int                    // record_written only; 0 means 1
    IdempotencyKey       *EffectValue           // record_written only
    KeyField             string                 // default "key"; names the record key field
    CountNewOnly         bool                   // record_written only; requires a readable pre-state baseline
    ForbidCollateralLoss bool                   // record_written only; default true; refute on collateral loss
    Timeout              time.Duration          // 0 = verifier default
}

type EffectVerdictStatus string

const (
    EffectVerdictConfirmed     EffectVerdictStatus = "confirmed"
    EffectVerdictRefuted       EffectVerdictStatus = "refuted"
    EffectVerdictIndeterminate EffectVerdictStatus = "indeterminate"
)

type EffectObservation string

const (
    EffectObservedPresent     EffectObservation = "present"
    EffectObservedAbsent      EffectObservation = "absent"
    EffectObservedConflicting EffectObservation = "conflicting"
    EffectObservedUnknown     EffectObservation = "unknown"
)

type EvidenceTier int

const (
    TierSystemOfRecord       EvidenceTier = 1 // API, DB, FHIR, file, mail
    TierSeparateSession      EvidenceTier = 2 // separately signed-in, read-only UI session
    TierReacquiredPersisted  EvidenceTier = 3 // acting session re-navigates and re-reads
    TierSameScreen           EvidenceTier = 4 // acting screen still shows the value
)

type EffectVerdict struct {
    Verdict        EffectVerdictStatus
    Observed       EffectObservation
    Tier           EvidenceTier
    Reason         string
    ObservedCount  *int
    ObservedValue  *string
    EvidenceDigest string // privacy-safe; no raw record/text
}
```

**Evidence tiers.** Lower numbers are stronger. A write is **effect-verified** only at tier 1 or 2;
other tiers may inform diagnostics but cannot yield `OutcomeVerified`. Tier 3 is useful for
diagnostics but is still the acting session; tier 4 is a UI hint, not verification. A `regulated`
policy may require tier 1. The verifier advertises its tier; the runtime never infers it from a
class name or prose.

**Verdicts.** For `record_written`, `confirmed` means the intended record is observed present
exactly the expected number of times; for `field_equals`, it means the matched record carries the
required value. `refuted` means the record positively contradicts the effect; `indeterminate` means
the record is unreachable, ambiguous or incomplete. `indeterminate` halts and is never treated as
success. A `refuted` verdict with `EffectObservedAbsent` is the only positive absence claim;
`refuted` with `present`/`conflicting` is a wrong effect. Verifiers must keep status and observation
consistent: `confirmed` requires `present`; absence requires `refuted` + `absent`.

**Outcome taxonomy.**

```go
type TransactionOutcome string

const (
    OutcomeVerified               TransactionOutcome = "verified"
    OutcomeHaltedBeforeEffect     TransactionOutcome = "halted_before_effect"
    OutcomeReconciliationRequired TransactionOutcome = "reconciliation_required"
    OutcomeCompletedUnverified    TransactionOutcome = "completed_unverified"
    OutcomeRejectedPolicy         TransactionOutcome = "rejected_policy"
    OutcomeCanceled               TransactionOutcome = "canceled"
    OutcomeFailedPlatform         TransactionOutcome = "failed_platform"
    OutcomeRolledBack             TransactionOutcome = "rolled_back"
)
```

Rules:

- Resolve and snapshot the target before the effect. Record the resolved contract hash and pre-state;
  `count_new_only` without a readable baseline is `indeterminate`.
- The oracle must be isolated from the action channel. A success banner or same-session screen read
  can corroborate, but it cannot be the independent oracle for a consequential write.
- Run the verifier on an isolated copy of the resolved contract and reject any verifier that mutates
  it. A verifier timeout, unreachable record, ambiguous match or incomplete read is `indeterminate`.
- `record_written` requires the exact expected count (default one). It catches missing, phantom and
  duplicate writes; `field_equals` catches a row that persisted but dropped a field.
- Use an idempotency key where the system of record supports one. Resolve it before delivery and
  consult a durable ledger: a repeat under a settled key returns the prior effect outcome and must
  not actuate again; an uncertain ledger entry is `reconciliation_required`. Do not count a
  pre-existing record as new. With `forbid_collateral_loss`, refute if a pre-existing non-matching
  record disappeared.
- Within each consequential step, every declared effect must have exactly one retained verdict.
  Missing evidence is **unknown**, not absence; duplicate evidence must not cover two contracts.
  Verifier-established absence means `Verdict == refuted && Observed == EffectObservedAbsent`; an
  empty evidence list or an unverified delivery failure is not absence.
- A run may claim no effect (`halted_before_effect`, `rejected_policy`, `canceled`,
  `failed_platform`) only when every consequential step has a recorded `not_actuated` stop before
  delivery was attempted or has verifier-established absence for every declared effect. Otherwise
  the outcome is `reconciliation_required`.
- `rolled_back` requires exact, settled evidence that compensation completed; a rollback whose
  outcome is missing or contradictory is `reconciliation_required`.
- No read-back, a mismatch, a duplicate/partial write, collateral loss or an unreachable record is
  `reconciliation_required`; never retry blindly. Create a human reconciliation task when the write
  may exist.
- `completed_unverified` is demo-only: never a production write success, and it must not be counted
  as a verified task success. Only an effect-verified outcome is a production write success.
- Retain a per-step effect journal (contract hash, tier, verdict, privacy-safe evidence digest).
  Effect verification is required before enabling write capabilities; the read-only MVP reserves
  these types but uses UI postconditions.

### 4.9 Metrics and observability

- Core/pack coverage: implemented vs advertised capabilities; unsupported-pack rate.
- Postcondition pass rate, wrong-entity rate, read completeness, scroll coverage/duplicate rate,
  OCR confidence, `ErrUnsupported`/`ErrAmbiguous` rate.
- Effect verdict/tier distribution, exact-coverage rate and reconciliation rate; silent-incorrect-
  success rate (writes) and over-halt rate (all capabilities).
- Stale/ambiguous rate, denial/HITL rate, send verification failures, TCC/helper errors.
- Model coverage, drift age, time-to-update, traffic on verified vs fallback/HITL paths.
- Latency/retry/cancellation per capability and per pack; generated-artifact version mismatch.
- Metrics never contain message text, contact data, QR content or secrets.

## 5. Platform control frameworks

Each platform implements the narrow platform-neutral surface from §2.1 (`Controller` plus optional
interfaces) and optional event/health extensions. App adapters consume it; they do not reimplement
OS mechanics. §2.3 is the summary matrix; this section is the detailed platform contract.

### 5.0 Platform framework contract

Every platform must define the same areas, even if some are unsupported:

| Area | Requirement |
|---|---|
| Observe | accessibility/DOM tree, normalized elements, window/identity, UI hash |
| Act/input | semantic actions first; input events fallback; activation before global input |
| Capture/OCR | window/region capture, format/coordinate space, OCR engine and confidence |
| Focus/window/launch | activate, foreground, multi-window/multi-account, launch/attach |
| Permissions | preflight, denial, secure input/lock screen, refresh |
| Placement/security | in-process/helper/sidecar/operator APK; signing; local IPC token; no remote exposure |
| Health/identity/version | permissions, helper version, app build/UI hash, capability refresh |
| Errors | map platform failures to typed errors (`ErrTCCDenied`, `ErrSecureInput`, `ErrUnsupported`, ...) |
| Fallback | structured → OCR/vision → HITL; no silent downgrade |
| Tests | contract/fixtures, platform integration, E2E matrix |

An unsupported area must return `ErrUnsupported`, not silently degrade.

### 5.1 macOS (MVP)

- **Observe.** AX (`AXUIElement`) tree/attributes/actions; targeted traversal with depth/element
  budgets; virtualized lists are row-template based. Vision OCR for custom-rendered panes.
- **Act/input.** AX actions first, preferably background and window-scoped; `CGEvent` only when AX
  cannot perform the action. Global input requires foreground activation and explicit
  policy/consent. Serialize per app/account; IME/Unicode text needs care.
- **Capture/OCR.** ScreenCaptureKit is the target; the current helper uses
  `CGWindowListCreateImage`. Capture is window-targeted; clip is normalized `(0..1)`; PNG; Retina
  handled by the controller. Vision OCR returns normalized block bounds/confidence.
- **Focus/window/launch.** `NSWorkspace`/`open -b`; activate and raise the selected window; support
  multiple windows/app instances and bind the session to one.
- **Permissions.** Accessibility + Screen Recording. Input Monitoring only for event taps/listening
  (verify per macOS version/feature). Secure input, lock screen and TCC denial are hard stops.
  TCC is tied to signed identity/path; preflight first and only request permission when missing.
- **Placement/security.** Signed Swift/ObjC `computer-use-operator` owns TCC/AX/CGEvent; Go speaks a
  narrow local protocol with a token. No remote exposure. Handle helper crash/restart and version
  handshake.
- **Health/identity/version.** `/health` reports tcc, screen, secure input, capabilities and helper
  version. App identity uses `CFBundleShortVersionString`/`CFBundleVersion`, executable hash and
  UI hash (§3.10).
- **Operator protocol (MVP).**

| Method | Request | Response |
|---|---|---|
| `launch/activate` | bundle ID | pid, window ID/title |
| `list_windows` | pid | windows + titles |
| `identity` | pid | build, executable hash, window |
| `observe` | pid, options (depth, image) | AX snapshot + normalized tree + UI hash |
| `act` | pid, action + resolved target | ok/error |
| `ocr` | pid, optional normalized clip | text + blocks + confidence |
| `screenshot` | pid, optional clip | PNG |
| `focus` | pid, window? | ok |
| `wait/events` | pid, condition/timeout | stable/event result (optional) |
| `health` | none | TCC/screen/secure-input/capabilities/version |

- **Errors.** TCC/screen/secure-input/app-not-running/window-not-found/stale/unsupported/timeout map
  to the typed errors in §4.8.
- **Fallback/limits.** AX gaps and custom panes use OCR/vision; TCC and secure input are hard stops;
  helper rebuilds may require re-consent.
- **Tests.** AX fixtures, TCC denial, secure input, helper restart, activation, virtualized list,
  OCR, multi-window.

**WeCom reference findings.** Live discovery: sidebar `AXTextField` search; an `AXButton` beside it;
a virtualized chat `AXTable`; `AXScrollArea` page actions in the sidebar; a selected row that
identifies the current chat; a custom-rendered message pane that requires OCR. In-chat search, a
native back/leave affordance and a stable conversation id are still `unknown`. These findings are
why WeCom needs its own adapter and a versioned model.

**WeChat.** 4.x exposes only window chrome; it is a separate vision/OCR adapter, not the WeCom
implementation with a different profile.

### 5.2 Windows (post-MVP)

- **Observe.** UIA tree/patterns (Invoke, Value, Toggle, SelectionItem, ExpandCollapse, Scroll,
  Text, Window, Transform); WebView2/CDP or accessibility fallback for embedded content.
- **Act/input.** UIA patterns first; `SendInput` for custom controls; coordinates last. Activate the
  target window; per-monitor DPI awareness; serialize actions.
- **Capture/OCR.** Windows.Graphics.Capture or BitBlt; OCR via Windows.Media.Ocr or a configured
  engine; PNG; normalized clip; DPI handled by the controller.
- **Focus/window/launch.** Foreground activation, multi-monitor/DPI, ShellExecute/process launch;
  bind one window/account per session.
- **Permissions/hard stops.** UAC/secure desktop, elevated windows/integrity mismatch, input
  injection restrictions. These are hard stops or HITL.
- **Placement/security.** Sidecar first (FlaUI/`uiautomation`), signed, local IPC token, no remote
  exposure; native CGo COM later if needed.
- **Health/identity/version.** Sidecar health, UAC/elevation state, app build via FileVersionInfo,
  executable hash and UI hash.
- **Fallback/limits.** UIA gaps, DPI/focus stealing, legacy/Electron/WebView2; use OCR/vision/HITL.
- **Tests.** UIA fixtures, UAC/elevated, DPI/multi-monitor, sidecar restart, focus stealing.

### 5.3 Linux (post-MVP)

- **Observe.** AT-SPI2 over D-Bus; toolkit coverage varies (GTK/Qt/Electron/Flatpak/Snap); X11 vs
  Wayland; DOM/CDP for web content where available.
- **Act/input.** AT-SPI actions; X11 XTest; Wayland via portal/libei/`ydotool`/`wtype`. No
  root/uinput default. IME/Unicode and focus handling are controller concerns.
- **Capture/OCR.** X11 capture; Wayland via PipeWire/xdg-desktop-portal with consent; OCR via
  Tesseract/on-device engine or vision model; normalized clip.
- **Focus/window/launch.** EWMH/compositor-specific window management; desktop entries/exec;
  activate before input.
- **Permissions/hard stops.** Accessibility bus availability, portal consent, sandbox
  (Flatpak/Snap), lock screen/secure input.
- **Placement/security.** In-process or sidecar; no privileged/uinput default; local IPC token.
- **Health/identity/version.** AT-SPI/portal state, package version, executable hash and UI hash.
- **Fallback/limits.** Wayland has no universal input API; per-compositor matrix; AT-SPI gaps.
- **Tests.** X11/Wayland, GNOME/KDE/wlroots, portal consent, toolkit matrix, focus/IME.

### 5.4 Android (post-MVP)

- **Observe.** AccessibilityService tree; UIAutomator for tests; coverage varies for
  Compose/Flutter/React Native/WebView/Canvas.
- **Act/input.** Accessibility actions; ADB input fallback; gestures/multi-touch; IME.
- **Capture/OCR.** MediaProjection consent or `screencap`; on-device OCR (ML Kit) or vision model;
  normalized clip.
- **Focus/window/launch.** `am start`, foreground service, multi-window/work profiles; bind one
  device/account per session.
- **Permissions/hard stops.** Accessibility service enablement, runtime permissions, foreground
  service type, MediaProjection consent, lock screen/secure input, OEM restrictions.
- **Placement/security.** AccessibilityService operator APK + host ADB/control plane; signing,
  version handshake, local IPC; live sessions never migrate across devices.
- **Health/identity/version.** Service/permission state, operator version, `versionName`/
  `versionCode`, APK hash and UI hash.
- **Fallback/limits.** WebView/Canvas/mini-programs, OEM differences, emulator detection.
- **Tests.** AVD/real/cloud devices, OEM matrix, permissions, WebView/CDP, gestures.

### 5.5 Web/Electron

- **Observe.** Go-native CDP: DOM/accessibility tree, targets, network/console; iframes/shadow DOM
  need explicit traversal. Screenshots for canvas.
- **Act/input.** CDP input; DOM focus/fill; `bringToFront`; activation for Electron windows.
- **Capture/OCR.** CDP screenshot; canvas/custom-rendered content uses OCR/vision.
- **Focus/window/launch.** Launch/attach Chromium/Electron with remote debugging; select target/page;
  one profile/`UserDataDir` per account; PWA launch where supported.
- **Permissions/security.** Browser permissions (camera/mic/geolocation), CDP localhost-only,
  token/auth, profile isolation, no remote debugging exposure.
- **Placement/security.** In-process Go CDP; optional external MCP source only via policy proxy;
  never expose raw handles.
- **Health/identity/version.** Browser/Electron version, build id, UI hash, target health.
- **Fallback/limits.** Chromium-only; Electron remote debugging may be disabled; iframes/shadow DOM,
  anti-automation/ToS.
- **Tests.** CDP attach, target selection, iframes/shadow DOM, Electron, PWA/WebMCP.

`chrome-devtools-mcp` remains a **reference/optional external source**, not the SDK architecture:
allowlist tools, validate args, redact output, no implicit postconditions, policy proxy, experimental
only. Stage 1 may use it for Web/Electron discovery; Stage 2 uses Go-native CDP as the primary path.

### 5.6 Primitive, OCR and focus mapping

| Action | macOS | Windows | Linux | Android | Web |
|---|---|---|---|---|---|
| focus/activate | NSRunningApplication/AX raise | UIA/SetForegroundWindow | AT-SPI/compositor | a11y/`am` | CDP bringToFront |
| click | AX/CGEvent | UIA/`SendInput` | AT-SPI/XTest/portal | a11y/ADB | CDP mouse |
| type/fill | AX value/CGEvent | UIA Value/`SendInput` | AT-SPI/XTest | a11y/ADB | DOM/CDP |
| key | CGEvent | `SendInput` | XTest/`ydotool`/`wtype`/libei/portal | ADB/a11y | CDP key |
| scroll | AX/CGEvent | UIA/`SendInput` | XTest/portal | a11y/ADB swipe | CDP wheel |
| capture | ScreenCaptureKit/CGWindow | Windows.Graphics.Capture/BitBlt | X11/PipeWire portal | MediaProjection/screencap | CDP screenshot |
| OCR | Vision | Windows.Media.Ocr/configured | Tesseract/configured | ML Kit/configured | Vision OCR |
| launch | NSWorkspace/`open -b` | ShellExecute/process | desktop entry/exec | `am start` | new tab/URL |
| window/tab | AX/NSWorkspace | UIA/Win32 | EWMH/compositor | a11y/`am` | CDP target |
| clipboard | NSPasteboard | Win32 clipboard | X11/portal | a11y/ADB | CDP/policy |
| file/dialog | NSOpenPanel/AX | IFileDialog/UIA | portal/AT-SPI | SAF/MediaStore | DOM file input |
| wait/events | AX notifications | UIA events | AT-SPI events | a11y events | CDP events |

Semantic actions are preferred over coordinates. Coordinates are a fallback and remain
policy-gated. Linux does not use root/uinput by default.

### 5.7 Limits, version matrix and tests

| Platform | Supported scope | Hard stops | Known limits | Tests |
|---|---|---|---|---|
| macOS | 14/15, Apple Silicon/Intel | TCC, secure input, lock screen | custom panes, helper re-consent, multi-window | AX fixtures, TCC, OCR, activation |
| Windows | 10/11 | UAC/secure desktop, elevated windows | UIA gaps, DPI/focus, WebView2 | UIA fixtures, UAC, DPI, sidecar |
| Linux | X11 + selected Wayland compositors | portal/accessibility denial, lock screen | no universal Wayland input, toolkit gaps | compositor/toolkit matrix, portal |
| Android | selected API levels/OEMs | accessibility/permission denial, lock screen | Compose/Flutter/WebView, OEM | AVD/real/cloud, permissions, WebView |
| Web/Electron | Chrome/Electron with CDP | browser permission denial | iframes/shadow DOM, anti-bot/ToS | CDP attach, targets, Electron |

Each platform must keep a contract test suite, fixture replay, platform integration tests and a
live E2E matrix. Unsupported areas return `ErrUnsupported`; no silent fallback.

## 6. Agent integration

### 6.1 Tools and capability filtering

Three tool families (normative reference: §6.5):

- `app_*` routing tools — `app_list`, `app_delegate`, `app_status`, `app_cancel`; multi-app
  orchestration without exposing every app's tools.
- App semantic tools — generated from the capability manifest. They are organized as a **common
  core + optional capability packs**: `chat_*` is the core for chat-like apps; packs add
  `workspace_*`, `channel_*`, `thread_*`, `message_*`, `search_messages`, `mail_*`, `doc_*`,
  `file_*`, etc. Each app model declares supported packs. Tools are capability-filtered, map to
  guarded primitives, and return typed postconditions.
- `app_*` primitives — `app_observe`, `app_act`, `app_screenshot`, `app_launch`; no app semantics.
  Available only under explicit policy for opaque/exploratory flows.

The capability runtime is domain-neutral; “chat” is one app family, not a universal interface.

**Naming.** Capability IDs (`chat.find`) are used by the manifest, policy and presets; tool names
(`chat_find`) are used by the agent. The compiler maps IDs to tool names and adds
`x-capability-id` to the schema. Details and pack names are in §6.5.0.

**Capability cards.** Every tool is projected from a card: name, summary, `when_to_use`,
`when_not_to_use`, input/output schema, examples and errors. Cards are generated from the manifest;
missing/ambiguous cards are a tool-selection risk (§9.3 metrics).

For normal semantic operation, the model sees structured inputs/outputs, never element ids,
coordinates, OCR clips or raw handles. The explicit `app_*` fallback may expose normalized element
ids/coordinates under policy. Unsupported capabilities are omitted or return `ErrUnsupported`; they
are never “fixed” with prompt workarounds. Exact tool names, parameters, responses and errors are in
§6.5. Which apps and tools are enabled, and the projection formula, are in §6.6.

### 6.2 Agent presets and prompts

- A preset composes: the app model/bundle, the capability tool list, a domain prompt, policy and
  operator/HITL configuration. Presets reference **capability IDs**; the compiler maps them to tool
  names.
- Presets are versioned and pinned to model/capability-schema versions; changing the model or tool
  schema triggers a preset review.
- The prompt contains domain intent (what to find/summarize, read-only rules, untrusted-content
  warnings, when to ask/refuse, do not invent refs, use latest state, handle typed errors), **not**
  UI mechanics (where to click, which OCR clip, which scroll amount).
- WeCom and WeChat have different presets/adapters; they may share generic tools but not interaction
  instructions.
- Multi-app parents use routing tools and subagents; each subagent has its own app preset and sees
  only that app's semantic tools.
- The agent may compose capabilities (find → open → read → scroll → search → leave) under a prompt;
  it cannot expand its authority.
- Each preset needs golden prompts and tool-selection tests (first-tool accuracy, wrong-tool rate;
  §9.3).
- Optional advanced presets may add planner/decider/reflector/memory roles; these are agent-level
  orchestration, not SDK core, and still operate only through typed capabilities.

### 6.3 Playbooks

- A playbook is a versioned deterministic sequence of **capability calls** (not raw UI primitives),
  recorded/reviewed once and replayed cheaply.
- It uses the same adapter, Guard/Policy and postcondition verification as interactive capability
  calls; it is not a second control path.
- It compiles to permissions carrying `Playbook`/`Step`; it cannot grant permissions or bypass HITL.
- Version by app build/UI hash and capability-schema version; include fixtures and E2E tests.
- On selector/state mismatch, escalate to OCR/vision or HITL, or repair offline; do not silently
  continue.
- Use a playbook for repeated stable flows. Use interactive capabilities for adaptive flows and an
  exploratory agent only for long-tail/unsupported apps.
- Playbooks may batch or speculate multi-action sequences to reduce model calls, but must keep
  per-step postconditions and never retry an uncertain effect blindly.
- Playbooks are procedures, not authority.

### 6.4 Login, sessions and HITL

- **Login state machine:** logged-out → challenge (QR/SSO/password/2FA) → authenticated →
  expired/locked. Only `authenticated` exposes app capabilities.
- QR flow: detect the challenge → `QRConsumer` (controlled phone or manual scan) → wait for login →
  verify the main window/account → store session metadata. No credential injection.
- **Account/tenant binding:** verify the active account/avatar/tenant before exposing tools; a wrong
  account stops the session. Multiple accounts/workspaces are separate sessions.
- **Session expiry/re-auth:** re-run the challenge flow; never reuse expired tokens or stale
  handles.
- **Session vault:** encrypt cookies/tokens/handles; define key management/retention; secrets never
  enter model context or logs.
- **Secret substitution, not just redaction:** capability arguments may carry placeholders such as
  `%secret.name%`; the runtime resolves them only at the execution edge and never returns the
  resolved value in a tool result. Response redaction is defense-in-depth, not a security boundary:
  never rely on pattern matching to protect a credential. A secret that cannot be resolved fails the
  call before any input is delivered.
- **Idle TTL/cleanup:** close idle sessions/helpers, release leases, and remove temporary profiles;
  no orphaned sessions.
- **Challenges:** `ChallengeResolver` for CAPTCHA/new-device/step-up; CAPTCHA defaults to stop/HITL;
  biometrics are hard stops unless inside a pre-provisioned trusted envelope.
- **HITL:** confirm/input/takeover with operator routing, channels, pending store, resumption and
  audit. Decisions are allow-once/session/persistent/deny; takeover returns control to the operator
  and resumes with a result. Details in §8.4.
- Writes (send/delete/forward/pay) require explicit permission and HITL where configured; sent-state
  verification/idempotency is in §4.8.

### 6.5 App tool reference

This is the normative tool surface. Tools are **capability-filtered** and **policy-gated**; they
are not raw UI tools.

Three tool families:

| Family | Examples | Purpose |
|---|---|---|
| Routing | `app_list`, `app_delegate`, `app_status`, `app_cancel` | multi-app orchestration without exposing every app's tools |
| Semantic | core + capability packs (below) | app operations the agent normally calls |
| Primitive fallback | `app_observe`, `app_act`, `app_screenshot`, `app_launch` | opaque/exploratory flows, explicit policy only |

#### 6.5.0 Naming and result semantics

| Concept | Example | Used by |
|---|---|---|
| Capability ID | `chat.find` | manifest, policy, preset |
| Tool name | `chat_find` | agent/tool call |
| Pack | `core`, `workspace`, `channel`, `thread`, `message`, `attachment`, `file`, `unread`, `send` | model/pack filtering |
| Error code | `not_found` | tool result |

The compiler maps capability IDs to tool names and adds `x-capability-id` to the tool schema. Policy
and presets use capability IDs; the agent sees tool names.

Tool schemas also carry metadata used by policy and projection:

- `x-tool-kind`: `readOnly` | `input` | `action` | `assertion` (read-only tools may be safe to run
  in the background; input/action tools require a bounded plan and policy);
- `x-capability-group`: an **opt-in operational group** (`core`, `vision`, `pdf`, `devtools`), distinct
  from the semantic `Pack`; an operator can enable a group without exposing it by default;
- `x-postcondition`: a stable postcondition ID referencing the capability contract (never a second,
  free-form definition), or `none` for explicitly exploratory tools.

This is SDK-native metadata, not an MCP-specific feature; MCP projection may mirror it.

**Result semantics.**

- No matches: `ok:true`, empty `hits`, `warnings:["no_matches"]`; not `not_found`.
- Boundary: `ok:true`, `at_top`/`at_bottom` flags; `at_boundary` only for an explicit
  strict/beyond-boundary request.
- Low OCR confidence/partial page: `warnings`; not an error.
- Errors are for real failures: invalid args, missing entity/ref, ambiguity, unsupported,
  not_switched, stale, permission/HITL, timeout.
- Every error should include current `state` when available, so the agent can recover.

**Capability cards.** Every tool is projected from a card:

```go
type CapabilityCard struct {
    Name         string
    CapabilityID string // chat.find
    Summary      string
    WhenToUse    []string
    WhenNotToUse []string
    Input, Output jsonschema.Schema
    Examples     []Example
    Errors       []ErrorCode
}

type Example struct {
    Input  any
    Output any
    Note   string
}
```

Cards must be discriminable; missing/ambiguous cards are a tool-selection bug. Tool selection is
measured with first-tool accuracy, wrong-tool rate, unnecessary calls and task success (§9.3).

**`chat_*` is a minimal common core, not the complete IM surface.** Apps differ in containers,
threads, search syntax, message actions and hierarchy; Slack/Discord/Feishu need additional packs.
Each app model declares which packs it supports; unsupported packs are not registered.

| Pack | Tools (examples) | Typical apps |
|---|---|---|
| Core chat | `chat_find/current/open/read/scroll/search/leave` | WeCom, WeChat, Feishu, Slack, Discord |
| workspace/guild | `workspace_list/switch`, `guild_list` | Slack, Discord |
| channel | `channel_list/open/join/leave/search` | Slack, Discord, Feishu |
| thread | `thread_list/open/read/reply` | Slack, Discord, Feishu |
| message | `message_reply/react/edit/delete/pin/forward` | Slack, Discord, Feishu |
| search | `search_messages` with app-specific filters/operators | Slack, Discord, Feishu |
| attachment | `attachment_list/open/download` | apps with message attachments |
| file | `file_list/open/upload/download` | Files apps, not message attachments |
| presence/unread | `unread_list`, `mark_read` | Slack, Discord, Feishu |
| docs/bot | `doc_open`, `bot_command` | Feishu and similar |

A flat `chat_search {query, scope, limit}` is not sufficient for apps with search operators
(`in:`, `from:`, `before:`, etc.); those apps expose `search_messages` with a `filters` object or an
app-specific search verb.

#### 6.5.1 Common types

`ChatRef` is `EntityRef{Kind: "chat"}`. The full field set is in Appendix D.

```go
type AppInfo struct {
    App                 string
    Platform            string
    Build               string
    Account             string
    Status              string   // ready | degraded | unavailable
    Capabilities        []string // capability IDs
    Packs               []string // core, workspace, channel, thread, ...
    MissingCapabilities []string
    Reason              string   // why unavailable/degraded
}

type TaskStatus struct {
    TaskID  string
    Status  string // pending | running | done | failed | cancelled
    Result  *ToolResult
    Error   *ToolError
    ExpiresAt time.Time
}

type ToolResult struct {
    OK         bool
    State      any
    Data       any
    Evidence   []Evidence
    Confidence float64
    NextCursor *Cursor
    Warnings   []string
    Next       []string
}

type ToolError struct {
    Code          string
    Message       string
    Details       map[string]any
    Retryable     bool
    SuggestedNext []string
    State         any
}

type Evidence struct {
    Kind  string
    Value string
}
```

#### 6.5.2 Response and error envelopes

Success:

```json
{
  "ok": true,
  "state": {"location": "entity", "entity": {"kind": "chat", "key": "chat:...", "name": "..."}},
  "data": {},
  "evidence": [{"kind": "header", "value": "..."}],
  "confidence": 0.92,
  "next_cursor": null,
  "warnings": [],
  "next": ["chat_read", "chat_scroll", "chat_leave"]
}
```

Error:

```json
{
  "ok": false,
  "state": {"location": "search_results"},
  "error": {
    "code": "ambiguous",
    "message": "Multiple chats match the query",
    "details": {"candidates": [{"kind": "chat", "key": "chat:...", "name": "..."}]},
    "retryable": false,
    "suggested_next": ["chat_open"]
  }
}
```

Error codes:

| Code | Meaning | Retryable | Suggested next |
|---|---|---|---|
| `invalid_argument` | schema/parameter error | no | fix arguments |
| `not_found` | entity/ref missing | no | `chat_find`/`chat_open` |
| `ambiguous` | multiple candidates | no | choose a candidate or HITL |
| `unsupported` | app/build/model does not support this | no | stop or HITL |
| `not_switched` | open did not change the current entity | yes | re-observe/retry |
| `at_boundary` | strict/beyond-boundary request | no | use `at_top`/`at_bottom` flags |
| `stale_observation` | state changed since observe | yes | re-observe/retry |
| `delivery_indeterminate` | input may have been posted; final state unknown | no | observe/reconcile first; HITL if unresolved |
| `no_permission` | policy denies the capability/target | no | HITL/host policy |
| `hitl_required` | approval required | yes | complete HITL |
| `model_mismatch` | app build/UI hash has no verified model | no | fallback/HITL |
| `busy` | another action is in progress | yes | retry later |
| `timeout` | operation timed out | yes | retry bounded |
| `internal` | unexpected failure | maybe | report/retry |

`not_found` is for a missing entity/ref, **not** for an empty search result. Empty search results are
`ok:true` with `hits:[]` and `warnings:["no_matches"]`. `at_boundary` is only for an explicit
strict/beyond-boundary request; normal scrolling returns `at_top`/`at_bottom` flags with `ok:true`.

#### 6.5.3 Routing tools

`app_list`

- Purpose: list available apps/accounts and their capability summary.
- Use when: the prompt may involve more than one app, or the host did not pre-bind an app.
- Do not use: inside an app-bound subagent.

| Field | Type | Required | Description |
|---|---|---|---|
| — | — | — | no input |

Response: `{apps: AppInfo[]}`. Example:

```json
{
  "ok": true,
  "data": {"apps": [
    {"app": "wecom", "platform": "macos", "build": "5.0.10", "status": "ready",
     "capabilities": ["chat.find", "chat.open", "chat.current", "chat.read", "chat.scroll",
                      "chat.search", "chat.leave"],
     "packs": ["core", "attachment"]},
    {"app": "slack", "platform": "macos", "build": "1.2.3", "status": "unavailable",
     "missing_capabilities": ["chat.read"], "reason": "no verified model"}
  ]}
}
```

`Status`: `ready` = verified model + all advertised capabilities available; `degraded` = model
available but some capabilities missing; `unavailable` = no model/permission/app. `Reason` and
`MissingCapabilities` explain the gap.

`app_delegate`

- Purpose: delegate a goal to an app-bound subagent/session; only that subagent sees the app's
  semantic tools.
- Use when: a goal requires operating a specific app; multiple apps are handled by multiple calls.
- Side effects: starts/uses a session; no app action happens until the subagent acts.

| Field | Type | Required | Description |
|---|---|---|---|
| `app` | string | yes | app id from `app_list` |
| `goal` | string | yes | natural-language goal for the subagent |
| `wait` | bool | no | wait for completion; default false (async) |
| `timeout_ms` | int | no | bound for `wait=true` |
| `idempotency_key` | string | no | avoid duplicate delegation |
| `max_result_bytes` | int | no | cap the returned result |

Response: `{task_id, status, expires_at}`; if `wait=true`, include `result`/`error`. Errors:
`not_found` (unknown app), `unsupported`, `no_permission`, `model_mismatch`, `busy`, `timeout`,
`cancelled`.

Task lifecycle rules:

- Tasks have a TTL (`expires_at`); expired results are discarded.
- A host limits concurrent tasks/queue depth; excess returns `busy`.
- Results are capped; large payloads must use file/resource references.
- `idempotency_key` deduplicates repeated delegate calls.
- A subagent failure is reported as `TaskStatus.failed` with `ToolError`.
- Parent exit does not orphan a task; the host either cancels or persists it.

`app_status`

- Purpose: poll/retrieve a delegated task.
- Input: `{task_id: string}`.
- Response: `TaskStatus` (`pending|running|done|failed|cancelled`).
- Errors: `not_found`, `timeout`.

`app_cancel`

- Purpose: cancel a delegated task.
- Input: `{task_id: string}`.
- Response: `TaskStatus` with `cancelled`.
- Errors: `not_found`, `busy`, `unsupported`.

Multi-app example:

```text
app_list {}
app_delegate {app:"wecom", goal:"找到 X 的讨论并总结", wait:false, idempotency_key:"wecom-x"}
app_delegate {app:"slack", goal:"找到 Y 的讨论并总结", wait:false, idempotency_key:"slack-y"}
app_status {task_id:"..."}  // repeat until done
app_cancel {task_id:"..."}  // if the parent no longer needs it
```

The parent agent never receives WeCom and Slack semantic tools at the same time.

#### 6.5.4 Core chat tools

This is the common core for chat-like apps; capability packs extend it. An app model may omit a
core tool if the app genuinely does not support it (for example, no in-chat search).

| Tool | Class | Side effect | Default availability |
|---|---|---|---|
| `chat_find` | read | none (search UI may be transient) | yes |
| `chat_current` | read | none | yes |
| `chat_open` | navigate | selects a chat | yes |
| `chat_read` | read | none | yes |
| `chat_scroll` | read | scrolls the message pane | yes |
| `chat_search` | read | opens/closes in-chat search | if supported |
| `chat_leave` | navigate | closes overlays/clears selection | if supported |
| `chat_send` | input | sends a message | post-MVP, HITL only |

`chat_find`

- Purpose: find chats or message hits; returns candidates with evidence. The agent chooses when
  ambiguous.
- Use when: the user references a chat/content that is not already current.
- Do not use: to read messages; use `chat_read` after `chat_open`.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `query` | string | yes | — | name/preview/message text |
| `scope` | enum `all`,`chats`,`messages` | no | `chats` | search chats, message hits, or both |
| `limit` | int | no | 20 | max hits |

Response: `{hits: SearchHit[], state, warnings}`. Each hit has `ref`, `kind`
(`chat|message|contact|document`), `snippet`, `time`, `score`. Scope constrains kinds: `chats` →
chat/contact; `messages` → message; `all` → mixed. No matches are `ok:true`, `hits:[]`,
`warnings:["no_matches"]`, not `not_found`. Errors: `ambiguous`, `unsupported`,
`invalid_argument`.

`chat_current`

- Purpose: return the current chat and confidence.
- Input: none.
- Response: `{chat: ChatRef, confidence: number, state}`.
- Errors: `ambiguous` when header and selected row disagree.

`chat_open`

- Purpose: open/select a chat and verify the switch.
- Input: `{ref: ChatRef}` (required).
- Response: `{chat: ChatRef, state, confidence}`. Postcondition: `Current == ref`.
- Errors: `not_found`, `not_switched`, `stale_observation`, `unsupported`.

Request:

```json
{"name":"chat_open","arguments":{"ref":{"kind":"chat","key":"chat:...","name":"POE Website"}}}
```

Response:

```json
{"ok":true,"state":{"location":"chat","entity":{"kind":"chat","key":"chat:..."}},
 "data":{"chat":{"kind":"chat","key":"chat:...","name":"POE Website"}},"confidence":0.97}
```

`chat_read`

- Purpose: read the visible message page.
- Use when: the current chat is open and the user wants content; use `chat_scroll` for history.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `ref` | ChatRef | no | current | must match current if provided |
| `cursor` | Cursor | no | current | page anchor from a previous read/scroll |
| `limit` | int | no | 50 | max messages |

Response: `Page{entity, messages, cursor, at_top, at_bottom, warnings}`; messages include
sender/time/text/direction/attachment metadata. `warnings` may include `low_confidence` and
`partial_page`; these are not failures. Errors: `not_found`, `stale_observation`, `unsupported`.

`chat_scroll`

- Purpose: page through history.
- Input: `{direction: older|newer|top|latest, pages?: int (default 1), ref?: ChatRef}`.
- Response: `Page`; postcondition: content advanced or `at_top`/`at_bottom` flags. `at_boundary` is
  returned only for an explicit strict/beyond-boundary request.
- Errors: `stale_observation`, `unsupported`, `timeout`.

`chat_search`

- Purpose: search messages in the current context (chat/channel/thread/workspace/global).
- Input: `{query: string, scope?: current|container|workspace|global (default current), filters?: object, limit?: int}`.
- Response: `{hits: SearchHit[], cursor?, warnings}`; hits include snippets and positions when
  available. No matches are `ok:true`, `hits:[]`, `warnings:["no_matches"]`.
- Errors: `unsupported` (scope not supported), `ambiguous`, `invalid_argument`; never fake with a
  full-pane OCR guess.

`chat_leave`

- Purpose: leave the current context: close search/modals, then return to the parent location.
- Input: none.
- Response: `{location: Location}`; postcondition: overlays closed and parent state verified.
- Errors: `unsupported`, `busy`, `not_switched`.

`chat_send` (post-MVP, HITL)

- Purpose: send a message; disabled in the read-only MVP.
- Input: `{ref: ChatRef, text: string, confirm?: bool}`.
- Response: `{message_id?, state}`; postcondition: message appears in the conversation.
- Errors: `no_permission`, `hitl_required`, `not_found`, `timeout`, `internal`; never retry blindly.

**Optional capability packs.** Pack tools use the same response/error envelope and policy model;
exact parameters are profile-specific. Illustrative signatures:

`EntityRef` is used for workspace/channel/thread/chat; `FileRef` is `EntityRef{Kind:"file"}` plus
MIME/name/size metadata.

| Pack | Tool | Input | Output |
|---|---|---|---|
| workspace | `workspace_list` | `{}` | `EntityRef[]` |
| workspace | `workspace_switch` | `{ref: EntityRef}` | `{workspace: EntityRef, state}` |
| channel | `channel_list` | `{workspace?}` | `EntityRef[]` |
| channel | `channel_open` | `{ref: EntityRef}` | `{channel: EntityRef, state}` |
| channel | `channel_join` / `channel_leave` | `{ref: EntityRef}` | `{state}` |
| thread | `thread_list` | `{channel?, limit?}` | `EntityRef[]` |
| thread | `thread_open` | `{ref: EntityRef}` | `{thread: EntityRef, state}` |
| thread | `thread_read` | `{ref, cursor?, limit?}` | `Page` |
| thread | `thread_reply` | `{ref, text, confirm?}` | `{message?, state}` (HITL) |
| search | `search_messages` | `{query, workspace?, channel?, thread?, filters?, limit?}` | `SearchHit[]` |
| message | `message_react` | `{message_ref, emoji}` | `{state}` |
| message | `message_reply` | `{message_ref, text, confirm?}` | `{message?, state}` |
| message | `message_edit` / `message_delete` | `{message_ref, text?}` | `{state}` (HITL) |
| attachment | `attachment_list` | `{message_ref?}` | `EntityRef[]` |
| attachment | `attachment_open` / `attachment_download` | `{ref}` | `{file: FileRef, state}` |
| file | `file_list` / `file_open` | `{ref?}` | `EntityRef[]` / `{file: FileRef, state}` |
| file | `file_upload` / `file_download` | `{ref, path?}` | `{file: FileRef, state}` |
| unread | `unread_list` | `{workspace?, channel?}` | `EntityRef[]` |
| unread | `mark_read` | `{ref}` | `{state}` |
| send | `send` | `{ref, text, confirm?}` | `{message?, state}` (post-MVP/HITL) |

Examples of app composition:

- **WeCom/WeChat**: core chat + attachment; no workspace/thread packs.
- **Slack**: core + workspace + channel + thread + message + search + attachment + file + unread.
- **Discord**: core + guild/channel + thread/forum + message + search + attachment + file + unread.
- **Feishu**: core + chat/thread + doc + bot + message + search + attachment, depending on account role.

#### 6.5.5 Other app families

Use the same verbs with a domain prefix:

| Domain | Examples |
|---|---|
| Mail | `mail_find`, `mail_open`, `mail_read`, `mail_scroll`, `mail_search`, `mail_leave`, `mail_send` |
| Documents | `doc_find`, `doc_open`, `doc_read`, `doc_scroll`, `doc_search`, `doc_leave` |
| Files | `file_find`, `file_open`, `file_upload`, `file_download` |

The capability runtime is domain-neutral; do not force chat semantics into non-chat apps. Message
attachments use `attachment_*`; the Files app family uses `file_*`.

#### 6.5.6 Primitive fallback tools

Available only under explicit policy for opaque/exploratory flows:

| Tool | Input | Output | Notes |
|---|---|---|---|
| `app_observe` | `{app?, depth?, max_elements?, boxes?, include_image?, query?}` | normalized observation | bounded; `query` finds within the snapshot |
| `app_act` | `{kind, element?, observation_id?, text?, key?, point?, ...}` | ok/error | policy-gated; no raw handles to the model |
| `app_screenshot` | `{clip?}` | image/file reference | heavy asset as file/resource |
| `app_launch` | `{app, account?}` | `AppInfo` | host/operator policy |

#### 6.5.7 Availability and filtering

- A tool is registered only if its capability is `verified`, supported by the detected app
  build/UI hash, allowed by policy, and compatible with the current role/read-only mode.
- Policy/preset matching uses capability IDs; the schema carries `x-capability-id`; the agent sees
  tool names.
- Tools are session-scoped: an app-bound subagent sees only that app's semantic tools.
- `next` in the response is a hint, not a hard restriction; tool arguments are always validated.
- On app update/restart, the tool set is refreshed after re-resolving the model; stale tools are
  removed. MCP exposures refresh `tools/list` and schema versions.
- Tool count should stay small (target ≤20 per session). For larger catalogs, use capability packs
  or in-app capability search; do not dump all apps' tools into one session.
- Tool selection is measured with first-tool accuracy, wrong-tool rate, unnecessary calls and task
  success (§9.3); ambiguous cards/names are bugs.

### 6.6 Configuration model

Which apps an agent can operate, and which tools it can use, is **configuration**, not model
discretion. Five layers decide the final tool list:

| Layer | Decides | Examples | Owner |
|---|---|---|---|
| Host/deployment | enabled apps, platforms, accounts, devices, model-registry paths | app inventory, client options, `ConfigDirs` | host operator |
| App Control Model / registry | capabilities supported by app × platform × build/UI hash; schemas/pre/post/errors | model bundle, registry entry | app owner |
| Policy | allowed/denied capabilities, read/write, HITL, limits, target rules | allow/deny/hitl/rate/expiry | security/host |
| Agent preset | which capability packs/tools this agent sees; prompt; multi-app routing | research vs assistant vs multi-app | agent owner |
| Session | binds app/account/model and projects the final tool list | session config | host |

**Projection formula.**

```text
advertised_tools =
    verified_capabilities
  ∩ model_supported          # app model/manifest
  ∩ policy_allowed           # Guard/Policy
  ∩ preset_exposed           # agent preset/tool groups
  ∩ role/account_supported   # account/tenant/role
```

If a capability fails any step, it is not registered, or it returns `Unsupported`/`NoPermission`.
The agent cannot add apps, capabilities or tools; it only composes the advertised set. A prompt can
express intent but cannot grant authority.

**Host/app inventory (example).**

```yaml
schema_version: 1
apps:
  - id: wecom
    platform: macos
    account: work
    model: apps/wecom/macos/5.0.10/default
    policy: policies/wecom-readonly.yaml
  - id: slack
    platform: macos
    account: work
    model: apps/slack/macos/1.2.3
    policy: policies/slack-readonly.yaml
```

The inventory selects **which app/account/model/policy**; it does not list every tool. Capabilities
come from the model, permissions from policy, and exposure from the preset. Multiple
accounts/workspaces are separate inventory entries. Policy/preset entries use capability IDs
(`chat.find`); the compiler maps them to tool names (`chat_find`).

**Policy (example).**

```yaml
schema_version: 1
capabilities:
  allow: [chat.find, chat.open, chat.current, chat.read, chat.scroll, chat.search, chat.leave]
  deny:  [chat.send, message.delete, message.forward]
  hitl:  []
```

**Agent preset (examples).**

```yaml
# read-only research agent
agent: research
apps: [wecom]
semantic_tools: [chat.find, chat.open, chat.current, chat.read, chat.scroll, chat.search, chat.leave]
```

```yaml
# post-MVP assistant: write capability is enabled only with HITL
agent: assistant
apps: [wecom]
semantic_tools: [chat.find, chat.open, chat.read, chat.scroll, chat.send]
hitl: [chat.send]
```

```yaml
# multi-app parent: routing only; each subagent gets its own semantic tools
agent: multi-app-research
apps: [wecom, slack, feishu]
tools: [app_list, app_delegate, app_status]
subagents:
  wecom:  {preset: wecom-research}
  slack:  {preset: slack-research}
  feishu: {preset: feishu-research}
```

**Go SDK mapping (illustrative).**

- `ClientOptions` / `ConfigDirs`: host inventory, model-registry paths and policy locations.
- `AgentConfig`: agent preset, prompt and allowed tool groups.
- `SessionConfig.Tools`: the host-generated, session-bound tool list (projection result).
- `pkg/apps` Guard/Policy: enforcement; the adapter cannot bypass it.
- Model registry: selects the bundle/adapter for the detected app identity.

```go
client, err := agent.NewClient(&agent.ClientOptions{
    Root:        stateDir,
    ConfigDirs:  configDirs,
    ModelInfo:   modelInfo,
    Credentials: credentials,
    Model:       modelID,
})
if err != nil { return err }
defer client.Close()

if err := client.RegisterAgent("wecom", agent.AgentConfig{System: domainPrompt}); err != nil {
    return err
}

// Host-side projection: verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role.
tools := projectTools(bundle, policy, preset)

session, err := client.CreateSession(ctx, &agent.SessionConfig{
    Agent: "wecom",
    Tools: tools,
})
if err != nil { return err }
defer session.Close()
```

**Rules.**

- Configuration is authority; the prompt is only intent.
- A capability must be verified, model-supported, policy-allowed and preset-exposed to become a
  tool. Missing at any step means `Unsupported`/omitted.
- Capability IDs are the configuration unit; tool names are a projection. Policy/preset validation
  checks IDs against the selected model.
- Config files carry `schema_version`; invalid/unknown fields fail closed.
- Multi-app agents use routing tools and subagents; the parent does not receive every app's
  semantic tools.
- Config changes require recomputing the tool list and refreshing the session; changes are audited.
- Fail closed on missing/invalid policy or model; never silently broaden the tool set.
- The model cannot create apps, tools, permissions or capability packs at runtime.

## 7. MVP: macOS WeCom

### 7.1 MVP definition and non-goals

**MVP definition.**

- Platform: macOS 14/15, Apple Silicon/Intel, logged-in GUI session.
- App: 企业微信 (WeCom, `com.tencent.WeWorkMac`), **WeCom only**.
- Target build/UI hash: recorded per build (for example WeCom 5.0.x), pinned in the model registry.
- Target environment: enterprise account/tenant, `zh-CN`, light theme, recorded default window size
  plus one resized variant.
- Mode: **read-only**.
- In scope: `chat.find`, `chat.current`, `chat.open`, `chat.read`, `chat.scroll`, optional
  `chat.search`/`chat.leave`, screenshot/OCR fallback, policy/audit.
- No WeCom API dependency for desktop control; official APIs are an optional post-MVP data/action
  source.
- WeChat (`com.tencent.xinWeChat`) is a **post-MVP** separate vision/OCR adapter, not a WeCom
  profile.

**Out of scope.**

- WeChat, other platforms, other accounts/tenants, multi-app routing.
- send/delete/forward/pay, bulk export, attachment open/download.
- mini-programs, protocol RE, anti-bot evasion.
- WeCom official API, MCP/HTTP/A2A exposure, non-core capability packs
  (workspace/channel/thread/attachment/file/unread/send).

### 7.2 Prerequisites and developer inputs

- Signed `computer-use-operator`; Accessibility + Screen Recording TCC; preflight before prompting;
  helper token/health.
- A logged-in test account/tenant with non-production data.
- 5–10 golden goals with known answers, including duplicate names, empty chat, long history,
  at-top/at-bottom and stale-search cases.
- Target WeCom build/UI hash and support window (for example latest N builds).
- Optional recorded demonstration/event trace of the golden flows; treated as a hypothesis source.
- Developer decisions: identity/ambiguity rules, meaning of leave/search/read scope, non-goals.
- Developer gates G0–G7; no destructive probes.

### 7.3 Stage 1 plan (WeCom model)

**Commands and artifacts.**

```bash
appmodel discover --app wecom --platform macos --build <build> \
  --goals goals.yaml --out apps/wecom/macos/<build>/<variant>
appmodel probe    --app wecom --platform macos --build <build> \
  --scenario scenarios/open-chat.yaml --record
appmodel coverage --map apps/wecom/macos/<build>/<variant>/interaction-map.yaml
appmodel drift    --map apps/wecom/macos/<build>/<variant>/interaction-map.yaml --live
appmodel validate --bundle apps/wecom/macos/<build>/<variant>
```

**Plan.**

1. **Dossier.** Bundle/build, TCC permissions, AX+vision channels, account/tenant, locale/theme,
   window size.
2. **Interaction map.** Sidebar/search/chat table/current chat/message pane/scroll; record
   `unknown` for in-chat search, back/leave and stable id.
3. **Entity model.** Chat (name, preview, time, selected, unread, identity confidence) and message
   (sender, time, text, direction, attachment metadata).
4. **Identity strategy.** Prefer native id if AX exposes it; else name+avatar, then
   name+preview+time, then position. Ambiguity → `ambiguous`/HITL; never silently choose.
5. **Search taxonomy.** Classify chat/contact/message/document hits; global/sidebar search vs
   in-chat search; debounce/stale results.
6. **Virtualized list.** Do not enumerate all rows; use native search for recall; local AX rows are
   only a ranking/fallback aid.
7. **Scroll discovery.** Locate the message scroll container; verify AX page action or wheel;
   define `AtTop`/`AtBottom` and dedupe.
8. **OCR clip.** Derive from the message pane bounds, not a hardcoded fraction; handle Retina and
   window resize; record confidence.
9. **Leave semantics.** Decide: close search/modals, clear selection/focus chat list, or
   `unsupported`. Do not assume a native back button.
10. **Capability manifest.** Core: `chat.find/current/open/read/scroll`, optional
    `chat.search`/`chat.leave`; pre/post/errors/policy class; no attachment/file tools in MVP.
11. **Coverage/fixtures.** Golden goals × paths; duplicate names, empty chat, long history,
    at-top/bottom, modal/login, stale search.
12. **Exit (G1/G2).** Every in-scope goal has a path; unknown/unsupported declared; bundle
    validated; developer approves identity/leave/search semantics.

**Unknown gating.** P2 cannot start until:

- the scroll container is found and verified; otherwise `chat.scroll` is `unsupported`;
- leave semantics are decided; otherwise `chat.leave` is not registered;
- in-chat search availability is decided; otherwise `chat.search` is not registered;
- the identity fallback/ambiguity policy is approved.

### 7.4 Stage 2 plan (WeCom adapter)

- **State machine:** Attached → ChatList → SearchResults/Chat → optional ScopedSearch; Modal on top.
- **Core adapter:** `chat.find/current/open/read/scroll` plus optional `chat.search`/`chat.leave`.
- **Find:** native sidebar search; classify hits; debounce/stale handling; ranking/ambiguity; local
  AX list only as fallback.
- **Current:** selected row + header cross-check; return entity/container/confidence.
- **Open:** select row/result; wait for header/selected row; wait for message load; verify
  `Current == ref`.
- **Read:** OCR message pane; reconstruct sender/time/direction; attachment metadata only; dedupe;
  cursor; `warnings` for low confidence/partial page.
- **Scroll:** message scroll container; AX page action or wheel; wait stable; dedupe;
  `AtTop`/`AtBottom`; jump to latest.
- **Search:** optional; if the app/build has no in-chat search, do not register `chat_search`.
- **Leave:** close search/modals; clear selection/focus chat list; verify parent state; if no
  reliable path, do not register `chat_leave`.
- **Tools:** capability IDs → `chat_*`; read-only projection; no raw element ids/coordinates/OCR
  clips.
- **Errors:** no matches = `ok:true`, `warnings:["no_matches"]`; boundary = flags; real failures use
  typed errors; low confidence is a warning.
- **Tool selection:** `chat_find` (locate chat/content) vs `chat_search` (inside current context) vs
  `chat_read`/`chat_scroll`; cards carry when-to-use/when-not-to-use.
- **Tests:** contract + fixture replay locally; live canary belongs to P4.

### 7.5 Acceptance

1. Launch/attach; missing TCC/helper → diagnostic, no silent failure; helper health reports
   TCC/screen/secure-input.
2. Detect build/UI hash; model registry match; mismatch fails closed or uses a configured fallback.
3. Find a chat by prompt via native search; candidates + evidence; ambiguous names →
   `ambiguous`/HITL.
4. Open direct/group chat; `Current == ref` verified from header + selected row.
5. Read visible page with sender/time/text/direction; attachments listed, not opened/downloaded.
6. Scroll older/newer/top/latest; cursor/dedupe; `AtTop`/`AtBottom` flags.
7. In-chat search implemented **or** explicitly not registered/`unsupported`; never faked with
   full-pane OCR.
8. Leave closes overlays and returns to ChatList, or is explicitly unsupported/not registered.
9. Read-only: no send/delete/forward; denied actions audited; no raw handles/PIDs to the model.
10. Default-deny/read-only policy active; permission/HITL behavior audited.
11. App restart/PID change/multi-window: re-resolve identity/model; no stale session.
12. Modal/login/update/stale-search recovery: detect, re-observe, bounded retry or HITL.
13. Privacy: screenshots/OCR/fixtures redacted; no credentials/QR/message bodies in logs.
14. The agent completes the flow with a generic prompt and `chat_*` tools; no WeCom-specific
    click/OCR/AX instructions.
15. Model registry entry, fixtures, coverage and unknown/unsupported lists exist.
16. Latency/cost budgets: initial targets for observe/open/read/scroll p95 and token/latency are
    recorded and tracked.
17. Tool selection: first-tool accuracy and wrong-tool rate measured on golden prompts.
18. Reliability: wrong-chat rate 0 on the golden set; postcondition ≥99%; read completeness ≥95%;
    scroll duplicate <1%.
19. Window/background: reads and opens are bound to the target window; background AX is used where
    supported; foreground/global input only under policy and without focus theft during reads.

**Demo commands.**

```bash
go run ./examples/computer-use-wecom --show-names --ocr
go run ./examples/computer-use-wecom-agent --prompt "找到关于 X 的聊天并总结最近消息"
```

### 7.6 Milestones and exit criteria

| Phase | Content | Exit |
|---|---|---|
| **P0** | Signed operator, TCC/AX/screenshot/OCR, default-deny + audit | health/TCC/AX/OCR smoke; audited denial |
| **P0.5** | Discovery harness: snapshot/probe/diff/fixtures/coverage/drift | produces a validated bundle for WeCom |
| **P1** | Stage 1 WeCom model: dossier/map/identity/manifest/coverage/fixtures | G1/G2 approved; unknowns resolved or marked optional |
| **P2** | Stage 2 WeCom core adapter: find/current/open/read/scroll + optional search/leave; fixture replay/contract tests | G3/G4; local smoke; no wrong-chat |
| **P2.5** | Registry/drift baseline: model registry, build/UI-hash detection, drift automation | registry/drift checks pass |
| **P4** | Advanced policy templates/DevBypass, live canary E2E, metrics thresholds, rollout/rollback | G5/G6; read-only MVP accepted |
| **P3 (post-MVP, after P4)** | Composer + send with permission/HITL; deny/verify; no duplicate send | write enablement only |

P2 uses contract/fixture replay and local smoke; P4 is the first live canary/metrics gate. Policy
and audit are implemented in P0/P1; P4 adds advanced templates and rollout. Post-MVP phases P5–P8
(WeChat/apps, platforms, playbooks/remote, full maintenance) are defined in §11; this table is the
read-only MVP path.

### 7.7 MVP risks, rollout and rollback

Full risk register: §12. MVP-specific risks:

- WeCom UI/version drift; virtualized chat list; OCR loss; identity ambiguity; TCC/secure input;
  helper re-consent; message pane paint timing.
- Rollout: canary account/build → compare wrong-chat/read/scroll/latency → gradual rollout.
- Rollback: keep the previous model/adapter; revert registry entry; invalidate sessions.
- Maintenance: support window, drift reports, fixture refresh, re-discovery triggers.

## 8. Safety, privacy and policy

### 8.1 Threat model

- **Content/tool injection:** app content, tool descriptions, MCP tools, design sources and
  fixtures can contain adversarial instructions; content is data, never authority.
- **Identity/tenant:** wrong entity/account/tenant, ambiguous names, stale search, wrong window.
- **Data exfiltration:** screenshots/OCR, clipboard, exports, attachments, logs, fixtures and
  model-provider retention.
- **Write/destructive:** send/delete/forward/pay, file upload, bulk export.
- **Model/version drift:** stale model, UI hash mismatch, A/B/server-driven UI, helper update.
- **Credential/session:** QR, tokens, cookies, vault, helper/sidecar/operator APK IPC token.
- **Cross-session leakage:** multi-app subagents, multiple accounts/workspaces.
- **Resource abuse:** tool loops, large screenshots, OCR/vision cost, delegate queue exhaustion.
- **Audit integrity:** tampering, deletion, over-retention.
- **Platform permissions:** TCC/UAC/portal/Android denial or silent revocation.
- **Legal/ToS:** host-owned use case and account risk.

### 8.2 Permissions, guard and audit

- Explicit allow-list matching by capability ID and/or primitive target. A target can match app,
  window, role, label, selector, key, region or `EntityRef`.
- Background AX actions may be allowed independently; foreground/global input requires explicit
  policy/consent.
- `Session` is the single execution choke point; denied actions never reach the platform driver.
- Capability authorization uses a **bounded primitive plan** (§2.4). High-risk capabilities may
  require per-primitive grants via policy.
- Grants are single-use/TTL-bound; permissions may carry max-uses, rate and expiry limits.
- JIT approval/remember/bypass are host policy; bypass is explicit, scoped, expiring, audited and
  never model-controlled.
- Capability classes map to permissions (`read`, `navigate`, `input`, `send`, ...); writes require
  HITL where configured.
- `Effective = verified ∩ model_supported ∩ policy_allowed ∩ preset_exposed ∩ role` (§6.6).
- Installed ≠ allowed; subagents cannot escalate.
- Policy validation rejects empty/ambiguous targets and unsafe bypass.

```go
type Permission struct {
    ID           string
    Capabilities []string // capability IDs; empty = primitive-only
    Packs        []string
    Class        ActionClass
    Target       TargetMatcher
    Limits       Limits
    HITL         bool
}

type Grant struct {
    PermissionID  string
    Capability    string
    Plan          []Step
    Target        ActionTarget
    ObservationID string
    ModelRef      ModelRef
    SessionID     string
    ExpiresAt     time.Time
    Used          bool
}

type AuditEvent struct {
    Time         time.Time
    SessionID    string
    ModelRef     ModelRef
    Capability   string
    Plan         []Step
    Target       ActionTarget
    PermissionID string
    Result       string // executed | denied | failed
    Postcondition string
    HITL         string
    Bypass       bool
    ErrorCode    string
    Duration     time.Duration
}
```

`TargetMatcher`, `Limits`, `ActionTarget` and `Scope` are defined in `pkg/apps`; `ModelRef` is in
§3.10; `Step` is in Appendix D. Audit events must be content-minimized: no message text, secrets, QR
content or raw handles.

### 8.3 Data handling

- Redact PII before observations reach the model; minimize screenshots/OCR sent to providers.
- Define provider retention/training policy; prefer local OCR/vision where required.
- Fixtures/screenshots may contain sensitive data: test account, redaction, encryption, access
  control and retention.
- Never log secrets, QR content, tokens, credentials or full chat bodies; redact tool args/results.
- Tool results substitute secret values (secret refs) before reaching the model; credentials are
  filled host-side from the vault where possible.
- Clipboard/export/attachment upload are policy-gated; bulk export is out of scope for the read-only
  MVP.
- Audit logs: tamper-evident, access-controlled, retention-policy bound.
- Session vault: encryption at rest, key management/rotation, per-account isolation.
- Test accounts and fixtures are isolated and cleaned up; no production PII.

### 8.4 HITL

- Confirm/input/takeover with operator routing, channels, pending store, resumption and audit.
- Use for login challenges, irreversible actions, wrong-entity ambiguity and operator takeover.
- HITL does not bypass platform permissions or Guard.

```go
type ApprovalDecision string // allow_once | allow_session | allow_persistent | deny

type ApprovalRequest struct {
    ID             string
    Scope          Scope
    CapabilityID   string
    Target         ActionTarget
    Plan           []Step
    RequestedBy    string
    ApproverRole   string
    Channel        string // web | chat | email | phone | operator_console
    Reason         string
    ExpiresAt      time.Time
    IdempotencyKey string
}
```

- Channels/routing/escalation/SLA are host policy.
- Pending requests are persisted and resumable (agentd/A2A `input-required`).
- Approver identity/role is authenticated; duplicate approvals are idempotent.
- Deny/timeout stops the action and is audited; takeover re-observes before continuing.

## 9. Testing and validation

### 9.1 Test matrix

| Layer | Scope | Examples |
|---|---|---|
| Stage 1 | schema, evidence, coverage, drift | bundle schema/referential integrity, probe repeatability, provenance, drift/report format, fixture redaction |
| Stage 2 | tool projection, core/packs, policy | capability ID→tool name, `x-capability-id`, cards, contract/postconditions, pack availability, policy/HITL, error mapping |
| Integration | platform/build/account/locale/window | model mismatch, restart/PID change, multi-window, multi-account, locale/theme/layout |
| Adversarial | bypass/injection/leakage | app/MCP/design/tool-description injection, cross-session, raw handles, wrong entity/account, resource exhaustion, IPC token, OCR misread |
| E2E | golden goals | live app, canary, rollback |
| Performance | latency/cost | observe/open/read/scroll p50/p95, OCR/vision/token cost |
| Security | permissions/exfiltration/audit | denied-before-controller, redaction, audit integrity |
| Maintenance | drift/version | fixture refresh, compatibility matrix, deprecated models |

### 9.2 Stage 1 validation

- Schema and referential integrity: capability → interaction/state/transition → coverage → fixture.
- Probe repeatability (for critical transitions, ≥3 runs), timing, non-determinism and failure notes.
- Evidence/provenance/confidence review; unknown/unsupported explicit.
- Coverage matrix: every in-scope goal has a path and capability.
- Drift reports reviewed; design/runtime conflicts recorded.
- Fixtures are redacted, versioned and replayable.

### 9.3 Stage 2 validation

- Tool projection: capability ID/name/schema/`x-capability-id`/card.
- Capability cards and tool selection: first-tool accuracy, wrong-tool rate, unnecessary calls.
- Contract tests for core and packs: pre/post/errors.
- Policy/HITL tests: denied-before-controller, allow-once/session/persistent, takeover.
- Error mapping and recovery: no_matches vs not_found, boundary flags, stale/wrong entity, modal.
- Effect tests (post-MVP writes): inject no-land/partial/duplicate/wrong-record/other-record,
  unreachable/timeout and verifier-mutation faults; assert pre-state, evidence tier, three-valued
  verdict, exact coverage, idempotent replay (same key does not re-actuate) and outcome
  (`reconciliation_required`); missing evidence is never treated as absence; never retry blindly.
- Multi-window/account/restart; generated artifact version; privacy/redaction.
- Performance: latency/token/cost budgets; concurrency/serialization.
- MCP edge: external tools allowlisted/proxied; server projection/filtering.

### 9.4 Metrics and definitions

- **Wrong-entity rate:** wrong reads/writes ÷ total entity operations.
- **Silent-incorrect-success rate (write):** runs that report success while the effect is wrong or
  missing ÷ runs that report success. Target 0 on the golden set.
- **Over-halt rate:** runs that halted although the correct action was available ÷ runs where the
  correct action was available. Reported alongside the silent-incorrect rate so safety is not bought
  with unusable halts.
- **Effect verification coverage:** consequential declared effects with exactly one retained verdict
  ÷ all declared effects.
- **Postcondition pass rate:** passed postconditions ÷ executed capabilities.
- **Read completeness:** known messages read ÷ known messages in the target window.
- **Scroll duplicate rate:** duplicate messages ÷ total returned messages across pages.
- **First-tool accuracy:** correct first tool ÷ golden prompts.
- **Wrong-tool rate:** incorrect tool calls ÷ total tool calls.
- **Latency/cost:** p50/p95 and token/OCR/vision cost per capability.
- **Observation budget:** elements/text tokens, snapshot depth/boxes, full-dump avoidance.
- **Unsupported/ambiguous rate:** per capability and pack.
- **Model:** supported builds/platforms, drift age, time-to-update, verified-model traffic,
  fallback/HITL rate, deprecated-model usage.
- Thresholds: wrong-entity 0 and silent-incorrect-success 0 on the golden set; postcondition ≥99%;
  read ≥95%; scroll duplicate <1%; first-tool target calibrated per app (initially ≥95%); over-halt
  calibrated per app after the first live canary; latency/cost budgets from §7. Thresholds are
  calibrated on the app's golden set and approved at G5.
- Metrics never contain message text, contact data, QR content or secrets.

### 9.5 E2E, CI and data governance

- CI: unit/contract/fixture replay; local smoke; live E2E is env-gated.
- Flaky tests are quarantined with an owner and expiry; no silent retries that hide failures.
- Test accounts are isolated; fixtures are encrypted/redacted with retention policy.
- Canary uses a golden dataset (known chats/messages, duplicate names, long history, empty/error)
  and the §9.4 thresholds before rollout.
- Effect fault injection (post-MVP writes): no-land/partial/duplicate/wrong-record/other-record;
  assert read-back and outcome.

**Traceability (MVP).**

| Goal | Capability | Test | Metric | Gate |
|---|---|---|---|---|
| Find correct chat by prompt | `chat.find` | search/ambiguity fixtures | first-tool accuracy, wrong-entity | G2/G4/G5 |
| Open and verify chat | `chat.open` | header/selected postcondition | wrong-entity, postcondition | G5 |
| Read visible content | `chat.read` | OCR/read fixtures | read completeness, OCR confidence | G5 |
| Scroll history | `chat.scroll` | boundary/dedupe fixtures | scroll coverage/duplicate | G5 |
| In-chat search | `chat.search` | no-match/unsupported fixtures | unsupported/ambiguous, first-tool | G2/G4 |
| Leave/return | `chat.leave` | overlay/parent tests | not_switched, postcondition | G5 |
| Build/UI mismatch | registry | identity/hash tests | model mismatch, fallback rate | G5/G7 |
| Write/send (post-MVP) | `chat.send` | HITL/sent-state tests | send failures, duplicate sends | G6 |

### 9.6 Release gates

| Gate | Artifacts | Required tests/evidence | Metrics / exit |
|---|---|---|---|
| **G0** | scope, goals, test account, permissions, non-goals | environment reproducibility | scope approved |
| **G1** | dossier, interaction map, probe logs, provenance | schema validation, probe evidence | every goal has a path or explicit `unknown` |
| **G2** | capability manifest, coverage matrix | contract completeness, referential integrity | postconditions/errors/policy present |
| **G3** | adapter design (state machine, identity, algorithms, recovery) | design review, threat review | no raw UI leaks; no unresolved semantics |
| **G4** | adapter code, fixtures, tool projection/cards | fixture replay, unit/contract/pack tests | code/fixtures approved; redaction verified |
| **G5** | live E2E, metrics, unsupported list | canary E2E on golden goals | thresholds in §9.4; tool-selection/latency/privacy |
| **G6** | tool registration, policy wiring, monitoring | policy/HITL tests, rollout plan | capabilities enabled only if verified |
| **G7** | registry entry, drift report, version matrix, generated artifact versions | regression/fixture replay | compatibility/support window accepted |

A gate is not passed by prose; artifacts and evidence must exist. G1/G2 belong to Stage 1;
G3–G6 to Stage 2; G7 is maintenance.

## 10. Operations and release

### 10.1 Registry and versioning

- Registry entry per app × platform × build/UI hash with lifecycle state, model/adapter versions,
  capabilities/packs, fixtures, `last_verified`, `compatible_with` and deprecation.
- Registry changes require owner/security approval and audit.
- Generated artifacts pin bundle hash + generator version; reports list both.

### 10.2 Build detection and CI

- Detect new builds via release feed/manual/CI/user report.
- Compare build/UI hash; run drift reports on changed surfaces; full re-discovery on major changes.
- Fixture refresh and compatibility checks run in CI; invalid bundles block release.

### 10.3 Canary, rollout and rollback

- Canary on a test account/build; compare wrong-entity/read/scroll/latency/tool-selection metrics.
- Gradual rollout by account/build; keep the previous model/adapter registered.
- Rollback: revert registry entry, invalidate sessions, restore previous fixtures; audit the event.
- Define rollback triggers (wrong-entity >0, read drop, latency/cost spike, error spike).

**Repair lifecycle (post-MVP).** A repair moves only through:

`candidate → reviewed → replay_passed → fault_passed → approved → staged → canary → active`

with `rejected` reachable before staging and `rolled_back` reachable from `canary`/`active`. A model
may propose a candidate but may never review, approve, stage or activate it. Review and approval
require a human; the agent that taught the repair may not review or approve it. Approval binds the
prior and proposed bundle content digests, and staging/canary re-verify those bytes. The first
`max_runs` canary runs (default 5) are individually verified; any silent-incorrect effect or
verification regression automatically reverts to the prior bundle and holds the step for human
review.

**Qualification and admission.** For a read-only model, admission binds the bundle/model/adapter and
the G1–G6 evidence; subsequent updates also bind G7. For a write-capable model it additionally binds
the effect qualification campaign. The campaign is counted, not narrated: each task/condition has
at least three trials, and its failure taxonomy must include `silent_incorrect_success` and
`over_halt`. Campaigns use a sequestered test split; oracle payloads and task-specific expected
values are withheld from the shipped bundle. Admission is a signed, tenant/bundle/runtime-bound
record containing the applicable gate evidence and contract hashes, issuer,
`not_before`/`expires_at` (at most 30 days) and an admission id checked against a local revocation
set; a write admission also binds the campaign and oracle. A registry entry becomes `verified` only
after the record is trusted, active (`not_before` ≤ now < `expires_at`), unrevoked, and matches the
exact bundle/model/adapter that will run. Signer trust is local configuration; a development signer
may not enter a production trust map. Admission is not a substitute for the live run gates; a
fetch/verify failure fails closed.

### 10.4 Incidents and on-call

- Severity/owner/SLA/escalation for wrong-entity read/write, data leak, permission bypass, helper
  failure and model drift.
- Stop the affected capability/model, preserve audit/evidence, notify per policy, postmortem.
- Each app × platform has an owner and escalation path.

### 10.5 Security operations

- Helper/sidecar/operator signing/notarization, update path and TCC re-consent.
- Token/key rotation; local IPC token scope; exposure auth/rate limits for HTTP/A2A/MCP.
- Permission/secure-input health monitoring and alerts.
- Idle session/helper/browser-profile cleanup; release leases on TTL; audit orphan cleanup.

### 10.6 Data governance

- Redaction/retention/access policy for logs, fixtures, screenshots and audit.
- Breach notification/legal process; test-account isolation and cleanup.
- Audit retention/tamper-evidence and access reviews.

### 10.7 Capacity, cost and exposure

- Quotas for delegates/OCR/vision/tool loops; per-app/platform cost budgets.
- HTTP/A2A/MCP exposure: auth, rate limits, tenant isolation, monitoring.
- Long-tail apps use primitives/vision + HITL with lower reliability and explicit labeling.

### 10.8 Backup and disaster recovery

- Back up registry, fixtures and audit according to retention; test restore.
- Multi-tenant/account isolation; recovery time/point objectives.

## 11. Roadmap

The roadmap aligns with §7. Each phase has an exit; dependencies are explicit.

| Phase | Content | Exit | Depends on |
|---|---|---|---|
| **P0** | macOS operator/TCC/AX/OCR; default-deny + audit | health/TCC/AX/OCR smoke; audited denial | — |
| **P0.5** | Discovery harness: snapshot/probe/diff/fixtures/coverage/drift | validated draft bundle for WeCom | P0 |
| **P1** | WeCom Stage 1 model: dossier/map/identity/manifest/coverage/fixtures | G1/G2; unknowns resolved/optional | P0.5 |
| **P2** | WeCom core adapter: find/current/open/read/scroll + optional search/leave; contract/fixture/local smoke | G3/G4; no wrong-chat | P1 |
| **P2.5** | Registry/drift baseline (pulled from P8) | model registry, build detection, drift automation | P2 |
| **P4** | Live canary, metrics thresholds, rollout/rollback | G5/G6; read-only MVP accepted | P2.5 |
| **P3 (post-MVP)** | Composer/send with permission/HITL; deny/verify | write enablement only | P4 |
| **P5** | WeChat vision adapter; more app adapters | separate model/adapter; registry entries | P2.5 |
| **P6** | Platforms: Windows sidecar, Linux AT-SPI/X11/Wayland, Android operator/ADB, Go-native Web CDP | platform contract + tests | P2.5 |
| **P7** | Playbooks, planner/grounder/verifier, remote/A2A, official APIs | reviewed artifacts/tests | P4+ |
| **P8** | Continuous maintenance: incident/SLA, support window, full drift operations | registry/rollback/SLA drills | P4/P6 |

Rules:

- Do not scale apps/platforms before the registry/drift baseline (P2.5).
- Read-only MVP is P0–P2 + P4; write is post-MVP (P3).
- High-value apps get dedicated adapters; long-tail apps use primitives/vision + HITL.
- Policy/audit are P0/P1; P4 adds advanced templates and rollout.
- MCP remains an optional edge, not the core path.

## 12. Risks

| Risk | Impact | Likelihood | Mitigation | Owner |
|---|---|---|---|---|
| UI/version drift | wrong entity/read failure | high | model registry, build/UI hash, drift reports, re-discovery | app owner |
| Virtualized/heterogeneous UI | recall gaps, wrong search | high | native search, fixtures, coverage matrix | adapter owner |
| Identity ambiguity (duplicate/renamed/tenant) | wrong chat/privacy incident | medium-high | composite identity, `ambiguous`/HITL, wrong-entity metric | app owner |
| OCR/vision errors | incomplete/wrong content | high | local OCR/vision, confidence warnings, E2E | adapter owner |
| Async/non-determinism (loading, incoming messages, A/B) | no-op/race/partial read | high | stable waits, bounded retries, postconditions, fixtures | platform owner |
| Write idempotency/partial success | duplicate/incorrect send | medium | permission/HITL, sent-state verification, no blind retry | adapter owner |
| Platform fragmentation | inconsistent behavior | high | per-platform contracts/tests (§5) | platform owner |
| TCC/UAC/portal/Android permissions | hard stops/silent revocation | medium | preflight, health checks, HITL | platform owner |
| Helper/sidecar/operator compromise | privileged access/IPC token theft | medium | signing, token scope, least privilege, local-only IPC | security/platform |
| Supply chain (MCP/design tools/dependencies) | malicious tools/data | medium | allowlist, version pinning, policy proxy, review | security |
| Model-provider retention/training | PII leakage | medium-high | redaction, local OCR where required, provider policy | security/privacy |
| Audit/log tampering/over-retention | accountability/privacy loss | medium | tamper-evident logs, access control, retention | security |
| Resource/cost exhaustion | runaway spend/latency | medium | quotas, budgets, tool-loop limits, delegate queue caps | operations |
| Multi-tenant/exposure misconfiguration | cross-tenant access | medium | auth, tenant isolation, rate limits, monitoring | operations/security |
| Ownership/maintenance gap | stale models/silent failure | high | named owner, registry, drift/SLA | release owner |
| Legal/ToS/consent | account ban/legal exposure | medium-high | read-only default, host approval, legal basis | host |
| Test flakiness/environment drift | false confidence | medium | fixtures, canary, flaky-test policy | QA |
| Tool-selection errors/prompt injection | wrong action/authority confusion | medium | capability cards, selection metrics, policy | agent owner |

Risk acceptance is host-owned for legal/ToS/account risk. TCC/secure input/lock screen and
biometric challenges remain hard stops; other risks use policy/HITL/fail-closed mitigations.

## 13. Decisions

| ID | Decision | Scope |
|---|---|---|
| **C1** | Computer use is an optional capability package + preset, not core. | Architecture |
| **C2** | Access sources (API/structured UI/region OCR/full vision/HITL), execution modes (semantic capability/playbook/exploratory/HITL) and verification are independent; route per step, not by a universal ladder. | Architecture |
| **C3** | Web uses **Go-native CDP** as the primary path; external MCP is optional and policy-proxied; one profile per account; export/import where supported. | Web |
| **C4** | QR login via `QRConsumer`; zero-human requires a controlled phone; no bypass. | Login/session |
| **C5** | Sessions live in a vault; secrets never enter model/logs. | Login/session |
| **C6** | Platform control frameworks are separate per OS; macOS AX/CGEvent is the MVP. | Platform |
| **C7** | Playbooks are versioned deterministic sequences of capability calls using the same adapter/Guard/verification; procedures, not authority. | Playbooks |
| **C8** | Remote/multi-device reuse the same operator via MCP/A2A; no core changes. | Remote |
| **C9** | Default-deny permissions, HITL for configured risks, audit, no anti-bot evasion. | Safety/policy |
| **C10** | Challenges go through `ChallengeResolver`; CAPTCHA defaults to stop/HITL; biometrics hard stop. | Login/challenge |
| **C11** | The document defines mechanisms; per-app behavior/policy is host configuration. | Architecture |
| **C12** | Explicit permission matching by capability ID(s), target type (`EntityRef`/region) and bounded primitive plan; limits/JIT/deployment deny/prompt; one choke point; audit. | Safety/policy |
| **C13** | Examples are templates, not built-in rules. | Architecture |
| **C14** | Inventory/permissions are preconfigured; installed ≠ allowed; no runtime expansion. | Safety/policy |
| **C15** | Constraints are enforced in the execution path, not prompt-only. | Safety/policy |
| **C16** | Authorization is permission matching of capability IDs + targets + plans, not a global read/write classification. | Safety/policy |
| **C17** | Bypass is explicit, scoped, expiring, audited, never model-controlled. | Safety/policy |
| **C18** | Android: AccessibilityService operator APK primary; ADB control plane/fallback; UIAutomator/Appium for tests. | Platform (post-MVP) |
| **C19** | Windows: sidecar first; native CGo COM later. | Platform (post-MVP) |
| **C20** | Linux: AT-SPI2 + X11/Wayland tools; per-compositor matrix. | Platform (post-MVP) |
| **C21** | Device pools/lease; live sessions never migrate. | Platform (post-MVP) |
| **C22** | Emulator-backed apps are a DeviceProvider backend; same Session/Guard/Policy. | Platform (post-MVP) |
| **C23** | MVP = macOS **WeCom reference adapter**, read-only; WeChat is a post-MVP separate vision/OCR adapter; no WeCom API dependency; write/send is post-MVP with permission + HITL. | MVP |
| **C24** | macOS signed operator; TCC/secure input/lock screen hard stops. | MVP/platform |
| **C25** | WeCom and WeChat are separate app adapters on one macOS controller; profiles are UI data, behavior is code. | App adapter |
| **C26** | There is no universal chat adapter; shared code is platform mechanics, Guard/Policy/Audit, wait/OCR/AX helpers, errors/tests. | App adapter |
| **C27** | Model-facing chat tools are capability-filtered semantic operations; no element ids/coords/OCR clips for normal semantic operation; the explicit `app_*` fallback may expose normalized ids/coordinates under policy. | Tools |
| **C28** | The App Control Model is keyed by app × platform × build/UI hash; continuous maintenance is first-class. | Model/registry |
| **C29** | Models are published through a registry with lifecycle states; compatibility fails closed; stale models are never silently used. | Model/registry |
| **C30** | The project is **SDK-first**: in-process Go interfaces are the source of truth; MCP is an optional edge. No Node/MCP process in the core control path; `chrome-devtools-mcp` is a reference and optional Web/Electron discovery/source, not the architecture. | SDK/MCP |
| **C31** | Stage 2 is split into **2a compilation** (schemas/policy/skeleton/fixture wiring), **2b handwritten adapter semantics** (state machine, identity, search/read/scroll/verification/recovery) and **2c runtime**. The compiler never generates semantic algorithms from YAML. | Stage 2 |
| **C32** | A Stage 1 bundle must pass schema validation and referential-integrity checks before Stage 2; invalid bundles are rejected. | Model/registry |
| **C33** | Design sources (Figma/Penpot/files/screenshots/MCP) are optional hypotheses; runtime probes + developer review are authoritative. | Design sources |
| **C34** | Capability IDs (`chat.find`) are the configuration unit; tool names (`chat_find`) are projections. Policy/presets use IDs; schemas carry `x-capability-id`. | Tools/config |
| **C35** | Semantic tools are a common core + optional capability packs; packs are declared per app model and capability-filtered. | Tools/packs |
| **C36** | Multi-app agents use routing tools (`app_list`/`app_delegate`/`app_status`/`app_cancel`) and app-bound subagents; the parent does not receive all semantic tools. | Multi-app |
| **C37** | No matches return empty results + `warnings`; boundaries return `at_top`/`at_bottom` flags; low confidence is a warning, not an error. | Tool semantics |
| **C38** | External MCP tools are opaque sources: allowlist, schema validation, redaction, no implicit postconditions, policy proxy. | MCP |
| **C39** | Model registry + drift baseline must land before scaling apps/platforms. | Operations/maintenance |
| **C40** | Provider retention/data-residency policy is host-owned; screenshots/OCR are minimized and redacted. | Data/privacy |
| **C41** | Writes use a typed effect contract, pre-state, exact evidence coverage, three-valued verdict and tiered read-back; only tier 1/2 is effect-verified; uncertain writes become `reconciliation_required` and are never retried blindly. | Write/verification |
| **C42** | Sessions are window-scoped; AX semantic actions prefer background delivery, while global input requires foreground activation and policy/consent; PID/window/bounds/layer are revalidated and indeterminate delivery is not blind-retried. | Platform/session |
| **C43** | Observations are bounded and must surface truncation/deadline/incomplete-read warnings; never infer focus, selection or completeness from a truncated tree; use find/local snapshots, depth/boxes and file references. | Observation/tools |
| **C44** | Human demonstrations/recordings and agent exploration traces are optional Stage 1 inputs and hypotheses, never verified authority. | Stage 1 |
| **C45** | Repair follows `candidate → reviewed → replay_passed → fault_passed → approved → staged → canary → active`; models cannot promote, approval binds hashes, canary auto-reverts, and admission is signed, bounded and revocable. | Operations/repair |
| **C46** | Secrets are substituted at the execution edge; response redaction is defense-in-depth, never a security boundary, and resolved secrets never enter model context, logs or tool results. | Security/secrets |

All decisions are accepted unless a later revision marks them draft/deprecated; the scope column is
informational.

## 14. Open items

Each item has a type (`unknown` / `decision` / `implementation` / `prereq`), scope, owner, blocking
phase and exit. A read-only AX spike can start before all items are closed, but a phase cannot pass
its gate with its blockers open.

### 14.1 Open unknowns and decisions

| # | Item | Type | Scope | Owner | Blocks | Exit |
|---|---|---|---|---|---|---|
| **O1a** | WeCom message scroll container and boundary detection | unknown | MVP | app owner | P1 | container identified; AX page/wheel verified; `AtTop`/`AtBottom` defined |
| **O1b** | WeCom leave/back semantics | decision | MVP | app owner | P1 | defined (close overlay/clear selection/list) or explicitly unsupported |
| **O1c** | WeCom in-chat search availability | unknown | MVP | app owner | P1 | implemented capability or explicitly not registered |
| **O1d** | WeCom identity/ambiguity policy | decision | MVP | app owner | P1 | native/name+avatar/name+preview+time/position priority approved |
| **O1e** | WeCom search result taxonomy and debounce/stale handling | unknown | MVP | app owner | P1 | chat/contact/message/document classification + stale rules |
| **O1f** | WeCom modal/login/update map | unknown | MVP | app owner | P1 | detection/handling for each blocking state |
| **O1g** | WeCom build/UI-hash matrix | unknown | MVP | platform owner | P1 | target builds/hashes and variants recorded |
| **O10** | Legal/ToS/consent review | decision | host | host | — | approved use case and legal basis; read-only default |

### 14.2 MVP implementation items

| # | Item | Type | Scope | Owner | Blocks | Exit |
|---|---|---|---|---|---|---|
| **O2a** | WeCom adapter acceptance | implementation | MVP | adapter engineer | P2 | G3/G4; fixtures/contract/local smoke; no wrong-chat |
| **O3a** | Signing/notarization/update path | prereq | P0 | platform owner | P0 | signed/notarized operator; update path |
| **O3b** | TCC preflight/grant/re-consent | prereq | P0 | platform owner | P0 | Accessibility+Screen Recording; preflight; re-consent path |
| **O4** | Observation/element stability and stale detection | implementation | MVP | platform owner | P2 | hash algorithm/tests; stale errors |
| **O5** | Policy schema/validator | implementation | MVP | security | P2 | capability IDs, targets, bounded plan, limits, HITL; validation |
| **O7** | HITL approver auth/channel/routing/resume | implementation | MVP | HITL owner | P4 | authenticated approver/routing; pending/resume test |
| **O8** | Redaction/retention/provider-data policy | implementation | MVP | security/privacy | P4 | redactor tests; provider retention/OCR minimization; fixtures protected |
| **O9** | Latency/cost budgets | implementation | MVP | operations | P4 | observe/open/read/scroll p95; token/OCR/vision budgets |
| **O11** | Packaging/distribution | implementation | MVP | platform owner | P4 | signed package/update; TCC re-consent documented |
| **O13** | Adapter acceptance and metrics | implementation | MVP | QA | P2/P4 | contract/fixture at P2; live metrics/tool selection at P4 |
| **O14** | Discovery harness (Stage 1) | implementation | MVP | toolchain | P0.5 | snapshot/probe/diff/fixtures/coverage/drift + bundle schema validation |
| **O15** | Capability compiler/tool projection/cards | implementation | MVP | adapter engineer | P2 | bundle → schemas/policy/skeleton; `x-capability-id`; cards; no hand-editing |
| **O16** | Model registry, version detection and variants | implementation | MVP | release owner | P2.5 | registry lifecycle; build/UI hash; variants/A-B; fail-closed compatibility |
| **O19** | Config schema/validation | implementation | MVP | toolchain | P2.5 | app inventory/policy/preset schemas; capability-ID validation |
| **O22** | Test data/fixture governance | implementation | MVP | QA/security | P4 | isolated test accounts; encrypted/redacted fixtures; CI matrix |
| **O23** | Window-scoped session/background policy | implementation | MVP | platform owner | P2 | WindowRef + PID/window/bounds/layer validation; stale error; background AX; foreground consent for CGEvent; indeterminate delivery |
| **O24** | Bounded observation/find | implementation | MVP | platform owner | P2 | depth/boxes/budget; truncation/deadline warnings; find within snapshot; no completeness/focus inference from truncation; no full-tree dump |

### 14.3 Post-MVP items

| # | Item | Type | Scope | Owner | Blocks | Exit |
|---|---|---|---|---|---|---|
| **O2b** | WeChat vision/OCR adapter | implementation | post-MVP | adapter engineer | P5 | separate model/adapter; registry entry; tests |
| **O6** | Send idempotency | implementation | post-MVP | adapter engineer | P3 | one-shot grant + sent-state verification; no blind retry |
| **O12** | macOS host pool/remote | implementation | post-MVP | operations | P6 | Mac mini/VM provider/lease or remote transport |
| **O18** | Web/Electron primary adapter | implementation | post-MVP | platform owner | P6 | Go-native CDP controller; `chrome-devtools-mcp` reference only |
| **O20** | Exposure auth/tenant isolation | implementation | post-MVP | operations/security | P6 | HTTP/A2A/MCP auth, rate limits, tenant isolation |
| **O21** | Capability packs | implementation | post-MVP | adapter engineer | P5 | thread/channel/message/attachment/file/unread/send packs + tests |
| **O25** | Effect contract/read-back/outcomes | implementation | post-MVP | adapter engineer/QA | P3 | typed effects, pre-state, tiers, three-valued verdicts, exact coverage, idempotency/collateral checks, `reconciliation_required` |
| **O26** | Runtime repair/admission | implementation | MVP→post-MVP | release owner/operations | P4/P8 | P4: admission record; P8: repair lifecycle, no model self-promotion, hash-bound approval, canary auto-revert, signer trust/rotation |
| **O27** | Fault injection/effect E2E | implementation | post-MVP | QA | P3 | fake/real system of record; no-land/partial/duplicate/wrong-record; tier/verdict/outcome/coverage |

### 14.4 Maintenance and operations

| # | Item | Type | Scope | Owner | Blocks | Exit |
|---|---|---|---|---|---|---|
| **O17** | Maintenance/incident/rollback/SLA baseline | implementation | MVP→post-MVP | release owner/operations | P4 baseline; P8 full | drift automation, support window, incident/SLA, rollback drill |

### 14.5 Blocking summary

- **P0:** O3a, O3b.
- **P0.5:** O14.
- **P1:** O1a–O1g, O2a planning.
- **P2:** O4, O5, O13 (contract), O15, O19, O23, O24.
- **P2.5:** O16.
- **P4:** O7, O8, O9, O10, O11, O13 (live metrics), O17 baseline, O22.
- **Post-MVP:** O2b, O6, O12, O18, O20, O21, O25, O27; O26 baseline at P4, full at P8; O17 full.

## Appendix A: Example policies (WeCom)

### A.1 Read-only MVP

```yaml
schema_version: 1
platform: darwin
device: mac-1
app: wecom
match: {bundle: "com.tencent.WeWorkMac"}
mode: guided
onUnknown: deny
remember: session
allow:
  - {class: observe}
  - {class: pointer, target: {role: "AXRow"}}
  - {class: pointer, target: {label: "搜索"}}
  - {class: input, target: {label: "搜索"}}   # search field only, not the composer
# No composer, send, delete, forward or bulk export in the read-only MVP.
```

Semantic capabilities are authorized by capability ID; the adapter still maps them to guarded
primitives and cannot bypass the Guard:

```yaml
schema_version: 1
capabilities:
  - {name: chat.find,    class: read}
  - {name: chat.open,    class: navigate}
  - {name: chat.current, class: read}
  - {name: chat.read,    class: read}
  - {name: chat.scroll,  class: read}
  - {name: chat.search,  class: read, optional: true}
  - {name: chat.leave,   class: navigate, optional: true}
```

### A.2 Post-MVP write example

This is **not** part of the read-only MVP:

```yaml
schema_version: 1
allow:
  - {class: input, target: {role: "AXTextArea"}}   # composer
hitl:
  - {class: pointer, target: {label: "发送"}}
  - {class: key, target: {key: "Return"}}
capabilities:
  - {name: chat.send, class: input, hitl: true}
```

The YAML is illustrative; §8.2 is the normative permission/grant model. Capability IDs (`chat.find`)
map to tool names (`chat_find`); presets/policies use IDs.

## Appendix B: Usage scenarios

- **S0 (MVP smoke test)**: launch/attach, find a known chat by name, open, verify current, read the
  visible page, scroll older, leave; read-only; no PII/credentials in logs; TCC errors typed.
- **S1 (post-MVP)**: web travel planning; read/search pages, compare options; no booking without
  HITL.
- **S2 (post-MVP)**: shopping research/assisted checkout; explicit permissions/HITL; no anti-bot
  evasion.

A scenario does not pass if it fabricates data, reads the wrong entity after detection, or bypasses
permissions/HITL.

## Appendix C: Runtime and deployment options

- Web: local/remote Chromium + CDP; one profile per account.
- Android: real device, emulator/AVD, cloud device pool; DeviceProvider/lease.
- macOS: Mac mini/VM/cloud Mac with a logged-in GUI session; TCC provisioned; signed helper.
- Windows: interactive session; sidecar; UAC/secure desktop constraints.
- Linux: X11 or Wayland with portal; per-compositor support matrix.
- HITL transports: web/chat/email/phone/operator console; signed tokens; resumption.
- No runtime is universal; choose per phase, platform and risk profile.

## Appendix D: Normalized and app-semantic types (illustrative)

Platform types (`Capabilities`, `Observation`, `Element`, `Action`, `Rect`, `OCRResult`,
`ActionClass`, `Controller`) are defined in `pkg/apps`; the snippets in this document are
summaries. App-semantic types are not all implemented yet; they belong to the model/adapter layer
and should be defined in `internal/apps/model` (or `pkg/apps/model` when externally named).

```go
type EntityRef struct {
    App        string
    Platform   string
    WindowID   string // session-bound window/account
    Kind       string // chat | channel | doc | ...
    Key        string // stable within the model/session; never an AX/DOM element id
    Name       string
    Confidence float64
}

type WindowRef struct {
    ID      string
    Title   string
    App     string
    Account string
    // The session keeps a separate, model-invisible pin: owner PID, window ID,
    // bounds, layer and process generation. A pin mismatch is ErrStaleObservation.
}

type Entity struct {
    Ref      EntityRef
    Name     string
    Preview  string
    Time     time.Time
    Unread   int
    Selected bool
}

type FileRef struct {
    EntityRef
    Name string
    MIME string
    Size int64
}

type Attachment struct {
    Ref  FileRef
    Name string
    MIME string
    Size int64
}

type SearchHit struct {
    Ref      EntityRef
    Kind     string // entity | message | contact | document
    Snippet  string
    Time     time.Time
    Score    float64
}

type Location struct {
    Kind     string // workspace | channel | list | thread | search_results | entity | modal
    Entity   *EntityRef
    Parent   *Location
}

type Cursor struct {
    Anchor string // opaque adapter token; not an element id/universal scroll offset
    Offset int
}

type Page struct {
    Entity   EntityRef
    Messages []Message
    Cursor   Cursor
    AtTop    bool
    AtBottom bool
    Warnings []string // stable codes, e.g. no_matches | ax_truncated_depth | ax_truncated_deadline
}

type Message struct {
    ID        string
    Sender    string
    Time      time.Time
    Text      string
    Direction string // incoming | outgoing | system
    Attachment *Attachment
}

type ObserveOptions struct {
    Depth       int
    MaxElements int
    IncludeBoxes bool
    IncludeImage bool
    Query       string // find within the snapshot
}

type ReadOptions struct {
    Ref    *EntityRef
    Cursor *Cursor
    Limit  int
}

type ScrollOptions struct {
    Ref       *EntityRef
    Direction string // older | newer | top | latest
    Pages     int
}

type SearchOptions struct {
    Query   string
    Scope   string // current | container | workspace | global
    Filters map[string]string
    Limit   int
}

type LeaveOptions struct {
    Level int // 0 = parent, 1 = grandparent, ...
    To    string // optional explicit target kind/state
}

```

`CapabilityDescriptor` is defined in §4.2 and `Capability` in §3.6 (`ActionClass` and
`jsonschema.Schema` come from `pkg/apps` and
`pkg/jsonschema`). Its supporting manifest types are:

```go
type Condition struct{ Kind, Value string }
type Check struct{ Kind, Value string }
type Step struct {
    Kind string
    Args map[string]any
}
type ErrorCode string
```

`AppIdentity`/`ModelRef` are described in §3.10. None of these types should expose AX/DOM element
ids, coordinates or OCR clips to the model.

## Appendix E: OSS reference patterns (2026-10-19)

This appendix records the patterns reviewed in OpenAdapt, Peekaboo, Playwright MCP, Stagehand and
UFO. These are **references, not vendored code**; each project has a permissive license (MIT or
Apache-2.0), but any copied code needs a separate license/attribution review. The Go implementation
stays SDK-native and does not import these projects.

| Project | License | Inspected | Adopt | Limit / do not adopt |
|---|---|---|---|---|
| **OpenAdapt** | MIT | `packages/flow/benchmark/effectbench`, `openadapt_flow/{verification,transaction,qualification_admission}.py`, `openadapt_flow/repair/` | Typed effect contracts; pre-state; three-valued verdicts; evidence tiers; exact-coverage/no-effect proof; transaction outcomes; repair lifecycle; signed qualification admission | Do not port the Cloud/billing stack; write/repair is post-MVP; admission is a release gate, not a live-run substitute |
| **Peekaboo** | MIT | `Core/PeekabooAutomationKit/.../Services/UI/{BackgroundInputDriver,WindowRoutedPointerDriver,AXTreeCollector,BackgroundWindowActivationDriver}.swift`, `Core/Models/{Snapshot,Capture}.swift` | Exact-window pinning; PID/window/bounds/layer revalidation; background AX (`AXPress`/`AXShowMenu`); pid-routed input; stale vs indeterminate delivery; bounded AX traversal with truncation warnings | macOS-specific mechanics; do not copy Swift identifiers or expose AX handles to the model |
| **Playwright MCP** | Apache-2.0 | Upstream `packages/playwright-core/src/tools/backend/{find,snapshot,tool,idleTimer}.ts`; local clone is package-only | Find-in-snapshot with context/ancestor path; snapshot after mutation; `target/depth/boxes`; capability groups; tool kinds; idle TTL after a completed call; output-size eviction; response redaction caveat | Do not adopt `browser_run_code_unsafe`/arbitrary evaluate for semantic agents; full DOM snapshot is not our primary observation; redaction is not a security boundary |
| **Stagehand** | MIT | `packages/sdk-go/models.gen.go`, `stagehand.go` | `observe/act/extract`; locator scoping; `ignoreLocators`; `variables` placeholders; `Action{selector,method,arguments}`; cache | Web/selector-centric; no effect verification; selectors stay inside the adapter |
| **UFO** | MIT | `ufo/agents/{host_agent,app_agent,evaluation_agent}.py`, processor strategies/schemas, `config/config_schemas.py` | Host/app split; target registry; multi-action responses; evaluation agent; control-backend/grounding fallback | No typed effect contract or admission; Windows/UIA-centric; planner is not authoritative |

**Cross-cutting conclusions.**

- **Verification is a record check, not a screen check.** The screen is a witness, not the system of
  record.
- **Observation is bounded and identity-bound.** Truncation and window identity are first-class
  results, not hidden implementation details.
- **Tool exposure is policy metadata.** Capability group + tool kind + postcondition let a host
  enable a safe subset without prompt engineering.
- **Secrets are substituted, not merely redacted.** Redaction protects logs; substitution protects
  the model context and execution edge.
- **Repair and admission are release processes.** A model may propose; only a human can promote; the
  exact approved bytes are what runs; a canary can revert automatically.