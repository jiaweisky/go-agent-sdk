# go-agent-sdk — video/audio (multimodal) model support

> Scope: make models that accept **video** and **audio** input usable end to end
> — through the root SDK API, `cmd/agentserve` and `cmd/a2aserve` — and bindable
> to a **subagent** (each agent may name its own model), so a media-capable model
> can be used for the specific task that needs it.
>
> Status: design proposal (not implemented). Numbers in braces like **{M1}** are
> decisions; all design decisions are recorded in §8 (test tooling in §7).

## Change log

| Date | Change |
|---|---|
| 2026-10-07 | First draft: current-state analysis, per-model provider config, OpenAI audio, a2aserve inbound media, size policy, subagent binding |
| 2026-10-07 | Expanded test plan: local **Ollama** harness (`qwen3-vl:2b`, `nemotron3:33b` — OpenAI-compatible, both Text+Image only), the main×subagent **capability matrix**, and where audio/video are validated (Gemini + request-capturing stub) |
| 2026-10-07 | Added media **sources/transport** ({M7}: inline / external URL / Files-API `file_id`, per-protocol support, `FileUploader` hook) and **image detail** ({M8}), per DeepSeek's vision + Files API (three image-input modes, `file` block, `detail`) |
| 2026-10-07 | Added the **extensibility architecture** ({M9}): the three independent axes (model capability = data, wire protocol = registry, provider API = host hook) and a single negotiation step, so new models/protocols/providers are config or registration, not scattered edits |
| 2026-10-07 | Post-review revision: added {M13} (canonical media value + one planner + the **three** protocol touch-points), the **subagent-delivery blocker** (G9, §4.6) — per-agent models exist but attachments never reach the child session, the `serveconfig` **catalog precedent** (`examples/internal/util/catalog.go`) with `ProviderID` override / per-entry credentials+limits, and corrected DeepSeek facts (model id `deepseek-flash`, `file_data`, Anthropic beta header) |
| 2026-10-07 | Corrected the test plan (§7.1/§7.3) after the Ollama API docs: Ollama's compatible APIs are **text + image, base64 only** (no URL images, no audio/video; `/v1/responses` images undocumented), so Ollama validates text/image and the `url`-source negative path, while audio/video use a stub + Gemini and image `url`/`file` use DeepSeek |
| 2026-10-07 | Test harness switched to **vLLM-only**: vLLM's OpenAI-compatible server carries `image_url` + `input_audio` + `video_url`/`audio_url`, so one local server covers image/audio/video E2E (§7.1/§7.3). Removed the Ollama harness and the Gemini/DeepSeek cloud smoke (deferred, §8). `openai-compatible-chat` gains vLLM's video/audio extension blocks, distinct from `openai-chat` |
| 2026-10-07 | Clarified §2.3 (the wire-support table is **SDK lowering**, not server capability — an OpenAI-compatible server can accept more than we emit) and §4.3 ({M3} now splits `openai-chat` = `input_audio` only from `openai-compatible-chat` = `input_audio` + vLLM `video_url`/`audio_url`) |
| 2026-10-07 | Documented media **forms**: §4.8 "one source, several wire encodings" (data URL vs bare-base64 `input_audio` vs `file_id`) and §7.1 vLLM's accepted forms per block (host URL / data URL / bare base64 only for `input_audio` / server-local `file://`), noting `file://` ≠ the SDK's `file` source |
| 2026-10-07 | Added {M14} to §4.10: the **protocol factory** (`OpenAIChatProtocolWith(profile)`, `MediaEncodingProfile`) for "same protocol, different provider encodings" (OpenAI vs vLLM vs DeepSeek), plus the **source/encoding selection** rule — the catalog declares capability, the *attachment* declares the source, and the profile's ordered list picks the encoding |
| 2026-10-07 | Review pass: §1 now carries the subagent-delivery caveat; §4.3 reconciled with {M14}; added `MaxInlineBytes` to `ProtocolMediaSupport` and unified naming; fixed "config declares encodings"; added the `Encoding` hint to `MediaPart`/`PromptFileAttachment`; made the descriptor derive from the profile (one source); defined **empty = unconstrained** in the planner; split `openai-compatible-chat` in the transport table; added the provider-extension recipe; put {M14} in P2; updated the §7.4 gate test |
| 2026-10-07 | Closed the remaining design details: planner **handoff** (writes resolved `Source`/`Encoding` onto the part; lowering reads only that) and **role** plumbing (§4.10); `MediaUploader` returns a `MediaRef` (id *or* URI) with the upload adaptation loop (§4.10/§4.8); `{M4b}` sets `Source`/`Encoding` when inlining A2A parts (§4.4) |
| 2026-10-07 | Trimmed over-design in the abstraction: dropped the per-attachment `Encoding` hint (profile order is fixed), flattened `MediaEncodingProfile` to an ordered `[]BlockKind` (MIMEs/Sources derived, not maintained), and removed the model-level `Detail` boolean (per-part `Detail` stays) |
| 2026-10-07 | Cross-protocol review: generalized the `file` source to "provider-native reference" (file_id / `fileUri` / `s3://`) and moved Bedrock's `s3Location` out of `url`; noted Bedrock Converse's `document`/`video` (Nova) blocks and document-block rules; added a normalized role vocabulary; scoped `MaxInlineBytes` to inline only (url/file are provider-enforced); expanded the `BlockKind` examples beyond OpenAI; and noted Anthropic `file` availability differs on Bedrock/GCP |
| 2026-10-07 | Spelled out `openai-responses` in §4.3: its `input_image`/`input_file`/`input_text` blocks and its wider roles (`user`/`developer`/tool-output) — its own `BlockKind`s/profile, not the chat lowerer |
| 2026-10-07 | Resolved the last factual open question: **OpenAI Responses has no audio input** (OpenAI directs audio-chat to Chat Completions); `openai-chat` audio requires `modalities:["text","audio"]` + an audio model, `format` `wav`/`mp3`. §4.3 and §8/Q3 updated |
| 2026-10-07 | §8 split into **Decided** vs **Still open**. Decided: flat `models:` list + per-entry limits; a2aserve `spool` default / `inline` opt-in; `read`-tool media deferred to P9; expose `MediaUploader`; MVP = session's own model ({M6a}); `detail` per-attachment only |
| 2026-10-07 | Reframed for **industrial-grade scope** (no MVP cut): removed MVP/deferral wording (§1, §4.3, §4.6, §5, §6, §8); both delivery paths (session model + subagent) and both video transports (inline + provider ref) are in scope; added **§9 Production requirements** (typed outcomes/observability, security, limits, concurrency/caching, versioning, config validation, testing, performance) |
| 2026-10-07 | More decisions: **no local cap interception** (provider is the authority; typed `provider_error`) — removed `MaxInlineBytes` and the size clause from the planner/descriptors (§4.5/§4.10), keeping only an OOM safety bound; **cloud E2E** = yes (Gemini free tier + DeepSeek, key-gated) alongside vLLM; **upload caching** = SDK content-hash dedup |
| 2026-10-07 | Framed the free tier correctly: it is a **development/CI cost saver, not a design decision** — E2E uses whatever credentials are configured (free tier in dev, paid in production/release, same code path). Removed "free-tier specifics" from §8 open; §7.1/§8 reworded |
| 2026-10-07 | Moved the **protocol→vLLM shim** out of design questions into the test plan (§7.5, dev/test only, never in deployment); §8 is now **Decisions only** (12), no open items |
| 2026-10-07 | Added **§4.11 Consumer surfaces ({M15})** after a surface review: SDK (`AttachmentOmitted` event, `core/tool.FileContent` gains `Source`/`FileID`, `MediaRef` expiry + ref persistence), agentserve (`GET /api/model`, config-file-only catalog, large-media/upload story), a2aserve (model-derived card modes, omission mapping, outbound `Source`). Decisions 13–14 + phase P10 added |
| 2026-10-07 | Pinned the two surface calls (§4.11/§8-15): agentserve exposes `POST /api/session/{id}/file` (host `MediaUploader`) for the `file` source; a2aserve outbound maps inline→`fileWithBytes`/url→`fileWithUri` and **omits** a provider `file` ref; card `inputModes` are **derived** from the model's media capabilities |
| 2026-10-07 | Clarified the **empty-`Input` semantics** (§2.2/§4.10/§9): empty = unconstrained ("protocol only"), **not** text-only; the host must declare `input: [text]` to reject media. Documented the two-layer fallback: declared capability → graceful omit; undeclared/mis-declared → provider is authority, and `DropErroredTurns` prevents wedging |
| 2026-10-09 | Implementation review fixes: wired the **role** axis into the planner (roles now passed from `to_llm_message.go`, `unsupported_role` emitted); made `MediaRef.Source` authoritative in the adaptation loop; **narrowed `anthropic-messages` `Sources` to `[inline,url]`** (image `file` block is not a valid Anthropic image source; `document` is not lowered); bounded the upload dedup cache (FIFO). §2.3/§4.8 tables updated |

---

## 1. Motivation and goal

The SDK today exercises **text + image** models (e.g. DeepSeek). Several providers
accept **audio** (OpenAI audio models, Gemini) and **video** (Gemini, Amazon
Nova) input. Enabling them lets an agent do things such as transcribe an audio
clip or reason over a screen recording.

Because a **subagent can name its own model** (`AgentInfo.Model`; §2.7), the
intended cheap path is not "make the main model multimodal" but "add a subagent
bound to a multimodal model": the main agent stays on its text/image model, and a
`transcribe` / `video-review` subagent runs a multimodal model.

**Caveat (see §3 G9, §4.6).** That subagent pattern **does not work today**: the
child session is seeded from the `task` prompt text only, so an attachment on the
parent never reaches the child. This design **treats that as a defect to fix
(G9)**, not a limitation to accept: both the session-model path ({M6a}) and the
subagent path ({M6b}/{M6c}) are in scope (§4.6). Until the delivery mechanism
lands, only the session's own model can carry media.

**Goal.** Sending an audio/video attachment to a session whose model accepts that
modality reaches the provider correctly; otherwise it is omitted and the client is
told (existing behavior). No new transport, no schema-first codegen.

**Non-goals.** Media **output** (the model generating audio/video), transcription
post-processing, provider file-upload lifecycle, and porting the models.dev
catalog (see `AGENTS.md`).

## 2. Current state (analysis)

### 2.1 The media pipeline
`schema.PromptFileAttachment{URI, Mime, Name}` (user attachments) and
`schema.ToolFileContent{URI, Mime, Name}` (tool results) become
`llmschema.MediaPart{MediaType, Data}` when a session turn is assembled
(`core/session/runner/to_llm_message.go`), then each protocol lowers the part to
its wire shape. `MediaPart` already carries **any MIME type**, so audio/video
needs no new type — but `Data` today is always **inline** bytes/a data URL; other
sources (external URL, provider file id) are {M7}.

### 2.2 The capability gate
At context assembly the runner keeps a media part only if:
- the **protocol** can carry its MIME (`protocols.MediaAllowed`, per-protocol
  `ProtocolEntry.MediaMIMEs`), and
- the **model** accepts its modality (`ModelInfo.Capabilities.Input`, an empty
  list means "protocol only"), and
- its payload is well-formed (`utils.ValidateMedia`: base64, size).

Anything else is replaced by a text note and reported as the durable
`session.next.attachment.omitted` event, so a client can perceive it. Modalities
are produced by `utils.MediaModality` (`image`, `audio`, `video`, `text`, `pdf`).
Note: an empty `Input` means "protocol only", not "text-only" — the host must
declare modalities (e.g. `[text]`) to make the model **reject** media it does not
accept (see §4.10).

### 2.3 Per-protocol wire support (what the SDK lowers, today)

| Protocol | Images | Audio | Video | Lowering |
|---|---|---|---|---|
| `openai-chat` | yes | **yes** | no | `image_url` / `input_audio` |
| `openai-compatible-chat` | yes | **yes** | **yes** | `image_url` / `input_audio` / `audio_url` / `video_url` |
| `openai-responses` | yes | **no** | no | `input_image` |
| `anthropic-messages` | yes | no | no | `image` block |
| `gemini` | yes | **yes** | **yes** | generic `inlineData` / `fileData` |
| `bedrock-converse` | yes | no | no | `image` / `document` block |

> **Scope of this table — SDK code, not server capability.** These columns say
> what *this SDK's lowering functions emit*, not what a provider accepts. The
> `openai-compatible-chat` row carries audio/video via the vLLM extension blocks
> (`input_audio`/`audio_url`/`video_url`), while `openai-chat` (OpenAI proper)
> carries audio via `input_audio` but no video; both are driven by the protocol's
> `MediaEncodingProfile` ({M3}/P4, §4.3). Conversely a protocol may support blocks
> we do not lower yet: Bedrock Converse also carries `document` and `video` (Nova)
> blocks alongside `image` (§4.8), and Anthropic `document` blocks are not lowered
> (a `file` source is therefore omitted for `anthropic-messages`).

> **Key finding:** **Gemini already supports audio and video** on the wire today.
> `geminiMediaMIMEs` = images + `videoMIMEs` + `audioMIMEs`
> (`llm/protocols/shared.go`), and `geminiLowerUserPart` emits one generic
> `inlineData` block. So a Gemini model that declares (or is defaulted to) the
> audio/video modalities already works — no protocol code change is required.

### 2.4 Model capability declaration
`ClientOptions.ModelInfo` is a `func(modelID) schema.ModelInfo`; the SDK reads
`Capabilities.Input` and `ProviderID` from it, and calls
`ClientOptions.Credentials(providerID)`. The command helper
`serveconfig.ModelInfoProvider(protocol, baseURL, aws, inputModalities)` fills
one protocol and one modality list, and `serveconfig.CredentialProvider` returns
one credential.

### 2.5 Size limits
`utils.ValidateMedia` bounds one part at 28 MB encoded / 20 MB decoded. Whole
video is usually larger; providers either accept a bounded inline blob or require
a provider file-upload API.

### 2.6 Command ingestion paths
- **agentserve**: `POST /api/session/{id}/prompt` `files[]` → `MessageOptions.Files`
  → native media parts. Any MIME the model+protocol accept already flows through.
- **a2aserve inbound**: A2A file parts are **spooled to disk** and referenced by
  path in the prompt text (`a2a_factory.go`); `MessageOptions.Files` is not set,
  so inbound audio/video never becomes a native media part.
- **a2aserve outbound** (this SDK as an A2A client): the last user message's media
  is decoded and sent as A2A file parts (`a2a_backend.go`).

### 2.7 Per-agent model
`AgentInfo.Model *ModelRef` (set from agent markdown frontmatter `model:` or in
code) is the model a **child (subagent) session** runs
(`task_run.go`: `subagent.Model`, else the parent's). A session's model is
resolved per turn: `ClientOptions.ModelInfo(modelID)` → protocol/limits/
capabilities, then `ClientOptions.Credentials(providerID)`.

The model is per-agent, but **the child does not inherit the parent's
messages**: `runChildSession` admits a `schema.Prompt{Text: prompt}` built from
the `task` tool input, which carries no attachments (`task.go`, `task_run.go`).
So per-agent model selection works, attachment delivery to the child does not
(§3 G9, §4.6).

### 2.8 Command configuration today
`model`, `protocol`, `modelBaseURL`, `credentials`, `inputModalities` are
**single values** applied to every model id. So all models an agent could use
share one protocol, endpoint, credential and modality list.

## 3. Gaps

- **G1 — per-model protocol/credential.** A subagent cannot use a
  different-protocol model (e.g. main = `openai-compatible-chat`, subagent =
  `gemini`) because the command provider/credential is single-valued. This is the
  main blocker for "add a multimodal subagent" whenever the multimodal model
  speaks a different protocol.
- **G2 — per-model modalities.** `inputModalities` is server-wide, so a mixed
  setup (text-only main model + audio/video subagent model) cannot be expressed;
  either everything is gated by the union, or the union is unsafe.
- **G3 — OpenAI / OpenAI-compatible audio & video.** `openai-chat` cannot carry
  audio even though OpenAI's chat API expresses it (`input_audio`). vLLM — an
  OpenAI-compatible *server* — additionally accepts `video_url`/`audio_url`
  extension blocks, which `openai-compatible-chat` cannot emit either. No
  protocol code path exists for any of these. (Text + image over
  `openai-compatible-chat` already work against vLLM with **no SDK change**;
  see §7.1.)
- **G4 — a2aserve inbound audio/video.** Inbound A2A media is spooled to disk, so
  a remote A2A caller's audio/video is never delivered to the model as media.
- **G5 — large-media policy.** No modality-aware size policy; a video over 20 MB
  is silently omitted with only a generic note.
- **G6 — docs/tests** for media models.
- **G7 — transport modes.** Only **inline base64** is supported. Providers expose
  more: a **remote `http(s)` URL** the provider fetches, and a **provider file
  reference** (`file_id` from a Files API). DeepSeek, for example, offers all
  three for images (base64 data URL, external URL, uploaded `file_id` via a
  `file` content block) and caps them differently (inline body 48 MiB, URL image
  32 MiB, file-id image 64 MiB). A plain `https://…` URI today is treated as
  base64, fails validation, and is omitted.
- **G8 — image detail level.** Providers accept a `detail` hint
  (`low`/`high`/`original`/`auto`); the SDK has no way to express it.
- **G9 — subagent delivery (the headline blocker).** The design's recommended
  pattern (§1, §4.6) is "keep the main model cheap and delegate media to a
  multimodal subagent". Per-agent model resolution already exists, but a child
  session is seeded from the `task` prompt **text only**: `TaskInput` has no
  attachment field (`task.go`) and `runChildSession` admits
  `Prompt{Text: prompt}` (`task_run.go`). The main model cannot see the media
  either, so it cannot forward it. Consequently an attachment can reach a
  multimodal model **only if that model is the session's own model**; the
  subagent pattern does not work as written. This is not a routing gap (G1/G2)
  but a delivery gap, and it gates the stated outcome.

> Note: G1/G2 are the enabler (per-model routing); **G9 is the gating gap** for
> the subagent outcome. G3/G4/G7 are protocol/transport follow-ups; G5/G8 are
> per-part policy; G6 is docs/tests.

## 4. Design

### 4.1 Per-model provider configuration (the enabler) — **{M1}**

Add a `models:` list to `cmd/agentserve` and `cmd/a2aserve` config, resolving
`ModelInfo` and credentials **per model id**:

Each entry declares the model's **media capability** ({M11}); `inputModalities`
is shorthand for `media.input`:

```yaml
# default entry (backward compatible: today's top-level keys)
model: deepseek-flash
protocol: openai-compatible-chat
modelBaseURL: https://api.deepseek.com
inputModalities: [text, image]          # shorthand for media.input
credentials: { apiKey: sk-... }

# additional models an agent/subagent may name
models:
  - id: gemini-2.5-flash
    protocol: gemini
    modelBaseURL: https://generativelanguage.googleapis.com
    media:
      input: [text, image, audio, video]
      sources: [inline, file]           # Gemini inlineData + Files API
    credentials: { apiKey: ... }
  - id: deepseek-vision            # catalog id
    model: deepseek-flash          # wire model: DeepSeek serves vision on deepseek-flash
    protocol: openai-compatible-chat
    media:
      input: [text, image]
      sources: [inline, url, file]  # base64 data URL / external URL / Files API file_id
    credentials: { apiKey: ... }
```

`serveconfig.ModelInfoProvider`/`CredentialProvider` are replaced by a small
catalog built from the default entry + `models:`, keyed by model id
(`ModelInfoProvider`), provider id (`CredentialProvider`) and provider id
(`MediaUploader`, {M12}). An unknown id falls back to the default entry.

**This shape already exists.** `examples/internal/util/catalog.go` is a working
per-model catalog: `ModelEntry{ID, Model, ProviderID, Protocol, BaseURL,
Context, Output}` → `Catalog.ModelInfo(id)` → `Catalog.Credentials(providerID)`.
The serve work is to lift that shape into `serveconfig` (shared by both
commands), not to invent one. Four details it must handle explicitly:

- **`ProviderID` must be set on the resolved `ModelInfo`.** `agent.NewModelInfo`
  hardcodes `ProviderID = protocol` (`model_info.go`); the example `Catalog`
  fixes this by overwriting `info.ProviderID = entry.ProviderID`
  (`catalog.go:112`). The serve helper must do the same, or
  `Credentials(providerID)` is called with the protocol id and a catalog that
  spans providers (same provider, different protocols, or vice versa) cannot
  resolve its key.
- **Per-entry credentials.** `Catalog.byProvider` keeps only the first entry per
  provider (`catalog.go:54`); per-entry credentials ({M1c}) need credentials
  keyed by entry, not deduped by provider.
- **Per-entry limits.** `contextLimit`/`outputLimit` are top-level today and
  `ModelInfoProvider` passes an empty `ModelLimit`; a real catalog needs them per
  entry (decided: flat `models:` list with per-entry limits, §8).
- **Shared config type + validation.** The two commands have separate flat
  `config` structs (`cmd/agentserve/config.go`, `cmd/a2aserve/config.go`), both
  requiring top-level `model`/`protocol`/`credentials` (`validateRequired`). Put
  the `models`/`media` types in `serveconfig` so both embed them, and relax
  `validateRequired` for a catalog-only config.

This keeps the `AGENTS.md` rule "do not port the catalog; the host supplies a
narrow input": the host declares exactly the models it exposes, each with its own
capabilities. The SDK API is the same shape — `ClientOptions.ModelInfo`/
`Credentials` already resolve per id; only the capability descriptor is added
({M11}). Modalities stay in `ModelCapabilities.Input`; the added
`ModelCapabilities.Media` holds only constraints (sources/size), so the
config-side `media.input` key is sugar the loader splits.

**Decisions inside {M1}**
- **{M1a}** Agent markdown uses `model: <id>` (or `provider/model`); the catalog
  is keyed by model **id**; provider id is taken from the resolved `ModelInfo`
  (which the catalog sets explicitly, per above); a `model:` field may map a
  catalog id to a different wire model name (`ModelEntry.Model`).
- **{M1b}** A model not in the catalog but matching the default protocol still
  resolves via the default entry (a subagent may name a same-protocol model).
- **{M1c}** Credentials per entry; a shared top-level credential remains for the
  default entry.

### 4.2 Per-model modalities — **{M2}**

Modalities come from each catalog entry (`inputModalities`), not a single
server-wide list. The runner already reads them per model
(`ModelInfo.Capabilities.Input` → `mediaPolicyForModel`), so this is purely a
config-plumbing change. A text-only main model and an audio/video subagent model
coexist: the main model omits audio/video, the subagent accepts them.

### 4.3 OpenAI / OpenAI-compatible audio & video lowering — **{M3}**

`openai-chat` (OpenAI proper) and `openai-compatible-chat` (vLLM and friends)
share `OpenAIChatProtocol`, but their **servers accept different blocks**, so
their lowering/descriptors must diverge.

**`openai-chat` — audio only:**
- Extend `openaiChatUserContent` with an `input_audio` variant
  (`{type:"input_audio", input_audio:{data, format}}`) and a lowering branch that
  maps the MIME to OpenAI's `format` (`wav`/`mp3`).
- Add the audio MIME set to `openai-chat`'s `ProtocolMediaSupport` so the gate
  allows it; the **model modality** gate keeps non-audio models safe.
- Audio input also requires the request's **`modalities`** to include `audio`
  (`modalities:["text","audio"]`) and an audio-capable model (`gpt-audio-*`); the
  lowering sets it when an audio part is present. `format` maps from the MIME
  (`wav`/`mp3`); other audio MIMEs are unsupported by OpenAI and omitted.
- Video: OpenAI proper has no native video block — `openai-chat` stays
  images + `input_audio`.

**`openai-compatible-chat` — audio + video (vLLM extensions):**
- `video_url`: `{type:"video_url", video_url:{"url": <url|data URL>}}`.
- `audio_url`: `{type:"audio_url", audio_url:{"url": <url|data URL>}}` (the
  alternative to `input_audio`).
- These are **vendor extensions**, not OpenAI proper; they are enabled only by the
  `openai-compatible-chat` descriptor, and only when the model/descriptor allows
  video/audio. `input_audio` is shared with OpenAI (same shape).

**`openai-responses` — its own block names and roles:**
- Content blocks are `input_image` (`image_url` **or** `file_id`, mutually
  exclusive), `input_file` (`file_id`/`file_data`/`file_url`) and `input_text`;
  **audio is not supported** — OpenAI directs the audio-chat pattern to Chat
  Completions. These differ from chat's `image_url`/`input_audio`, so Responses
  gets its **own** `BlockKind`s and profile — do not reuse the chat lowerer.
- Media may appear in `user` **and** `developer` messages, and inside tool-output
  items (`function_call_output` / `custom_tool_call_output` `.output`), so its
  `Roles` is wider than chat's (see the normalized roles, §4.10).

**Three touch-points, not one.** Adding a content-block type means updating the
protocol's **request-body JSON schema** (`llm/protocols/body_schema.go`, compiled
and checked on every built body), the lowering, **and** the registry descriptor —
not one of them. Today `openai-compatible-chat` **reuses `OpenAIChatProtocol`**
(`openai_compatible_chat.go`), so both entries share a single `Body.From` and
cannot lower differently; **{M14}** (§4.10) replaces that with a factory
parametrized by an encoding profile, so each entry's descriptor gates its own
blocks. Either way `ProtocolMediaSupport` (not a shared hardcoded set) must drive
what lowering may emit.

`anthropic-messages` carries images + documents (no audio/video);
`bedrock-converse` carries images + documents + **video (Nova)** — all three are
in-scope `BlockKind`s. Neither carries audio.

### 4.4 a2aserve inbound media — **{M4}**

Today inbound A2A file parts are spooled to disk (path in the prompt). Two
options:

- **{M4a} Keep spooling (default).** Safe and protocol-agnostic; audio/video is
  available to the agent's tools but not to the model as native media.
- **{M4b} Inline when the model accepts the modality.** New option
  `inboundMedia: spool | inline`: with `inline`, known media parts (with bytes)
  become `MessageOptions.Files` with `Source` set (`inline` for bytes, `url` for
  an A2A URL part) when the target model declares the modality (the same planner
  then decides), else spool. This is what lets an A2A caller's audio reach a
  multimodal model (e.g. vLLM/Gemini) as media rather than a path.

Recommendation: default `spool` (unchanged), add `inline` opt-in.

### 4.5 Size / large-media policy — **{M5}**

- **No interception.** The SDK enforces **no** media size or count caps; the
  provider is the authority and its rejection surfaces as a typed `provider_error`.
  This supersedes the earlier idea of a catalog `MaxInlineBytes` policy and of
  omitting on a size cap. Keep only a **memory-safety bound** on decoded bytes (to
  avoid OOM when decoding inline base64), not a media-size policy.
- The `session.next.attachment.omitted` event still gains a **`reason`** for the
  cases the planner *does* omit — `unsupported_type` | `unsupported_modality` |
  `unsupported_source` | `unsupported_role` | `invalid_payload`. This touches a
  **durable** event (`SessionEventAttachmentOmitted`, `Version: 1`), its typed data
  (`SessionEventAttachmentOmittedData`) and the runner producer; the version bumps.
  `too_large` is no longer a local omit reason.
- Provider-side caps (Anthropic 10 MB on the API / 5 MB on Bedrock-GCP; DeepSeek
  32 MB inline / 64 MB file; per-request counts) are surfaced only when the provider
  rejects; the SDK does not pre-check them.
- Provider file-upload APIs (Gemini/OpenAI/DeepSeek Files) are covered by the
  `MediaUploader` hook ({M7a}, {M12}).

### 4.6 Subagent usage (the user-facing outcome) — **{M6}**

The motivation (§1) keeps the main model cheap and delegates media to a multimodal
subagent. Per-agent models exist ({M1}/{M2}), but the **delivery path does not**
(G9): the `task` tool input is text-only and the child is seeded with
`Prompt{Text: prompt}` (`task.go`, `task_run.go`), so an attachment on the parent
never reaches the child — and the main model cannot forward it (it never receives
the media; its own gate omits it). **Closing this is part of the deliverable.**
Both paths are in scope:

- **{M6a} Session-level multimodal model.** The session's own model is multimodal,
  so attachments gate directly against it. Ship first (it exercises the whole media
  pipeline); it is not the endpoint.
- **{M6b} The child loads media from a reference.** The `task` prompt/metadata
  carries a reference — the spooled A2A path, a provider ref, or a host path — and
  the child loads it via a media-capable `read` tool. Requires `read` to emit
  audio/video as media (currently image-only) and the child agent to run with it.
- **{M6c} The `task` tool carries attachments.** Add a files/`MediaRef` field to
  `TaskInput` and admit them in `runChildSession`. The parent model cannot emit the
  bytes, so a host or an earlier structured step supplies the reference; this
  covers caller-driven flows and forwarding a parent's attachment by id.

Choose and specify the mechanism ({M6b} as the general child path, {M6c} for
explicit forwarding), with the **same planner/gate applied in the child session**.
Do not declare the subagent outcome done until it is implemented and E2E-tested.

The agent/catalog mechanics are unchanged once delivery is solved:
1. Add a catalog entry for the multimodal model (e.g. `gemini-2.5-flash`).
2. Define an agent that names it, e.g. `defs`/code:
   ```markdown
   ---
   description: Transcribe or review an attached audio/video clip.
   model: gemini-2.5-flash
   mode: subagent
   ---
   ...
   ```
3. Expose it in `agents:` (allow-list) / A2A cards.

### 4.7 Observability

Already in place: unsupported media → text note + durable
`session.next.attachment.omitted`; provider rejection → durable
`session.next.step.failed` + assistant `error`. {M5} adds a `reason`.

### 4.8 Media sources and transport modes — **{M7}**

A media part is not only its bytes: providers accept media through several
**sources**, and the correct source depends on the provider and the size. Model
this explicitly instead of assuming inline base64.

**Source kinds**

| Source | Meaning | Local validation | Examples |
|---|---|---|---|
| `inline` | bytes carried in the request (base64 / data URL) | yes (`ValidateMedia`: format, size) | `image_url.url = "data:image/jpeg;base64,…"` |
| `url` | an `http(s)` reference the **provider fetches** | none (format only) | `image_url.url = "https://…/a.png"` |
| `file` | a **provider-native reference** — an uploaded file id (`file_id`) or a provider object URI (`fileUri`, `s3://…`) | none (trust the ref) | `{"type":"file","file_id":"file-api-…"}`, `{"source":{"s3Location":{"uri":"s3://…"}}}` |

DeepSeek exposes all three for images (inline / external URL / Files API
`file_id`), with different limits per source; Gemini has inline (`inlineData`)
plus a Files API (`fileData.fileUri`); Anthropic has base64/url/file (`file` needs
the `anthropic-beta: files-api-2025-04-14` header, and is unavailable on
Bedrock/GCP — base64 only there); Bedrock Converse uses `bytes` (inline) or an
`s3Location` reference. The SDK carries the source and lets each protocol decide.
Two provider specifics to note:

- Inline is not one wire form: DeepSeek accepts a `data:` URL **or** a `file`
  block with `file_data` + `filename`; the protocol picks the encoding, the
  shared `Source` only says `inline`.
- The Anthropic `file` source is gated by a beta **header**, which belongs on the
  model (`ModelRequest.Headers`), since DeepSeek serves it on its own
  Anthropic-compatible endpoint (§4.10, Axis 3).

**One source, several wire encodings.** `Source` is an abstraction; the exact wire
form is the protocol's job:

| Source | Wire encoding (examples) |
|---|---|
| `inline` | image `image_url.url = "data:<mime>;base64,…"`; audio `input_audio = {data:"<bare base64>", format:"wav"}` **or** `audio_url.url = "data:…"`; video `video_url.url = "data:video/…;base64,…"`; DeepSeek image: data URL **or** a `file` block with `file_data`+`filename`; Gemini: `inlineData` |
| `url` | a block's `url = "https://…"` that the provider fetches (OpenAI/vLLM/DeepSeek/Gemini); the SDK never fetches |
| `file` | a provider-native reference: DeepSeek/OpenAI `file_id`, Gemini Files `fileUri`, Anthropic `file` (+ beta header), **Bedrock `s3Location` (`s3://…`)**. **Not vLLM** — it has no Files API; its `file://` is a *server-local path* (`--allowed-local-media-path`), which is **not** this `file` source |

So `inline` alone maps to at least three shapes: a `data:` URL, a bare base64
string plus a `format` (`input_audio`), or a `file_data` block. The protocol
lowers the source; the descriptor says which sources/shapes it may emit.

**Representation**

- Extend `llmschema.MediaPart` with a `Source` (`inline` | `url` | `file`) and
  keep `Data any` for the value (base64 string, http URL, or provider ref).
  Default (`empty`) = `inline`, so existing callers are unchanged.
- Extend `schema.PromptFileAttachment` (and `ToolFileContent`) with the same
  `Source` plus an optional `FileID`; today `URI` alone is ambiguous. Scheme
  inference is a fallback only (`data:` → inline, `http(s)://` → url) — explicit
  wins.
- Keep the SDK's "never read a path/URL" rule: for `url`, the **provider**
  fetches; the SDK does not.

**Gate** (the planner, §4.10): for `inline`, keep MIME+modality+payload; for
`url`/`file`, MIME+modality only (no local payload), plus protocol support of the
source and the role. Unsupported source/type/modality/role → omit +
`session.next.attachment.omitted` with a `reason`.

**Per-protocol transport support** (open only what the wire expresses):

| Protocol | inline | url | file |
|---|---|---|---|
| `openai-chat` | yes | yes | no (OpenAI proper; `input_file` documents are a distinct block, not this `file` source) |
| `openai-compatible-chat` | yes | yes | provider-specific: DeepSeek `file`+`file_id`; **not vLLM** |
| `openai-responses` | yes | yes | yes (DeepSeek `input_image` + `file_id`) |
| `anthropic-messages` | yes | yes | no (document block not lowered) |
| `gemini` | yes (`inlineData`) | yes (`fileData.fileUri`) | via Files API `fileUri` |
| `bedrock-converse` | yes (`bytes`) | no | yes (`s3Location`, `s3://…`) |

This table is the `ProtocolMediaSupport.Sources` column; it must match the
lowering and `body_schema.go` (§4.10).

**Sources vs blocks.** A *source* says where the bytes come from; a *block* is the
wire shape. Protocols also have a distinct **`document`** block for `pdf`
(Anthropic/Bedrock; Bedrock requires an accompanying `text` block; Gemini uses its
generic file block) and Bedrock Converse a **`video`** block (Nova). These are
`BlockKind`s chosen by modality, not sources — the planner picks the source, the
protocol picks image vs document vs video.

**Files API upload — host responsibility, optional hook.** Uploading is a
side-effecting provider call with storage/quota/expiry semantics (DeepSeek:
`POST /files`, `purpose=user_data`, 64 MiB, optional `expires_after`). Per
`AGENTS.md` (no provider/credential infrastructure in the SDK), the SDK
**references** `file_id`s; uploading is the host's job via the `MediaUploader`
hook ({M7a}, {M12}). The hook returns a resolved ref (id *or* URI, see §4.10),
which the planner writes onto the part before re-checking `file` feasibility.

### 4.9 Image detail level — **{M8}**

Add an optional `Detail` (`low` | `high` | `original` | `auto`) to
`PromptFileAttachment`/`MediaPart`, lowered where the protocol supports it
(`openai-chat`/`openai-responses` `detail`; ignored elsewhere). Default: unset
(provider default).

### 4.10 Extensibility architecture — **{M9}**, **{M13}**, **{M14}**

The hard part is not any single model but the **three axes that vary
independently**:

1. **Model capability** — which modalities a model accepts, and the constraints
   (sources, inline size).
2. **Wire protocol** — what a protocol can express and how it encodes it.
3. **Provider API** — extra endpoints (file upload/list/delete), URL fetching,
   quotas, expiry.

A design that hardcodes any axis into the others resists change. Keep each axis in
**one extension point**, introduce **one canonical media value**, and put every
decision in **one pure planner** — so a new model is data, a new protocol is a
file + a registry entry, a new provider API is a host hook, and the call sites
(`to_llm_message.go`) never change.

**One canonical media value.** Every input (host attachment, A2A file part, tool
result) normalises to `llmschema.MediaPart`, the only shape protocol lowering
sees:

```go
type MediaPart struct {
    Type      string
    MediaType string
    Source    MediaSource // "inline" | "url" | "file"; empty = inline
    Data      any         // inline: []byte|base64|dataURL; url: string; file: fileID
    Filename  *string
    Detail    string      // "" | low | high | original | auto
    Metadata  map[string]any
}
```

`Source` is **declared by the input** (blank infers: `data:` → inline,
`http(s)://` → url), never chosen by an SDK preference ({M10}).

**Axis 1 — model capability (data).** `schema.ModelInfo.Capabilities` is already
host-supplied and per model. Add a constraints descriptor additively, so existing
hosts that set only `Input` keep working:

```go
type ModelCapabilities struct {
    Tools  bool
    Input  []string              // modalities: text,image,audio,video,pdf (unchanged)
    Output []string
    Media  *ModelMediaCapability `json:"media,omitempty"` // new
}
type ModelMediaCapability struct {
    Sources []string // inline,url,file this model accepts; empty = protocol decides
}
```

New **models** need no code — the host declares them ({M1}). New **modalities**
are a name plus one mapping in `utils.MediaModality` and the protocol's
`BlockKind` list (from which `MIMEs` are derived).

**Axis 2 — wire protocol (code, registry).** Replace `ProtocolEntry.MediaMIMEs`
with a descriptor that is the **single source of truth** for the protocol:

```go
type ProtocolMediaSupport struct {
    Encodings MediaEncodingProfile // ordered block kinds; each implies its modality and source
    Roles     []string             // roles that may carry media, e.g. user, tool (DeepSeek: user only); empty = all
}
// MIMEs and Sources are derived from Encodings, not maintained separately.
```

A protocol encodes media in **three** places: the registry descriptor, the
lowering functions, and the request-body JSON schema (`body_schema.go`). Today the
last two hardcode their own MIME sets, separate from the registry; when they
disagree, the gate passes a part that lowering then rejects, failing the turn
instead of omitting gracefully. **The descriptor must drive all three**; lowering
and body schema read it and carry no independent list.

**Same protocol, different provider encodings — the protocol factory.** The
variation that matters is not just MIME/source but the **wire encoding** (which
content block). `openai-chat` (OpenAI proper) and `openai-compatible-chat` (vLLM)
share a wire family but differ: `input_audio` is common, while vLLM adds
`video_url`/`audio_url` and DeepSeek adds a `file`+`file_data` block. A protocol's
lowering is fixed per `route.Protocol.Body.From` (`llm/route/protocol.go`), and
today the two OpenAI entries **reuse `OpenAIChatProtocol`**, so they cannot lower
differently — exactly the extensibility gap. Make the protocol a **factory
parameterized by an encoding profile** (data):

```go
type BlockKind string
// openai:  imageURLDataURL | inputAudioBase64 | audioURLDataURL | videoURLDataURL | openAIFileBlock
// gemini:  geminiInlineData | geminiFileData
// anthropic: anthropicImageBase64 | anthropicImageURL | anthropicImageFile | anthropicDocument
// bedrock: bedrockImageBytes | bedrockImageS3 | bedrockDocument | bedrockVideoBytes | bedrockVideoS3

// A BlockKind implies its modality and source; the profile is the ordered set of
// blocks the wire may emit.
type MediaEncodingProfile []BlockKind

func OpenAIChatProtocolWith(p MediaEncodingProfile) route.Protocol // Body.From, body schema and ProtocolMediaSupport all derive from p
```

The registry then registers `openai-chat` and `openai-compatible-chat` with their
own profiles over the same factory, and each entry's `ProtocolMediaSupport`
(its `Encodings`, `Roles`, plus the **derived** `MIMEs`/`Sources`) is **compiled
from that profile** — one source of truth, not a descriptor kept beside the
factory. A **new provider extension** = a new profile (data) or a new `BlockKind`
encoder (code) + a registry entry; a **genuinely different API** = a new protocol
file (like `gemini`). Either way the call sites (`to_llm_message.go`) never change.

**Axis 3 — provider API (code, host-injected).** Split by kind:

- **Declarative** request shaping (headers/body/path/variant) is already
  expressible with `ModelInfo.API` + `ModelRequest` + variants — **no new code**.
  Example: DeepSeek's Anthropic Files API needs the header
  `anthropic-beta: files-api-2025-04-14`; a header is config, not a hook.
- **Side-effecting** endpoints are hidden behind a host hook (constructor
  injection, like `Credentials`), so the SDK never owns credentials or
  infrastructure:

```go
// MediaRef is a resolved provider reference to uploaded media: the source kind
// ("file") plus the provider's id/uri (DeepSeek/OpenAI file_id, Gemini fileUri).
type MediaRef struct {
    Source MediaSource
    ID     string
}

type MediaUploader interface {
    Upload(ctx context.Context, part llmschema.MediaPart) (MediaRef, error)
}
// ClientOptions.MediaUploader resolves one per provider id (see {M12}).
```

The SDK only *calls* it during adaptation; absent a hook there is no conversion. A
**new provider API** = one `MediaUploader` the host registers. Adaptation is a
**closed loop**: upload → write the returned ref back onto the part
(`Source = file`) → re-check `file` feasibility → encode; a `MediaRef` (not a bare
id) is needed because providers return an id *or* a URI.

**The planner — one pure function.** `mediaPolicyForModel` becomes a planner that
decides, per part, `Encode` / `Upload` / `Omit(reason)`, from the two descriptors
plus the runtime's abilities:

```
feasible(part, model, proto, role) =
    modality(part.MIME) ∈ model.Input                 # empty = unconstrained
  ∧ part.MIME ∈ proto.MIMEs
  ∧ part.Source ∈ proto.Sources
  ∧ (model.Sources empty ∨ part.Source ∈ model.Sources)
  ∧ role ∈ proto.Roles
  ∧ (part.Source != inline ∨ withinSafetyBound(part)) # OOM guard only, not a size cap
```

Every set in the formula follows one rule: **empty = unconstrained** (`model.Input`
empty = any modality; empty `MIMEs`/`Sources`/`Roles` = allow all). A
protocol/model that allows nothing declares a non-empty set that matches nothing;
an empty set never silently means "none".

**Consequence for unknown models.** Empty `Input` is **not** "text-only" — it is
"no modality gate", so protocol-supported media is sent even if the model does not
accept it. To make a text-only model omit media, the host must **declare
`input: [text]`**. The fallback is two-layered: with a **declared** capability, the
planner omits the part gracefully (text note + `session.next.attachment.omitted`)
and the turn proceeds; with an **undeclared or mis-declared** capability, the part
is sent and the provider is the authority — on rejection the turn fails
(`session.next.step.failed`) and `DropErroredTurns` (default on in both commands)
excludes that turn from later contexts so the session is not wedged.

Real source transitions are one-directional:

| transition | needs | feasible |
|---|---|---|
| inline → file | `MediaUploader` | yes (adapt) |
| file → inline | fetch content | no (SDK never fetches) |
| url → inline | fetch content | no |
| any → url | hosting | no |

So: use the declared source if feasible; else, if it is `inline` and an uploader
is available and `file` is feasible, upload and re-encode; else omit with a
`reason` and publish `session.next.attachment.omitted`. The SDK never reads a path
or fetches a URL.

**Choosing the source and encoding — config declares capability, not method.**
The provider catalog does **not** pick the transport; it declares which sources
are accepted (`Sources`). The *encodings* belong to the **protocol profile**
(§4.10 below), not the catalog. The method is chosen per attachment:

1. **The input declares `Source`** — bytes → `inline`, a public link → `url`, an
   uploaded id → `file` (URI scheme is only a fallback: `data:`→inline,
   `http(s)://`→url; explicit wins). The SDK never guesses, never fetches, never
   reads a path.
2. **The model ∩ protocol descriptors filter it.** A declared source outside the
   intersection is *adapted* (only `inline → file`, via a `MediaUploader`) or
   omitted with a `reason`. No size/count cap is applied (§4.5).
3. **Among the encodings of a feasible `(modality, source)`,** the profile's
   ordered list picks the **first** match. This is why DeepSeek image `inline` may
   be a data URL *or* `file`+`file_data`, and vLLM audio `inline` may be
   `input_audio` or `audio_url` — the protocol's fixed order decides, with **no
   per-attachment override** (add one only when a real caller needs it).

Concretely: a DeepSeek entry says `sources: [inline, url, file]` — whether a given
image travels as a data URL, an external URL or a `file_id` follows from how the
attachment arrived (and its size); a vLLM entry says `sources: [inline, url]`, so a
`file`-declared part is omitted (no Files API) while inline/url lower to
`image_url`/`input_audio`/`video_url`. The user picks the *source* by how they
supply the media; the *encoding* is the protocol's fixed order.

**Handoff.** The planner resolves the final `Source` and writes it back onto the
`MediaPart`; the protocol's lowering reads **only** that resolved value and never
re-runs feasibility — one decision, in one place.

**Role.** Roles use a **normalized** set — `user`, `assistant`, `tool` — and each
protocol maps its own names onto it (`tool_result` (Anthropic), `functionResponse`
(Gemini), `toolResult` (Bedrock) → `tool`; Responses `developer` → `user`). The
runner knows the containing message's role as it converts each message and passes
it into the planner, which enforces `role ∈ proto.Roles` (e.g. DeepSeek allows
media only in `user`; Anthropic/Gemini also allow it in tool results).

**Per-model capability at every layer — {M11}.** The capability descriptor is
first-class and always per model: the SDK reads it from
`ClientOptions.ModelInfo(id)`; each `models:` entry carries its own `media:`
descriptor; there is **no** server-wide capability value. The planner is evaluated
on the **target session's** model (`mediaPolicyForModel(model, agent)`), which is
what lets a text-only main model and a multimodal model coexist.

**Provider hooks are keyed by provider — {M12}.** A catalog may span providers,
only some of which have a Files API, so the uploader resolves per provider id,
mirroring `Credentials(providerID)`:

```go
// ClientOptions.MediaUploader: nil, or a per-provider resolver.
func(providerID schema.ProviderID) MediaUploader   // returning nil = no file API
```

**Extension recipes**

| Want to add | Change | Kind |
|---|---|---|
| a new model (any modalities) | catalog/config entry + `ModelInfo` | data |
| a new modality (e.g. `pdf`) | `MediaModality` mapping + protocol MIME lists | small code |
| a provider extending an existing protocol (e.g. vLLM `video_url`) | one `MediaEncodingProfile` (+ maybe one `BlockKind` encoder) + registry entry | data / small code |
| a new wire protocol | one protocol file (lowering + body schema) + registry entry | code |
| a new provider upload API | one `MediaUploader` impl, host-injected | code/host |
| a new provider declarative quirk | config (headers/body/path/variant) | data |
| a new source kind (e.g. base64 chunking) | enum + descriptor `Sources` + resolver branch | code |

**Invariants.** Unknown modality/source/MIME/role is always a graceful *omit with
reason*, never a panic or a failed turn; the planner is pure and
protocol-agnostic; capabilities are data (host), support is registry (protocol),
side effects are hooks (host), and the call sites (`to_llm_message.go`) never
change.

### 4.11 Consumer surfaces — **{M15}**

The engine changes (§4.1–§4.10) are not enough: each consumer surface must
**attach** media, **advertise** what the target model accepts, and **report**
omissions/rejections.

**SDK API (root package)**
- Attach: `MessageOptions.Files []schema.PromptFileAttachment` gains
  `Source`/`FileID`/`Detail` (additive; `session.go`).
- Capability: `ClientOptions.ModelInfo(id).Capabilities.Media.Sources`;
  `Credentials(providerID)`; new `ClientOptions.MediaUploader(providerID)`.
- **Outcomes (new).** Add a high-level `AttachmentOmitted` event
  (`Name`, `MIME`, `Reason`) to `events.go` — today the event list has **no** such
  event, so a Go host cannot perceive an omission (or its new `reason`) without
  reading durable rows.
- **Tool authors.** `core/tool.FileContent` (the tool-facing type, `tool.go`) gains
  `Source`/`FileID` too — not only `schema.ToolFileContent` — so a tool can return
  audio/video/URL media.
- **`MediaRef` contract.** Carries an optional expiry (so the content-hash cache can
  evict and re-upload), and the resolved ref is **persisted** on the message (as
  `PromptFileAttachment.FileID`) so a rebuilt turn or a restart reuses it instead
  of re-uploading.

**cmd/agentserve (HTTP/SSE)**
- Config: the `models:` catalog (in `serveconfig`) with per-entry
  protocol/baseURL/credentials/`media`. The catalog is **config-file-only**; the
  single-valued CLI flags set only the **default** entry (when `models:` is
  present, per-model config cannot be given on the command line).
- `POST /api/session/{id}/prompt` `files[]` accepts `source`/`fileId`/`detail`
  (shared struct; `server/session.go`).
- **New `GET /api/model`** (alongside `/api/agent|skill|tool`, `server/server.go`):
  lists catalog ids with protocol, modalities and accepted sources, so a caller
  knows what it may send.
- **Large media / `file` source — expose an upload endpoint.** Add
  `POST /api/session/{id}/file` backed by the host `MediaUploader` for the
  session's model provider; it returns the resolved ref (`fileId`), which the
  caller then sends as `files: [{source:"file", fileId:"…"}]`. A `file` ref is
  **provider-bound**: if the session's model/provider changes, the caller must
  re-upload (a mismatch surfaces as a `provider_error`). `inline`/`url` remain
  available without uploading.
- Omission is visible via the existing SSE `/api/session/{id}/event`; ensure
  `reason` is in the payload.

**cmd/a2aserve (A2A)**
- Config: `models:` catalog + `inboundMedia: spool | inline`.
- Inbound: `{M4b}` maps `fileWithBytes` → `inline`, `fileWithUri` → `url`, admitted
  as `MessageOptions.Files`.
- **AgentCard.** `inputModes`/`outputModes` are **derived from the agent's model
  media capabilities** (true → `text/plain`; image/audio/video → the protocol's
  MIME sets), replacing the fixed text+image `DefaultInputModes`
  (`a2a/server/card.go`); explicit card config overrides.
- **Outbound** (SDK as A2A client): `inline` → `fileWithBytes`, `url` →
  `fileWithUri`. A provider `file` ref is **omitted with a reason** — it names
  *our* provider's object and is not portable across A2A (the remote agent has no
  access to it); a caller that must forward such media resolves it to inline/url
  first.
- **Omission.** Map `session.next.attachment.omitted` onto an A2A event/status so a
  remote caller learns media was dropped (`a2aEventFrom` does not map it today).

**Cross-surface invariant (3 verbs).** Every surface must be able to **attach**,
**advertise** (the target model's modalities/sources), and **report** (typed
omission/rejection reason).

## 5. What does **not** change
- `llmschema.MediaPart` / `schema.PromptFileAttachment` stay wire-generic; {M7}
  only **adds optional fields** (`Source`, `FileID`, `Detail`), so existing
  inline callers are unchanged.
- The omission/degrade behavior and event (except the new `reason`).
- Per-agent model resolution (already per-agent). The `task` tool **will** gain a
  media-reference path ({M6b}/{M6c}) so a child session can receive media; until
  then the session's own model ({M6a}) carries attachments.
- Public SDK API: `ModelInfo`/`Credentials` providers already resolve per id; the
  capability descriptor is additive.
- No provider credential/infra in the SDK: Files API **upload** stays with the
  host ({M7a}).

The gate itself is refactored from three scattered checks into one planner
(§4.10); that is the main internal change.

## 6. Phases

| Phase | Content |
|---|---|
| P0 | This document; confirm the remaining decisions. Scope is the **full feature set** (no MVP cut). |
| P1 | {M13} architecture + {M1}/{M2}: lift the `examples` catalog shape into a `serveconfig` catalog (override `ProviderID`, per-entry credentials/limits, shared config type) and wire both commands. Backward compatible. |
| P2 | {M13} planner + {M14} factory: refactor the gate into one planner driven by the descriptors, introduce the protocol factory (`OpenAIChatProtocolWith(profile)`), and make lowering + `body_schema.go` read the same descriptor (removes the three-source-of-truth bug). |
| P3 | {M6a}: end-to-end media via the session's own model — docs + example config; the §7.1 **vLLM** harness (text/image/audio/video) over the `openai-compatible-chat` wire; the §7.2 matrix over the session-model path. |
| P4 | {M3}: OpenAI audio lowering (`input_audio`), plus the `openai-compatible-chat` video/audio extensions (`video_url`/`audio_url`); protocol unit tests. |
| P5 | {M4}: a2aserve `inboundMedia` (`spool` or `inline`); A2A test. |
| P6 | {M5}: modality-aware size limit + `reason` on the omission event (durable version decision). |
| P7 | {M7}: media `Source` (`inline`/`url`/`file`) end to end — schema fields, planner, per-protocol lowering (OpenAI `url`+`file`, Gemini `fileData.fileUri`), `MediaUploader` hook. |
| P8 | {M8}: image `detail` option. |
| P9 | {M6b}/{M6c}: the subagent delivery path (read-tool media, or task-tool attachments). |
| P10 | {M15} consumer surfaces (§4.11): SDK `AttachmentOmitted` event + `core/tool.FileContent`; agentserve `GET /api/model`; a2aserve model-derived card modes + omission mapping + outbound `Source`. |

Phases are **implementation order, not scope**: every phase is required for the
full capability — there is no MVP cut. P1–P3 stand up the pipeline and the
session-model path (validated locally against **vLLM**, image/audio/video over
`openai-compatible-chat`); P4–P8 close the protocol/transport/size/detail gaps;
P9 delivers the subagent path (§4.6); P10 makes every consumer surface attach,
advertise and report media (§4.11).

## 7. Test plan

### 7.1 Local integration harness — vLLM (openai-compatible-chat)
vLLM's OpenAI-compatible server (`vllm serve <model>`) exposes
`/v1/chat/completions` and accepts text plus `image_url`, **`input_audio`
(OpenAI Audio shape)**, **`video_url`** and **`audio_url`** (vLLM extensions). So
**one local server covers image, audio and video** with no cloud key — the whole
media matrix runs E2E locally:

```yaml
model: Qwen/Qwen3-Omni-30B-A3B-Instruct   # image + audio + video
protocol: openai-compatible-chat
modelBaseURL: http://localhost:8000/v1
media:
  input: [text, image, audio, video]
  sources: [inline, url]                  # data URL or http(s); vLLM fetches url
credentials: { apiKey: EMPTY }            # vLLM ignores the key
```

Local models by modality (all `vllm serve`-able):

| Modality | Example model |
|---|---|
| image + video | `Qwen/Qwen3-VL-8B-Instruct`, `llava-hf/llava-onevision-qwen2-0.5b-ov-hf` |
| audio | `fixie-ai/ultravox-v0_5-llama-3_2-1b`, `openai/whisper-large-v3-turbo` |
| image + audio + video | `Qwen/Qwen3-Omni-*`, `Qwen/Qwen2.5-Omni-*` |

**Prerequisite — what works now vs what needs code.** Pointing the SDK at a vLLM
server needs **no SDK change** for text + image (vLLM speaks OpenAI Chat
Completions; `image_url` is standard). The audio/video rows only pass once
**P4/{M3}** lands — `input_audio` plus vLLM's `video_url`/`audio_url` — because
until then the SDK's lowering emits only `image_url` and the gate omits the rest
(§2.3 scope note, §3 G3, §4.3).

Facts that shape the SDK:

- vLLM's audio block `input_audio` is the **same shape as OpenAI's**, so {M3}
  covers both; its `video_url`/`audio_url` are **vendor extensions** of the OpenAI
  Chat API. This is why `openai-compatible-chat` and `openai-chat` need **separate
  `ProtocolMediaSupport`** even though they share `OpenAIChatProtocol` (§4.10).
- Video inline is a **data URL** (`data:video/jpeg;base64,…` frames, or an mp4 data
  URL), not a bare base64 string; the `inline` value must carry data URLs.
- `url` sources are fetched by vLLM (`--allowed-media-domains` and
  `VLLM_MEDIA_URL_ALLOW_REDIRECTS=0` guard SSRF); the SDK never fetches.
- No Files API — sources are `inline` + `url` only.

**Accepted media forms (per block).** The `url` fields take a host URL or a data
URL; audio additionally has a bare-base64 form. Local paths are a server-side
option, not a Files API:

| Form | `video_url` | `audio_url` | `input_audio` | `image_url` |
|---|---|---|---|---|
| `http(s)://` host (vLLM fetches) | yes (30s) | yes (10s) | no | yes (5s) |
| data URL `data:<mime>;base64,…` | yes | yes | no | yes |
| bare base64 string | no | no | yes (`{"data":"…","format":"wav"}`) | no |
| local `file:///abs/path` | yes* | yes* | no | yes* |

\* needs `--allowed-local-media-path`; explicitly documented for images, and
audio/video go through the same media loader. `file://` is a **server-local
path**, not this SDK's `file` (file-id) source — vLLM has no Files API (`uuid` is
only a cache key). In SDK `Source` terms: `url` → the `url` fields; `inline` →
the data-URL form, or `input_audio` (bare base64 + `format`) for audio; `file` →
not applicable to vLLM.

**Cost/placement.** vLLM needs a GPU and model weights (the Omni audio+video
models are large), so this is a **separate E2E tier** (nightly / manual / a GPU
dev box), not part of `go test ./...`. Use the deterministic fixtures below so a
failure means a real regression, not a snapshot diff.

> Implemented: the harness lives in `e2e/media_test.go` behind the `e2e` build
> tag. Set `VLLM_BASE_URL` and `VLLM_MODEL` and run `go test -tags e2e ./e2e/...`;
> it skips otherwise, so the default suite stays offline.

Deterministic fixtures (offline-generated, committed small): an image, an audio
clip and a short video each embed a known token; assert the model's answer
contains it (routing in §7.3, assertions in §7.4).

> The **local** harness is vLLM (covers image/audio/video without a key). The same
> E2E also runs the **real providers** with whatever credentials are configured —
> a **free tier** during development/CI to keep it cheap (e.g. Gemini Flash
> audio/video), **paid** in production/release; the code path is identical (§8).
>
> The vLLM harness uses the **session's own model** ({M6a}); the subagent path
> ({M6b}/{M6c}) is E2E-tested once its delivery mechanism lands (§4.6/P9).

### 7.2 Capability matrix (target model × attachment)
The planner is evaluated on the **target session's model**. Each model declares a
modality profile:

| Profile | Input modalities |
|---|---|
| `T` | text |
| `TI` | text, image |
| `TA` | text, audio |
| `TV` | text, video |
| `TIAV` | text, image, audio, video |

Every model × attachment pair is covered by two assertions:
1. an attachment whose modality the model supports is delivered to that model's
   provider request;
2. an attachment whose modality the model lacks is omitted (text note) and
   reported as `session.next.attachment.omitted` — and never fails the turn.

Representative cases (the attachment is sent to a session whose model is
`Target`):

| Target | Attachment | Expected |
|---|---|---|
| `TI` | image | delivered (`openai-chat` wire; stub or vLLM) |
| `T` | image | omitted + event |
| `TIAV` | audio | delivered (`input_audio` on vLLM); omitted where the wire/model lacks it |
| `TA` | audio | delivered (vLLM/stub); omitted on a model/wire without audio |
| `TV` | video | delivered (`video_url` on vLLM/stub) |
| `TI` | video | omitted + event |

The **"main × subagent"** form of this matrix — an attachment routed through the
`task` tool to a differently-modeled child session — is the P9 target. Until
delivery exists ({M6b}/{M6c}), "target" is the session's own model, and the
parent-model fallback (a subagent that names no model uses the parent's) only
applies once that path lands.

### 7.3 Where each modality is validated (E2E)

| Modality / source | vLLM model | Stub/unit |
|---|---|---|
| text, image (inline/url) | `Qwen/Qwen3-VL-8B-Instruct` | any |
| audio (inline/url) | `fixie-ai/ultravox-v0_5-llama-3_2-1b` | request-capturing stub; OpenAI `input_audio` unit test (P4) |
| video (inline/url) | `Qwen/Qwen3-VL-8B-Instruct`, `llava-hf/llava-onevision-qwen2-0.5b-ov-hf` | request-capturing stub |
| image + audio + video | `Qwen/Qwen3-Omni-*` | request-capturing stub |

The stub asserts the exact `MediaPart` (MIME + source + base64) that reaches
`llm.Stream`; vLLM is the real provider that proves the wire is accepted.

### 7.4 Unit and other
- **Planner ({M13})**: table tests over `(modality, source, role, size) × model
  descriptor × protocol descriptor` asserting `Encode` / `Upload` / `Omit(reason)`;
  adaptation is only `inline → file`; source defaults from the URI.
- **Single source of truth**: a test that each protocol's lowering accepts exactly
  the MIME/source set its `ProtocolMediaSupport` declares (and `body_schema.go`
  covers it), so the gate and lowering cannot drift.
- **Protocol factory ({M14})**: each registry entry emits exactly its profile's
  `BlockKind`s — `openai-chat` rejects `video_url`/`audio_url` while
  `openai-compatible-chat` emits them; DeepSeek emits `file`+`file_data`; the
  chosen encoding is the profile's first match.
- **Protocol**: Gemini audio/video lowering (generic `inlineData`); OpenAI audio
  lowering (P4) — assert request body.
- **Gate**: replace the `MediaAllowed` / `ProtocolEntry.MediaMIMEs` checks with
  the descriptor-driven planner (extend `llm/protocols/media_test.go`).
- **Transport ({M7})**: an `http(s)` URL is carried as a URL (not base64);
  `inline` still data-URL; `file` lowers to the provider's `file`/`file_id`
  block; each protocol omits sources it cannot express (with `reason`).
- **Detail ({M8})**: `detail` appears in the OpenAI body and is ignored
  elsewhere.
- **Catalog config**: id → protocol/credentials/modalities; unknown id →
  default; per-provider credential.
- **A2A**: inbound `inline` vs `spool` (P5).

### 7.5 Optional local protocol shim (dev/test only)

To exercise the **Gemini / Anthropic transports** end to end for **free and
deterministically**, a small in-process shim can accept a Gemini- or
Anthropic-shaped request and forward it to a local vLLM OpenAI-compatible model.
This is **test tooling only** — never used in deployment (a deployment that uses
the Gemini protocol uses Google's paid endpoint). It proves the SDK builds a body
the shim can parse and that a real server round-trips it; it does **not** prove
Google's/Anthropic's exact behavior, so keep the real-provider smoke (§7.1) too.
Each protocol's lowering is otherwise covered by contract tests that assert the
exact request body.

> Implemented: `e2e/shim_test.go` (behind the `e2e` tag) — an in-process Gemini
> shim that translates `generateContent` to an OpenAI-compatible upstream and
> emits Gemini SSE back. `TestGeminiTransportThroughShim` runs **offline** against
> an in-process stub (asserting the image/audio blocks reached the upstream); set
> `VLLM_BASE_URL`/`VLLM_MODEL` to forward to a **real vLLM** instead.

## 8. Decisions

**Decided:**
1. **OpenAI Responses audio — no.** OpenAI directs audio-chat to Chat Completions;
   Responses stays images/documents. `{M3}` audio is on `openai-chat` only
   (requires `modalities:["text","audio"]` + an audio model).
2. **Catalog shape — flat `models:` list**, with per-entry `contextLimit`/
   `outputLimit` moved off the top level; no keyed map.
3. **a2aserve inbound — `spool` default, `inline` available** ({M4a}).
4. **`read` tool media — in scope (P9):** it is the {M6b} subagent-delivery
   enabler, not optional.
5. **Uploader — expose `MediaUploader`** ({M7a}/{M12}); the host implements it, the
   SDK calls it during `inline → file` adaptation.
6. **Delivery — both paths in scope:** the session's own model ({M6a}) and the
   subagent path ({M6b}/{M6c}) (§4.6).
7. **`detail` — per-attachment**; unset = provider default; ignored where
   unsupported.
8. **Video transport — inline *and* provider ref** (Bedrock `s3Location`, Files
   API) are both required ({M5}/{M7}).
9. **Back-compat — additive `Source`/`FileID`; omission event gains `reason`**
   (additive, durable version bump).
10. **Caps — do not intercept.** The SDK enforces **no** count/size caps; the
    provider is the authority and its rejection surfaces as a typed
    `provider_error`. Consequence: the current `ValidateMedia` size bound is
    revisited — keep only base64 well-formedness plus a memory-safety bound, and
    stop omitting on a media-size cap.
11. **Cloud E2E — yes.** E2E runs the real providers with **whatever credentials
    are configured** (same code path; key from env). Development/CI may use a free
    tier where one exists (e.g. Gemini Flash audio/video) just to keep it cheap;
    production/release uses paid credentials. Kept out of per-commit runs.
12. **Upload caching — SDK-side.** Dedup by content hash (`provider + hash → ref`,
    expiry-aware); the host only performs the upload. `MediaUploader` stays
    idempotent.
13. **`MediaRef` expiry + ref persistence.** `MediaRef` carries an optional expiry
    for the dedup cache; the resolved ref is persisted on the message
    (`PromptFileAttachment.FileID`) so rebuilt turns / restarts reuse it instead of
    re-uploading.
14. **Consumer surfaces — all three (§4.11).** SDK: an `AttachmentOmitted` event +
    `Source`/`FileID` on `core/tool.FileContent`. agentserve: `GET /api/model` and a
    config-file-only catalog. a2aserve: model-derived card modes, omission mapping,
    and outbound `Source` handling.
15. **HTTP upload + outbound A2A refs (§4.11).** agentserve exposes
    `POST /api/session/{id}/file` (host `MediaUploader`) so HTTP callers can use the
    `file` source (provider-bound; re-upload on provider change). a2aserve outbound
    maps `inline`→`fileWithBytes`, `url`→`fileWithUri`, and **omits** a provider
    `file` ref (not portable across A2A).

All design decisions are resolved. Remaining choices are **test tooling**, not
design — see §7.5.

**Architecture (decided):** the **source is declared by the input** and feasibility
is `model ∩ protocol` ({M10}); capabilities are **per model at every layer**
({M11}); provider hooks are **keyed by provider id** ({M12}); a new model is data
and a new protocol is one file + a registry entry, all decided by **one planner**
over a single canonical media value ({M13}); a protocol is a **factory
parameterized by an encoding profile** ({M14}). DeepSeek's vision / Files API facts
(three sources, 48/32/64 MiB, `file_data`, the Anthropic beta header) are confirmed
against the current docs.

## 9. Production requirements (industrial-grade)

Scope is the full capability, not a simplified subset. Cross-cutting requirements
the implementation must meet:

- **Typed outcomes and observability.** Every media decision is one of
  `Encode`/`Upload`/`Omit(reason)` with a **typed reason**
  (`unsupported_type` | `unsupported_modality` | `unsupported_source` |
  `unsupported_role` | `invalid_payload` | `too_large` | `provider_error`), emitted
  on the durable omission event and surfaced in metrics. Errors carry no media
  bytes or secrets (reuse the transport redaction).
- **Security.** The SDK never fetches a URL and never reads a path from an
  attachment ({M7}); provider-side URL fetch (SSRF) is the provider's concern
  (documented server flags, e.g. vLLM `--allowed-media-domains`). A2A inbound
  spooling is confined to the workspace with sanitized names (no traversal). Size
  is bounded **before** decode; declared MIME is validated against the block's
  expectations (providers may sniff content).
- **Limits — no interception.** The SDK enforces **no** count/size caps; the
  provider is the authority and its reject surfaces as a typed `provider_error`.
  Keep only a **memory-safety bound** on decoded bytes (to avoid OOM), not a
  media-size policy. URL fetch timeouts are provider-side.
- **Concurrency and caching.** The planner is pure and stateless. Uploads are
  deduplicated by content hash (`provider + hash → ref`, expiry-aware); there is an
  existing keyed-mutex/weak-cache pattern in `core`. `MediaUploader`
  implementations must be idempotent. Memory stays bounded: inline is covered by
  the safety cap; large media uses refs.
- **Backward compatibility and versioning.** Schema changes are additive
  (`Source`/`FileID`; `reason`); the durable omission event **bumps its version**;
  unknown source/modality/MIME degrades gracefully, never panics.
- **Config validation.** Catalog entries are validated at startup: known protocol,
  resolved `ProviderID`, per-entry limits, credentials, and a well-formed `media`
  descriptor; invalid config fails fast. A model's declared `media.input`/`sources`
  are the host's contract — a text-only model must declare `input: [text]` (empty =
  unconstrained, §4.10).
- **Testing.** Contract tests (descriptor ↔ lowering ↔ `body_schema.go`), planner
  unit tests, the capability matrix, and E2E against a real provider (vLLM local;
  cloud under keys) plus negative/robustness cases (rejection, oversize, malformed,
  traversal). See §7.
- **Performance.** Account for base64 expansion (~33%) and avoid re-encoding;
  large media goes by reference, not through the request body.
