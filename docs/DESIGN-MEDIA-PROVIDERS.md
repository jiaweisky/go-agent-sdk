# go-agent-sdk — provider-aware media uploads and capability matrix

> Status: **Implemented.** Config schema, profile selection, uploader, provider
> MIME/store-capability gating, conditional model headers, protocol MIME
> coverage and the canonical example matrix are in place.
>
> Scope: make provider-specific Files API uploads and `file_id` references
> explicit, configurable and testable for OpenAI, DeepSeek and Anthropic, without
> scattering provider quirks across the media uploader, protocol registry and
> model catalog.
>
> Related: `docs/DESIGN-MEDIA.md` (media sources, planner, `MediaStore` /
> `MediaUploader`), `docs/DESIGN-AGENTD.md` (agentd configuration and transport).

> **DeepSeek note.** DeepSeek has **two** official Files API variants. The
> default (`https://api.deepseek.com/files`) is **OpenAI-compatible**:
> `Authorization: Bearer` + `purpose=user_data`. A separate optional endpoint
> (`https://api.deepseek.com/anthropic/v1/files`) is **Anthropic-compatible**:
> `x-api-key` + `anthropic-beta: files-api-2025-04-14`. This document models them
> as provider IDs `deepseek` / `deepseek-anthropic` and profile names
> `deepseek` / `deepseek-anthropic-files`; their headers must never be mixed.

## 1. Problem

The current media design has three independent axes (model capability, wire
protocol, host-side hosting), but only the first two have descriptors that can be
read together. Provider-specific **upload** behaviour has no descriptor: it is a
single generic HTTP client plus a flat config block.

That is enough for the simplest case (one endpoint, one `Authorization` header),
but not for the providers the SDK claims to support:

- OpenAI and DeepSeek's **default** Files API use `purpose=user_data`, a Bearer
  token, and a top-level `id`. DeepSeek's documented upload is
  `POST https://api.deepseek.com/files` with `Authorization: Bearer`.
- Anthropic requires two request headers (`x-api-key` and `anthropic-version`),
  no `purpose`, a different expiry field (`expires_in_seconds`), a different file
  type allow-list and a much larger size limit.
- DeepSeek **additionally** documents an Anthropic-compatible Files API under
  `https://api.deepseek.com/anthropic/v1/files`, which is a separate endpoint
  from its default OpenAI-compatible one and requires `x-api-key` plus
  `anthropic-beta: files-api-2025-04-14`. The two variants need distinct
  `providerFileAPIs` entries because their auth, response shape and headers
  differ.
- OpenAI Responses accepts many document types; the SDK currently lowers only
  PDF and images.
- A single server can expose models from multiple providers, but
  `mediaUpload` is one flat config and the uploader factory ignores
  `ProviderID`.

The result is hard to answer two basic questions:

1. Which providers/protocols currently support a provider `file_id`?
2. Which MIME types/files are accepted, and where is that enforced?

### 1.1 Current implementation map

| Layer | Current location | Answers |
|---|---|---|
| Wire support | `pkg/llm/protocols/registry.go` (`ProtocolMediaSupport`) | Which MIME/source/role a protocol can encode |
| Wire lowering | `pkg/llm/protocols/*.go`, `media_profile.go` | Which block (`input_file`, `document`, `file`) is emitted |
| Model capability | `cmd/agentd/config` (`input`, `media.forms`), model catalog | Which modalities a model accepts |
| Inbound delivery policy | `cmd/agentd/config/media.go`, `cmd/agentd/media/policy.go` | inline / url / file / spool preference, size limits |
| URL hosting | `cmd/agentd/config/storage.go`, `cmd/agentd/storage`; exposed at `media.uploads.urlHost` | bytes → `url`; backend http/local/s3/cos, capability report |
| Provider upload | `cmd/agentd/config/media.go` (`ProviderFileAPI`), `cmd/agentd/media/uploader.go` | auth header map, fields, expiry style, MIME/limits, capability report |
| Provider selection | `cmd/agentd/run.go` (`mediaUploaderFor`) | per-server `providerID → profile` map from explicit model references |

### 1.2 Current `file_id` support

| Provider / path | Protocol | `file_id` support today | Notes |
|---|---|---|---|
| OpenAI Responses | `openai-responses` | Yes: `input_file.file_id` (PDF), `input_image.file_id` (images) | descriptor MIMEs = images + PDF only |
| OpenAI Chat Completions | `openai-chat` | No | profile has no file block; only inline/url |
| Anthropic Messages | `anthropic-messages` | Yes: `document.source.file` (PDF), `image.source.file` (images) | descriptor MIMEs = images + PDF only |
| DeepSeek OpenAI-compatible | `deepseek-chat` | Yes: `{"type":"file","file_id":...}` | image-only; `OpenAIFileBlock` |
| DeepSeek Anthropic-compatible | `anthropic-messages` | Wire shape exists | needs provider header + provider MIME restriction |

### 1.3 Current file-type control

File types are controlled by the intersection of:

1. `ProtocolMediaSupport.MIMEs` (wire capability);
2. `MediaEncodingProfile` / `BlockKind` MIME sets (encoding capability);
3. model `input` modalities (`text/image/audio/video/pdf`).

There is **no provider-level MIME allow-list** and **no upload-time check**.
Model input is modality-level, so it cannot express “PDF yes, DOCX no” or
“DeepSeek images only”. Anthropic `text/plain` is supported by its Files API but
not represented in the protocol descriptor.

### 1.4 Consequences

- Anthropic cannot be configured correctly with the current uploader (one
  header, fixed expiry field, no type control).
- DeepSeek Anthropic-compatible cannot be configured (two headers).
- A mixed-provider server cannot upload to different Files APIs.
- A support matrix must be reconstructed by reading five packages.
- Adding a provider requires edits in protocol registry, media profile,
  lowering, model catalog, agentd uploader config and route headers, with no
  single checklist.

### 1.5 Two delivery paths, three configuration surfaces

The core distinction is **where the bytes go before the model sees them**:

```
inbound bytes
  ├─ inline        bytes embedded in the model request
  ├─ url path      bytes → URL host → model/provider fetches the URL
  ├─ provider file bytes → provider Files API → file_id in the model request
  └─ spool         bytes written to disk as a last resort
```

The configuration surfaces map to the paths:

| Field | Responsibility | Provider-bound? | Output |
|---|---|---|---|
| `media.delivery` (`MediaDeliveryConfig`) | delivery policy: which path to try (inline / url / providerFile / spool), size limits, downsample | no (operator intent) | a chosen delivery path |
| `media.uploads.urlHost` (`StorageConfig`) | **URL path** store: bytes → URL | no | `Reference{Source: "url"}` |
| `providerFileAPIs.<name>` (`ProviderFileAPI`) | **provider file path**: bytes → provider `file_id` | yes, exactly one profile per providerID | `Reference{Source: "file"}` |

So `delivery` is the chooser, `storage` hosts a URL, and `providerFileAPIs`
uploads to a provider. `delivery` covers both URL and provider-file paths; it is
not itself an uploader.

The naming problem is real:

- `media` is too generic for a policy;
- `storage` sounds like session/state storage, not specifically the URL path;
- `mediaUpload` sounds like any upload, not a provider-bound Files API;
- `media.upload` sounds like the uploader block but only enables non-inline
  hosting.

Target naming separates the reusable provider Files API registry, the model
reference, and the server policy:

```yaml
providerFileAPIs:     # top-level; reusable provider Files API configurations
  deepseek:
    kind: multipart-json
    url: https://api.deepseek.com/files
    auth: {headers: {Authorization: "Bearer ${DEEPSEEK_API_KEY}"}}
    # fields/expiry/mime/limits omitted here; see §4.5.3

models:               # top-level catalog
  - id: deepseek-flash
    protocol: deepseek-chat
    providerID: deepseek
    providerFileAPI: deepseek     # explicit reference; no inference

servers:              # per-server policy
  - name: public
    models: [deepseek-flash]
    media:
      delivery:
        prefer: [providerFile, url, inline]  # provider Files API first
        allowUploads: true                   # was: media.upload
        inlineLimitBytes: 262144
      uploads:
        urlHost:                 # bytes -> a URL we host; provider fetches it
          backend: s3            # was: storage
          # s3: {bucket: ..., region: ...}
```

Read it as:

- `providerFileAPIs.<name>`: how to upload to a provider's Files API and what
  that API accepts;
- `models[].providerFileAPI`: the exact profile this model uses; no default,
  no inference from `protocol`/`providerID`;
- `media.delivery`: choose the path (`inline`, hosted `url`, provider `file`,
  `spool`) and its limits;
- `media.uploads.urlHost`: upload to **our** URL host (http/local/s3/cos).

This avoids the current ambiguity where `storage` could mean any storage and
`mediaUpload` could mean any upload. The old top-level `mediaUpload` shape is not
supported by the target schema; use `providerFileAPIs.<name>` with
`models[].providerFileAPI`, and `storage` becomes `media.uploads.urlHost`.

## 2. Vendor requirements (as of 2026-10)

Sources: DeepSeek Files API / vision guides, Anthropic Files API, OpenAI file
inputs guide.

| Item | OpenAI | DeepSeek (OpenAI-compatible) | Anthropic |
|---|---|---|---|
| Upload | `POST https://api.openai.com/v1/files` | `POST https://api.deepseek.com/files` | `POST https://api.anthropic.com/v1/files` |
| Auth | `Authorization: Bearer` | `Authorization: Bearer` | `x-api-key` **and** `anthropic-version: 2023-06-01` |
| Extra headers | — | — | — (old `anthropic-beta` optional) |
| Form fields | `file`, `purpose` | `file`, `purpose=user_data` | `file` |
| Expiry | not part of the documented Files API | `expires_after[anchor]=created_at` + `expires_after[seconds]` (3600–2592000) | `expires_in_seconds` (3600–7776000) |
| Response id | `id` | `id` | `id` |
| File types | broad document list + images; see §2.1 | JPEG/PNG/GIF/WebP only; see §2.1 | PDF, `text/plain`, images, datasets via code execution; see §2.1 |
| Limits | 50 MB per file input | 64 MiB; upload within 10 minutes | 500 MB per file; 1 TB per org |
| Model-edge block | Responses `input_file` / `input_image`; Chat `file` (PDF only) | `file` block with `file_id` | `document.source.file` / `image.source.file` |

DeepSeek therefore has **two** official Files API variants:

- **Default / OpenAI-compatible**: `POST https://api.deepseek.com/files`,
  `Authorization: Bearer`, `purpose=user_data`, OpenAI-shaped response (`bytes`,
  Unix `created_at`).
- **Anthropic-compatible**: `POST https://api.deepseek.com/anthropic/v1/files`,
  `x-api-key` plus `anthropic-beta: files-api-2025-04-14`, Anthropic-shaped
  response (`type`, `size_bytes`, RFC 3339 `created_at`).

Both return a top-level `id`. They must not be conflated in config: use
provider ID/profile `deepseek` for the OpenAI-compatible endpoint, and
provider ID `deepseek-anthropic` with profile `deepseek-anthropic-files` for
the Anthropic-compatible endpoint.

### 2.1 Supported file types by provider

“Documents” below means files carried by the provider's document/file input
blocks, not arbitrary files stored by a Files API.

| Provider / variant | Images | Documents / other files | Not accepted |
|---|---|---|---|
| **OpenAI** (Files API + Responses) | `image/jpeg`, `image/png`, `image/gif`, `image/webp` (via `input_image`) | PDF; text/code (`.txt`, `.md`, `.json`, `.html`, `.xml`, and code); rich documents (`.doc`, `.docx`, `.rtf`, `.odt`); presentations (`.ppt`, `.pptx`); spreadsheets (`.csv`, `.tsv`, `.xls`, `.xlsx`) via `input_file` | audio/video as file inputs; Chat Completions accepts **PDF only** for `file_id`/`file_data` |
| **DeepSeek — default (OpenAI-compatible)** | `image/jpeg`, `image/png`, `image/gif`, `image/webp` | none | PDF, `text/plain`, Office documents, audio, video |
| **DeepSeek — Anthropic-compatible** | `image/jpeg`, `image/png`, `image/gif`, `image/webp` | none | PDF, `text/plain`, Office documents, audio, video |
| **Anthropic** | `image/jpeg`, `image/png`, `image/gif`, `image/webp` | PDF (`application/pdf`), `text/plain`; datasets/other formats via `container_upload` for the code-execution tool | Office documents are not accepted directly (convert to PDF/text); no audio/video message blocks |

Important distinctions:

- OpenAI **Responses** supports the broad document list above; OpenAI **Chat
  Completions** supports only PDF for model file inputs.
- DeepSeek's two variants both share the image-only Files API restriction. The
  Anthropic-compatible variant changes the API shape and headers, not the
  accepted file types.
- Anthropic's `container_upload` is for the code-execution tool, not a generic
  model message block.

### 2.2 SDK coverage today (implementation gap)

Vendor support is broader than current SDK lowering:

| Provider | Supported by vendor | Supported by SDK today | Gap |
|---|---|---|---|
| OpenAI Responses | images + PDF + text/Office/sheets | images + PDF | non-PDF documents are rejected by the protocol MIME set |
| OpenAI Chat | PDF only | no `file_id` at all | `openai-chat` profile has no file block |
| DeepSeek (both variants) | images only | images | none, but the Anthropic-compatible variant needs provider headers |
| Anthropic | images + PDF + text/plain (+ datasets via code execution) | images + PDF | `text/plain` and `container_upload` are not lowered |

This gap is intentional to keep the protocol table honest: a provider may accept
a type, but the SDK must not advertise it until lowering and tests exist.

## 3. Design principles

1. **Separate the three axes.**
   - **Protocol** = wire encoding (block shape, source, lowering).
   - **Provider** = endpoint, auth, upload form, expiry style, MIME/limits.
   - **Model** = protocol/provider choice and model-specific capabilities.
2. **The host owns credentials and uploads.** The SDK must not learn provider
   secrets; it receives a capability reporter and an uploader factory.
3. **Declarative over provider switches.** Endpoint path, headers, fields and
   expiry can be expressed as data. Code adapters are a last resort.
4. **Fail fast on malformed config; degrade explicitly on unavailable sources.**
   - Malformed values (URL, header name, expiry value/range, MIME syntax,
     unknown enum) fail startup.
   - A missing/unreferenced `providerFileAPI`, an unavailable store, or a
     provider/model type mismatch disables that source and emits an actionable
     warning; it never falls back to a different provider/profile.
   - An explicit file-only policy with no fallback (for example
     `prefer: [providerFile]` and no other source) is a startup error.
5. **One place to look.** A provider capability table must be the canonical
   answer to “can this provider accept this file?”.
6. **Extensibility is a first-class constraint.** Adding a provider, protocol or
   model must be additive and predictable:
   - a declarative provider must be added by profile data + tests, not by
     editing the planner, uploader, transport or model request code;
   - a new wire shape adds a protocol block + lowering + tests, not a provider
     switch in the uploader;
   - a new model is a catalog entry, not code;
   - generic code contains no `switch providerID`; the only escape hatch is an
     explicitly documented adapter interface;
   - capability data has one owner per axis; MIME lists are never duplicated
     between protocol, provider and model;
   - an unconfigured provider disables `file` for that model and is reported
     with an actionable warning; it never falls back to another provider.
7. **Configuration must be self-evident.** An operator should not have to read
   Go code or infer behaviour from a working config:
   - one canonical profile matrix lists every supported `providerFileAPI`
     profile, provider ID, endpoint, auth headers, form fields, expiry style,
     MIME types, limits and model-edge block;
   - every provider variant has a copy-paste example in
     `cmd/agentd/agentd.example.yaml` (including `deepseek` vs
     `deepseek-anthropic-files`);
   - startup validation names the exact field path and lists the allowed
     providers/capabilities on error;
   - `agentd --check` and the startup log summarize the effective, redacted
     `providerFileAPIs` entries and their capabilities;
   - `GET /api/config` exposes the same redacted `providerFileAPIs` entries and
     the effective capability matrix;
   - documentation, examples, introspection and tests are generated from or
     checked against the same built-in provider table.
8. **Every configuration error must contain the fix.** “invalid config” is not an
   acceptable message. A startup error or `--check` warning must include:
   - the config path and, where available, the source file/line;
   - the server and model/provider involved;
   - the observed value and the expected value/range/known values;
   - a minimal copy-paste YAML fragment or a stable documentation anchor;
   - a remediation sentence (“add …”, “set providerID …”, “use …”).
   Secret values are never echoed; use `${ENV}` placeholders in examples.
9. **Protocol compatibility is not provider capability.** Names such as
   `openai-chat`, `openai-responses` or `anthropic-messages` describe the wire
   shape only. A proxy may implement the chat/response shape but not the Files
   API; it may have different auth, file types, limits or expiry. Therefore:
   - the media upload profile is selected by an **explicit
     `models[].providerFileAPI` reference**; it is never inferred from the
     protocol, provider ID, model name, or endpoint URL;
   - a file-capable model without an explicit `providerFileAPI` is a startup
     error (credentials may still use the existing `ProviderKey`/protocol
     fallback, which is a separate concern);
   - an `openai-*` or `anthropic-*` protocol match never implies an upload
     profile;
   - **capabilities are declared per `providerFileAPIs` profile and are never
     inherited** from a vendor name or protocol. A proxy may support any subset
     (for example: chat only, no Files API; images only, no PDF; no expiry;
     custom headers). The presence and contents of its profile are the source of
     truth; no reference/profile means `file` is disabled.
10. **Name by role, not by medium.** The target tree is
    `providerFileAPIs.<name>` (reusable provider Files API configs),
    `models[].providerFileAPI` (explicit model reference),
    `media.delivery` (choose the path), and
    `media.uploads.urlHost` (our URL host). `urlHost` says *where* the upload
    goes; `providerFileAPI` says *what the model receives* (`file_id`). The old
    `media`/`storage`/`mediaUpload` names are not accepted as aliases; the
    schema change is breaking. A name that requires reading this document to
    disambiguate is a defect.
11. **The LLM protocol and the File API protocol are independent.** A model uses
    `openai-chat`/`openai-responses`/`anthropic-messages` for inference; its
    `providerFileAPI` entry separately selects the Files API shape, auth and
    response envelope. A provider may use an LLM protocol that has nothing to do
    with its File API (for example, an OpenAI-compatible chat proxy with a custom
    multipart Files API). The two axes never constrain each other by name.

### 3.1 Extensibility invariants (acceptance criteria)

| Change | Should require | Must not require |
|---|---|---|
| New provider, existing wire shape | one `providerFileAPIs` entry + `models[].providerFileAPI` reference, matrix row, `agentd.example.yaml` example, validation error text, tests | planner/uploader/transport/request changes |
| New provider-specific upload quirk | profile field if declarative; otherwise one adapter + tests | edits to unrelated providers or the planner |
| New wire shape / block | protocol descriptor entry + lowering + tests | uploader changes |
| New model | catalog entry (`protocol`, `providerID`, `input`, optional `inputMIMEs`) | code changes |
| New file type for an existing provider | provider MIME list + protocol MIME list + lowering + tests | uploader changes |
| New expiry style | one `ExpiryStyle` value + uploader branch for that style + tests | provider-specific `switch` |

These are review gates: a PR that adds a provider by editing planner/uploader
logic instead of adding a profile is not following this design.

## 4. Proposed architecture

```
                    model catalog
        (protocol, providerID, input, mime overrides)
                          |
                          v
  protocol descriptor  +  providerFileAPI   +  model capability
   (wire MIME/source)     (upload API)          (model MIME/modality)
                          |
                          v
                 effective media capability
                          |
          +---------------+----------------+
          |                                |
   inbound planner                  MediaUploader(providerID)
   (inline/url/providerFile/spool)   (profile-driven HTTP upload)
```

### 4.1 `providerFileAPIs` profile

Each entry under the top-level `providerFileAPIs` map is a reusable provider
Files API configuration. Illustrative shape (names are not final):

```go
type ProviderFileAPI struct {
    // Kind selects the Files API request/response shape (a codec/adapter),
    // independently of the model's LLM protocol.
    Kind     FileAPIKind
    Upload   UploadProfile
    // ModelHeaders apply to model requests that reference a file_id
    // (e.g. "anthropic-beta: files-api-2025-04-14" for DeepSeek).
    ModelHeaders map[string]string
}

// FileAPIKind is the upload transport shape, not the LLM protocol.
// "multipart-json" covers OpenAI/DeepSeek/Anthropic; a proxy with a different
// envelope uses its own kind + adapter.
type FileAPIKind string

const FileAPIMultipartJSON FileAPIKind = "multipart-json"

type UploadProfile struct {
    URL       string
    Headers   map[string]string // env-expanded; never logged
    Fields    map[string]string // form fields, e.g. purpose=user_data
    IDField   string            // JSON path; default "id"
    Expiry    ExpiryProfile
    FileTypes FileTypeProfile
    Limits    UploadLimits
}

type ExpiryProfile struct {
    Style   ExpiryStyle // none | expires_after | expires_in_seconds
    Seconds int         // 0 = provider default/permanent
}

// FileTypeProfile is the canonical provider file-type declaration. MIME types
// are the source of truth; extension lists in the docs are display-only.
type FileTypeProfile struct {
    // AllowedMIMEs are the types the provider Files API accepts.
    // Model-level narrowing belongs in models[].inputMIMEs; the File API does
    // not vary by LLM protocol.
    AllowedMIMEs []string
}

type UploadLimits struct {
    MaxBytes int64
    Timeout  time.Duration
}
```

`FileAPIKind` is deliberately separate from the model's LLM protocol. The model
catalog's `protocol` (`openai-chat`, `openai-responses`, `anthropic-messages`)
governs inference requests; `ProviderFileAPI.Kind` governs upload requests.
They are chosen independently: a proxy may use `openai-chat` for inference and a
custom `multipart-json`/adapter for files. Adding a new File API shape means
adding a kind + codec/adapter + tests, not changing an LLM protocol.

The file-type allow-list is **mandatory provider data**, not something inferred
from the endpoint or the SDK:

| Profile | `FileTypes.AllowedMIMEs` (see §2.1 for extensions/examples) |
|---|---|
| `openai` | `application/pdf`, `text/plain`/text-code MIMEs, Office/document MIMEs, spreadsheet MIMEs, image MIMEs; Chat narrows to PDF |
| `deepseek` | `image/jpeg`, `image/png`, `image/gif`, `image/webp` |
| `deepseek-anthropic-files` | `image/jpeg`, `image/png`, `image/gif`, `image/webp` |
| `anthropic` | `application/pdf`, `text/plain`, `image/jpeg`, `image/png`, `image/gif`, `image/webp` |

MIME is the canonical identity (the provider may accept a file whose extension is
missing or wrong). The documentation may show `.pdf`/`.docx`/`.xlsx` for
readability, but enforcement always compares normalized MIME types.

The provider allow-list is one of the three owners from §3 (provider axis);
`ProtocolMediaSupport.MIMEs` owns the wire capability and model `inputMIMEs`
owns model narrowing. Never duplicate the same list into the uploader or the
model catalog.

Canonical `providerFileAPIs` templates live in `cmd/agentd/agentd.example.yaml`;
they are copied into config and are never selected implicitly. An operator may
**narrow** `FileTypes.AllowedMIMEs`, but must not add a type the provider does
not declare.

Profile names are per API family, not per company: e.g. `deepseek` and
`deepseek-anthropic-files` are distinct because their Files API contract differs.
A model selects one with an explicit `models[].providerFileAPI` field; this is
independent of its credential `providerID`.

### 4.2 Capability reporting to the SDK

The SDK currently receives only:

```go
MediaUploader func(providerID schema.ProviderID) schema.MediaUploader
```

Add an optional capability interface:

```go
type MediaCapabilities struct {
    MIMEs    []string
    MaxBytes int64
}

type MediaCapabilityReporter interface {
    MediaCapabilities() MediaCapabilities
}

type MediaUploader interface {
    Upload(ctx context.Context, media schema.MediaUpload) (schema.Reference, error)
}
```

- If an uploader implements `MediaCapabilityReporter`, the planner includes
  those MIMEs/limits in its source selection.
- If it does not, the uploader is treated as unconstrained (current behaviour).
- An empty `MediaCapabilities.MIMEs` from a custom reporter means unconstrained.
  agentd's `providerFileAPIs` implementations never report empty.
- Every `providerFileAPIs` profile MUST declare a non-empty MIME list. An empty
  list is a configuration error, not “unrestricted”.

`agentd` builds a per-server uploader factory from the model references:

1. resolve each model's explicit `models[].providerFileAPI`;
2. map `providerID -> ProviderFileAPI` for that server;
3. the SDK's `MediaUploader(providerID)` factory returns that profile's
   uploader (or nil when unmapped).

There is no cross-provider fallback and no legacy flat uploader. See §4.5.1 and
§4.5.4 for the exact reference and runtime mapping.

### 4.3 Model catalog and MIME control

Three checks, in order:

1. **Protocol**: `ProtocolMediaSupport.AllowsMIME` + `AllowsSourceFor`.
2. **Provider**: `ProviderFileAPI.Upload.FileTypes.AllowedMIMEs` (and limits).
3. **Model**: `input` modalities, plus an optional `inputMIMEs` allow-list.

Proposed model field (optional, empty = modality-level only):

```yaml
models:
  - id: doc-reader
    protocol: openai-responses
    providerID: openai
    providerFileAPI: openai
    input: [text, pdf]
    inputMIMEs: [application/pdf]
```

The effective capability is the intersection:

```
allowed = protocol.MIMEs ∩ provider.MIMEs ∩ model.inputMIMEs
source allowed = protocol.Sources ∋ source
                 && (source != url || mime ∉ protocol.NoURLSource)
```

An empty `model.inputMIMEs` means “no model-level narrowing” (the protocol and
provider still constrain); it does not mean “nothing”. MIME lists remain
lower-case and normalized. The referenced `providerFileAPI` is authoritative for
its Files API; the model entry only narrows it.

Recommended initial allow-lists:

| Profile (`providerFileAPI`) | Files API MIME allow-list |
|---|---|
| `openai` | PDF, text/code, Office, spreadsheets, presentations (full current OpenAI list) |
| `deepseek` | `image/jpeg`, `image/png`, `image/gif`, `image/webp` |
| `anthropic` | `application/pdf`, `text/plain`, `image/jpeg`, `image/png`, `image/gif`, `image/webp` |
| `deepseek-anthropic-files` | image MIMEs only (its Files API is image-only) |

OpenAI Chat Completions remains PDF-only when file source is enabled; Responses
keeps the broader list.

### 4.4 Protocol-layer changes

The protocol layer keeps the wire concerns and gains a small amount of MIME
granularity:

- `OpenAIFileBlock` stays DeepSeek's image `file` block (`file_id` at the top
  level); `OpenAIFileDocument` is OpenAI Chat's PDF document block
  (`file.file_id`). Both are file-source block kinds with separate MIME sets.
- OpenAI Responses accepts the declared PDF/text/Office document MIME types;
  `input_file.file_id` works for every admitted document MIME, and a url source
  is rejected for documents.
- Anthropic accepts `application/pdf` and `text/plain` in a `document` block
  (inline bytes, url or `file_id`). `container_upload` is a code-execution
  artifact path and is deliberately not emitted for inbound files.
- DeepSeek stays image-only. A separate `deepseek-anthropic` model entry reuses
  `anthropic-messages`, but its `providerFileAPI` profile restricts MIMEs to
  images and adds `anthropic-beta` to model requests when a file reference is
  used.
- OpenAI Chat enables PDF `file_id` through `OpenAIFileDocument` on the existing
  `openai-chat` protocol; models without a document form are unaffected.

### 4.5 No legacy `mediaUpload` compatibility

The old flat `mediaUpload` block is **not accepted**; there is no loader
translation, profile synthesis, or deprecated alias. The schema change is
breaking: a config containing `mediaUpload` is a startup error with a pointer to
the target shape.

Required shape:

```yaml
providerFileAPIs:
  deepseek:
    kind: multipart-json
    # url/auth/fields/expiry/mime/limits ...

models:
  - id: deepseek-flash
    protocol: deepseek-chat
    providerID: deepseek
    providerFileAPI: deepseek
```

- A profile must declare `kind`, `url`, auth, expiry style, non-empty MIME list
  and limits.
- A file-capable model must declare both `providerID` and `providerFileAPI`.
- `schemaVersion` must be bumped; old config files require a manual rewrite.

### 4.5.1 Provider resolution (exact only, no implicit default)

There is **no implicit default**. The upload profile is selected by an explicit
`models[].providerFileAPI` reference:

```yaml
providerFileAPIs:
  deepseek-files:
    # kind/url/auth/fields/expiry/mime/limits

models:
  - id: deepseek-flash
    protocol: deepseek-chat
    providerID: deepseek
    providerFileAPI: deepseek-files
```

The reference is an operator-chosen name; it is never derived from the model's
`protocol`, `providerID`, model name or base URL. A file-capable model **must
also set an explicit `models[].providerID`**: the SDK's uploader factory is
called with `ModelInfo.ProviderID`, which falls back to the protocol when
`providerID` is empty, so the media profile could otherwise be looked up under
the wrong key. The credential fallback rule still applies to models that do not
use `file`; it is not used for media-profile lookup.

For each model that can use the `file` source:

1. **Explicit keys**: the model must set `models[].providerFileAPI` and an
   explicit `models[].providerID`.
2. **Exact reference**: `providerFileAPIs[<providerFileAPI>]` is used.
3. **No reference / no match**: a file-capable model without a reference, an
   explicit `providerID`, or a matching profile is a **startup error**. A model
   that does not declare a file form is unaffected. A valid profile whose MIME
   set does not intersect the protocol/model only disables `file` for that model
   with a warning; the planner falls back according to `media.delivery.prefer`
   or emits the existing omission event.
4. **Never cross-serve**: an Anthropic profile is never used for a DeepSeek
   model or vice versa. A typo or missing reference fails startup; it does not
   silently send data to another endpoint.

This is deliberate: users often do not know whether an OpenAI-compatible proxy
implements a Files API, and a protocol/URL-based guess would be dangerous. File
upload is opt-in via an explicit `providerFileAPI` reference.

Examples:

| Model config | `providerFileAPIs` | Result |
|---|---|---|
| `providerFileAPI: deepseek-files` | `deepseek-files` | exact match |
| `providerFileAPI: deepseek-files` | only `anthropic-files` | startup error: unknown profile; the Anthropic profile is not used |
| file-capable model with no `providerFileAPI` | any | startup error |
| `providerFileAPI: deepseek-files` but no `providerID` | `deepseek-files` | startup error (providerID is required for media lookup) |
| model A `providerFileAPI: deepseek-files`, model B `providerFileAPI: anthropic-files` | both | each uses its own profile (distinct `providerID`s) |
| model A `providerID: deepseek`, `providerFileAPI: a`; model B `providerID: deepseek`, `providerFileAPI: b` | `a`, `b` | startup error: one `providerID` cannot map to two profiles |
| protocol `openai-chat`, `providerFileAPI: my-proxy-files` | `my-proxy-files` | use the proxy's own profile; protocol does not imply OpenAI Files API |

Type compatibility is checked only when an exact profile exists:

- protocol must allow a `file` source for the model's declared media forms;
- provider `FileTypes.AllowedMIMEs` must intersect the model's declared
  `inputMIMEs` (or modalities when no MIME list is set);
- if the model declares `image` only and the referenced `providerFileAPI` allows
  images plus PDF, that is valid (the model narrows the provider);
- if the provider only allows PDF while the model declares images, `file` is
  disabled with a warning; if `media.delivery.prefer` contains only `providerFile` (no
  inline/url/spool fallback), that is a startup error.

### 4.5.2 OpenAI/Anthropic-compatible proxies

Many gateways claim `openai-chat`, `openai-responses` or `anthropic-messages`
compatibility. That claim covers the **model request wire shape**, not the Files
API. A proxy may:

- implement chat/responses but have no Files API at all;
- implement a Files API at a different path, with different auth headers, form
  fields, response envelope or expiry;
- support a different subset of MIME types or size limits;
- route to different upstreams per model.

Therefore:

1. Give each proxy its own operator-chosen `providerID` (for example
   `my-proxy`) and its own `providerFileAPIs` entry (for example
   `my-proxy-files`), even when it claims OpenAI compatibility. Never reuse
   `openai` unless the proxy's Files API contract is exactly OpenAI's.
2. The model's `modelBaseURL` and `providerFileAPIs.<name>.url` are independent;
   a model can use one proxy for chat and another endpoint for files, or no file
   endpoint at all.
3. Built-in `providerFileAPIs` entries are templates and conveniences, not
   inference rules. No code selects them from a protocol name, `providerID`, or
   URL.
4. If the proxy does not implement a Files API, leave `models[].providerFileAPI`
   unset; `file` is disabled for its models and `prefer` falls back to
   inline/url/spool.

Capability is declared, never inherited:

| Concern | Source of truth | Not inferred from |
|---|---|---|
| Wire shape (chat/responses/messages) | protocol descriptor | provider name |
| Files API exists | a referenced `providerFileAPIs` entry | protocol name / base URL |
| Auth headers/form fields | `providerFileAPIs.<name>.auth` | vendor defaults |
| Expiry style/range | `providerFileAPIs.<name>.expiry` | URL |
| Accepted file types | provider `FileTypes.AllowedMIMEs` | model name / protocol |
| Size/timeout | provider `Limits` | proxy marketing claim |
| Model accepts file input | model `input`/`media.forms`/`inputMIMEs` | provider support |

Partial support is normal. For example, a proxy may accept only PNG/JPEG, use a
custom header, and have no expiry:

```yaml
providerFileAPIs:
  my-proxy-files:
    kind: multipart-json
    url: https://proxy.example/v1/files
    auth:
      headers:
        X-Proxy-Key: "${PROXY_API_KEY}"
    fields: {}                     # no OpenAI purpose field
    expiry:
      style: none
    mime:
      allowed: [image/png, image/jpeg]
    limits:
      maxBytes: 20971520
      timeout: 2m

models:
  - id: proxy-vision
    model: some-vision-model
    protocol: openai-chat
    providerID: my-proxy
    providerFileAPI: my-proxy-files
    modelBaseURL: https://proxy.example/v1
    input: [text, image]
```

There is no startup probing of third-party endpoints. Treat the declared
profile as authoritative; if the proxy changes behaviour, update the profile (or
use a new `providerFileAPIs` entry) rather than guessing from its protocol
name.

Example for a proxy that does implement an OpenAI-shaped Files API:

```yaml
models:
  - id: proxy-vision
    model: some-vision-model
    protocol: openai-chat
    providerID: my-proxy              # credential key; not inferred from protocol
    providerFileAPI: my-proxy-files   # explicit media profile
    modelBaseURL: https://proxy.example/v1
    input: [text, image]

providerFileAPIs:
  my-proxy-files:
    kind: multipart-json
    url: https://proxy.example/v1/files
    auth:
      headers:
        Authorization: "Bearer ${PROXY_API_KEY}"
    fields:
      purpose: user_data
    idField: id
    expiry:
      style: expires_after          # only if the proxy actually supports it
      seconds: 86400
    mime:
      allowed: [image/jpeg, image/png, image/gif, image/webp]
    limits:
      maxBytes: 67108864
      timeout: 10m
```

If the proxy changes its payload shape, use a separate `providerFileAPIs` entry
and `FileAPIKind`/adapter rather than changing the built-in OpenAI profile. If
its LLM wire also differs, that is a separate protocol change.

### 4.5.3 Canonical `providerFileAPIs` configuration

The following are the built-in reference configurations. They are data, not
special code paths; an operator can override URL/auth/fields/expiry/MIME/limits,
or add a proxy entry with a new name.

```yaml
providerFileAPIs:
  openai:
    kind: multipart-json
    url: https://api.openai.com/v1/files
    auth:
      headers:
        Authorization: "Bearer ${OPENAI_API_KEY}"
    fields:
      purpose: user_data
    idField: id
    expiry:
      style: none
    mime:
      allowed:
        - application/pdf
        - text/plain
        - text/markdown
        - text/csv
        - application/json
        - application/msword
        - application/vnd.openxmlformats-officedocument.wordprocessingml.document
        - application/vnd.ms-powerpoint
        - application/vnd.openxmlformats-officedocument.presentationml.presentation
        - application/vnd.ms-excel
        - application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
        - image/jpeg
        - image/png
        - image/gif
        - image/webp
    limits:
      maxBytes: 52428800
      timeout: 2m

  # DeepSeek default: OpenAI-compatible POST /files.
  deepseek:
    kind: multipart-json
    url: https://api.deepseek.com/files
    auth:
      headers:
        Authorization: "Bearer ${DEEPSEEK_API_KEY}"
    fields:
      purpose: user_data
    idField: id
    expiry:
      style: expires_after
      seconds: 86400
    mime:
      allowed: [image/jpeg, image/png, image/gif, image/webp]
    limits:
      maxBytes: 67108864
      timeout: 10m

  # DeepSeek optional Anthropic-compatible endpoint.
  deepseek-anthropic-files:
    kind: multipart-json
    url: https://api.deepseek.com/anthropic/v1/files
    auth:
      headers:
        x-api-key: "${DEEPSEEK_API_KEY}"
        anthropic-beta: "files-api-2025-04-14"
    modelHeaders:
      anthropic-beta: "files-api-2025-04-14"
    idField: id
    expiry:
      style: expires_in_seconds
      seconds: 86400
    mime:
      allowed: [image/jpeg, image/png, image/gif, image/webp]
    limits:
      maxBytes: 67108864
      timeout: 10m

  anthropic:
    kind: multipart-json
    url: https://api.anthropic.com/v1/files
    auth:
      headers:
        x-api-key: "${ANTHROPIC_API_KEY}"
        anthropic-version: "2023-06-01"
    idField: id
    expiry:
      style: expires_in_seconds
      seconds: 86400
    mime:
      allowed: [application/pdf, text/plain, image/jpeg, image/png, image/gif, image/webp]
    limits:
      maxBytes: 524288000
      timeout: 10m
```

Model wiring (the reference is explicit; the profile name is not derived from
`providerID` or `protocol`):

```yaml
models:
  - id: gpt-vision
    model: gpt-4.1
    protocol: openai-responses
    providerID: openai
    providerFileAPI: openai
    input: [text, image, pdf]

  - id: deepseek-flash
    model: deepseek-flash
    protocol: deepseek-chat
    providerID: deepseek
    providerFileAPI: deepseek
    input: [text, image]

  # Optional: same company, Anthropic-compatible Files API and Messages wire.
  - id: deepseek-anthropic
    model: deepseek-flash
    protocol: anthropic-messages
    providerID: deepseek-anthropic
    modelBaseURL: https://api.deepseek.com/anthropic
    providerFileAPI: deepseek-anthropic-files
    input: [text, image]

  - id: claude-docs
    model: claude-opus-5-5
    protocol: anthropic-messages
    providerID: anthropic
    providerFileAPI: anthropic
    input: [text, image, pdf]
```

Server policy (shared across the models it exposes):

```yaml
servers:
  - name: public
    models: [gpt-vision, deepseek-flash, deepseek-anthropic, claude-docs]
    media:
      delivery:
        prefer: [providerFile, url, inline]  # provider Files API first
        allowUploads: true
        inlineLimitBytes: 262144
        maxBytes: 52428800
      uploads:
        urlHost:
          backend: s3
          # s3: {bucket: ..., region: ...}  # see agentd.example.yaml
```

Notes:

- `providerFileAPIs.openai` is the exact OpenAI contract. A proxy that claims
  OpenAI compatibility but has a different Files API must use a different entry
  (for example `my-proxy-files`) and a different `providerID`.
- `deepseek` is the default OpenAI-compatible endpoint. Use
  `deepseek-anthropic-files` only for `/anthropic/v1/files`; its upload headers
  and `modelHeaders` are both required. `modelHeaders` are merged into the
  model's request headers (the model's own `headers` win on a duplicate key).
- `expiry.style` is explicit. OpenAI has no documented expiry; DeepSeek uses
  `expires_after`; Anthropic uses `expires_in_seconds`.
- `mime.allowed` is mandatory. Adding a type here does not make the SDK emit it;
  the protocol descriptor and model `input`/`inputMIMEs` must also allow it.
- Empty `providerFileAPI` reference means file upload is disabled, not
  “inherited from providerID”.
- Within a server, all models with the same `providerID` must reference the same
  `providerFileAPI`; otherwise startup fails (see §4.5.4). Different Files API
  contracts need different `providerID`s.

### 4.5.4 Runtime selection and providerID mapping

The SDK’s current `MediaUploader` factory is keyed by `ProviderID` only:
`func(providerID schema.ProviderID) MediaUploader`. It does not receive the model
ID or the `providerFileAPI` name at request time. `agentd` therefore builds a
per-server map at startup:

```
models[].providerID + models[].providerFileAPI
        │
        ▼
server profile map: providerID -> ProviderFileAPI (exact)
        │
        ▼
MediaUploader(providerID) -> profile uploader (or nil)
```

Rules:

1. Require both `models[].providerID` and `models[].providerFileAPI` on every
   file-capable model. `providerID` is the runtime map key; the SDK's
   `ProviderKey` protocol fallback is not used for media-profile lookup.
2. Resolve each model's `providerFileAPI` to its profile and build
   `providerID -> profile` for that server.
3. If the same `providerID` is used with two **different** profiles, startup
   fails:
   `server X: providerID "deepseek" is used with providerFileAPI "a" and "b";
   assign a distinct providerID per Files API contract`.
4. Models with the same `providerID` and the same profile share one uploader.
5. A file-capable model with a missing `providerID`, missing reference, or
   missing profile is a startup error; non-file models are absent from the map.
6. `providerFileAPI` remains the config/validation/visibility key; the runtime
   key is the SDK’s `providerID`. If the SDK later adds a model/profile
   identifier, this bridge can be removed without changing user config.

This rule is what keeps the named `providerFileAPIs` registry unambiguous with
the current SDK API: reuse a profile across models that share a `providerID`, or
give each distinct Files API contract its own `providerID`.

### 4.6 Uploader implementation

Split into three small pieces:

1. **Profile validation** (config layer): URL, headers, expiry ranges, MIME
   list, limits.
2. **Generic HTTP uploader** (media layer): profile-driven, no provider
   `switch`; builds multipart with `media.Mime`, headers, fields and expiry
   style; enforces `maxBytes`; reports capabilities; parses `id` (with optional
   dotted path).
3. **Optional adapter** for a provider whose quirk is not declarable (for
   example a non-standard response envelope). The adapter implements the same
   `MediaUploader` interface and is selected by the profile's `FileAPIKind`, not
   by a provider `switch`.

The uploader must not own the model-request headers. `ProviderFileAPI.ModelHeaders`
are applied by the model route/catalog when a file reference is emitted (e.g.
DeepSeek Anthropic `anthropic-beta`).

### 4.7 Capability matrix and discoverability

Keep one canonical table (the example profiles plus the protocol registry) and a
parity test that keeps it in sync:

| Profile (`providerFileAPI`) | providerID | Upload auth | Expiry | MIME | Model-edge file source | Protocol(s) |
|---|---|---|---|---|---|---|
| `openai` | `openai` | Bearer | none | PDF, text, Office, sheets | `input_file` / `input_image`; Chat PDF via `file.file_id` | `openai-responses`, `openai-chat` |
| `deepseek` | `deepseek` | Bearer | `expires_after` | images | `file` block | `deepseek-chat` |
| `deepseek-anthropic-files` | `deepseek-anthropic` | `x-api-key` + beta | `expires_in_seconds` | images | Anthropic `image` file source | `anthropic-messages` |
| `anthropic` | `anthropic` | `x-api-key` + version | `expires_in_seconds` | PDF, text, images | Anthropic `document`/`image` file source | `anthropic-messages` |

The table is enforced by a table-driven test, so documentation and code cannot
drift. `agentd` startup validates each model that can use `file`: protocol
supports the MIME/source, the referenced `providerFileAPI` admits the MIME, and
the model declares it.

### 4.8 Configuration discoverability

The profile matrix is not only a design artefact; it is exposed through every
surface an operator uses:

1. **Documentation**: this document's matrix and the per-profile YAML examples
   are the canonical reference. Each profile row points to the complete example
   in §4.5.3, including the distinction between `deepseek`
   (OpenAI-compatible) and `deepseek-anthropic-files` with
   `providerID: deepseek-anthropic` (Anthropic-compatible).
2. **Example config**: `cmd/agentd/agentd.example.yaml` contains commented,
   copy-paste blocks for every providerFileAPI profile. A profile is not
   considered supported until its example exists.
3. **`agentd --check` and startup log**: print a one-line summary per configured
   profile, for example:
   `media upload: openai (bearer, pdf/text/office/images, 50MB), deepseek
   (bearer, images, 64MB), anthropic (x-api-key+version, pdf/text/images, 500MB)`.
   Header values and credentials are never printed; only header names.
4. **Validation errors and warnings**: a file-capable model whose
   `providerFileAPI` is unset or unknown is a startup error:
   `model "M": unknown providerFileAPI "P"; known profiles: anthropic,
   deepseek, deepseek-anthropic-files, openai`. A malformed profile is a startup
   error and names its full path and expected shape
   (`providerFileAPIs.anthropic.auth.headers.anthropic-version is required;
   example: "2023-06-01"`). MIME/protocol incompatibilities are warnings and
   disable `file` for that model.
5. **`GET /api/config`**: the redacted media view is extended with the
   `providerFileAPIs` profiles showing, per profile: URL (sanitized), header
   **names**, field names (values redacted), expiry style/seconds, MIME list and
   limits, plus each model's `providerFileAPI` reference. It also reports the
   effective capability matrix so a host can answer “what can this instance
   upload?” without reading code.
6. **Parity tests**: a table-driven test asserts that the canonical profiles in
   the example YAML match the documented auth/expiry/MIME/limits; the
   introspection view is covered by its own config tests. Adding a canonical
   provider without updating the table fails tests.

The goal is that an operator can answer three questions from configuration and
CLI output alone:

- Which providers are supported?
- Which provider does this model use, and is it configured?
- What auth, fields, expiry, file types and limits does that provider need?

### 4.9 MediaStore, delivery policy and extensibility

The three blocks have different provider relationships and must not be modelled
as three variants of the same thing:

| Block | Provider association | Extension model | Capability model today |
|---|---|---|---|
| `media.delivery` | none (operator intent) | fixed source enum in SDK | none; only policy validation |
| `media.uploads.urlHost` | none (provider-independent) | backend switch/factory | none; only `Put` |
| `providerFileAPIs` + `models[].providerFileAPI` | explicit model reference | profile data (this design) | profile + `MediaCapabilities` |

#### 4.9.1 MediaStore capabilities

`MediaStore` must report what its URLs mean, because “url” is not enough:

```go
type StoreCapabilities struct {
    // Fetchable is true when the store returns an http(s) URL a provider can
    // fetch. A local store without serveBaseURL returns file:// and must report
    // false.
    Fetchable bool
    // MIMEs/MaxBytes are optional store limits; empty/0 = unrestricted.
    MIMEs    []string
    MaxBytes int64
    // TTL is the URL lifetime for presigned stores (0 = no expiry).
    TTL time.Duration
}

type MediaStoreCapabilityReporter interface {
    StoreCapabilities() StoreCapabilities
}
```

`agentd`'s store wrappers implement this optional interface; the planner
type-asserts `schema.MediaStore` to it. A store without the interface is treated
as unconstrained.

The planner selects `url` only when:

1. the provider/protocol accepts the `url` source for that MIME;
2. the configured store reports `Fetchable`;
3. the MIME/size is within the store limits.

A local store with `serveBaseURL` empty is valid for same-host file access, but
must not be treated as a provider-fetchable URL. This closes the current gap
where `file://` is produced and only fails later at the provider.

#### 4.9.2 Delivery-policy availability

`media.delivery.prefer` is an intent, not a promise. At startup/`--check`:

- `inline` is always available (subject to size);
- `file` requires an explicit `models[].providerID`, a
  `models[].providerFileAPI` reference, a matching `providerFileAPIs[<name>]`,
  and protocol/model `file` support;
- `url` requires a configured store with `Fetchable: true`;
- `spool` is a fallback, not a `prefer` source;
- `allowUploads` gates `url`/`file` hosting.

If a preferred source is unavailable, `--check`/startup reports the effective
order and the reason. If nothing is available, the existing omission/fallback
path applies with a clear note, except for a file-only policy with no fallback,
which fails startup (principle 4). Duplicate `prefer` entries are rejected.

#### 4.9.3 Extension model

- **Delivery sources**: the SDK planner enum (`inline/url/file`) is
  intentionally small; config spells the provider-bound source as
  `providerFile` and maps it to `file` at the boundary. Adding a source is an
  SDK change, not a config extension; it must come with planner + protocol +
  tests.
- **Store backends**: provider-independent. Adding one is a code change
  (factory + capabilities + config/validation + tests); a registry can replace
  the `switch`, but it is not provider-keyed. The important requirement is the
  capability report, not the registry.
- **Provider files**: profile-name-keyed, declarative `providerFileAPIs` entries
  as described in §4.1, referenced by `models[].providerFileAPI`. This is the
  only one of the three that needs a per-model reference.

This resolves the asymmetry: the URL-host backend (`storage` /
`media.uploads.urlHost`) should **not** adopt provider profiles, but it should
adopt a capability descriptor. The delivery policy (`media` /
`media.delivery`) should **not** become provider-aware, but it should validate
source availability against the store/uploader capability.

## 5. Extension recipe

To add a provider:

1. Add a `providerFileAPIs` entry with an explicit `kind` and reference
   it from the model's `models[].providerFileAPI`.
2. If the **File API** shape differs from `multipart-json`, add a
   `FileAPIKind` + codec/adapter + tests. Do not change the LLM protocol.
3. If the **LLM wire** lacks a `file` source/block for a modality, add a
   protocol block + lowering + tests. Do not change the uploader.
4. Add MIME lists to the `providerFileAPIs` entry and the protocol descriptor as
   appropriate.
5. Add the provider to the capability matrix **and** a copy-paste example in
   `agentd.example.yaml`.
6. Add the unknown-profile/missing-field validation messages and the
   `/api/config` redacted view.
7. If the provider needs model-request headers for file refs, set
   `ModelHeaders`.
8. Add an integration test with a fake Files API server for upload response,
   auth headers, fields and expiry, plus the docs/example/matrix parity test.

No edits to the planner, uploader or generic HTTP code should be needed for a
purely declarative provider. The File API shape and the LLM protocol are
independent: changing one must not require changing the other.

### 5.1 Review checklist for an extension

- [ ] The provider is added as profile data, not as a `switch` in generic code.
- [ ] The profile declares its `FileAPIKind`; a custom File API shape is a
      codec/adapter, not an LLM-protocol change.
- [ ] A new LLM wire shape is isolated in the protocol registry/lowering and does
      not require uploader changes.
- [ ] `FileTypes.AllowedMIMEs` is populated for the provider and matches §2.1.
- [ ] Every MIME is declared in exactly one owner layer and intersected by the
      planner.
- [ ] Missing profiles/unknown `providerFileAPI` references on a file-capable
      model are startup errors; capability mismatches warn and disable `file`;
      neither ever uses another profile.
- [ ] Each `providerID` maps to exactly one `providerFileAPI` per server; two
      profiles for one `providerID` are a startup error.
- [ ] The capability matrix, `agentd.example.yaml` example and their parity
      test are updated.
- [ ] `agentd --check` / startup output and the `/api/config` redacted view
      expose the new provider without secrets.
- [ ] Unknown-provider and missing-field errors are actionable and name the
      provider/field path, expected value/range, remediation and doc anchor.
- [ ] Error-contract tests assert each failure includes model/provider, full
      config path and a copy-paste fix or doc anchor.
- [ ] At least one fake-server upload test covers auth, fields and expiry.
- [ ] No unrelated provider behaviour changes.

## 6. Validation and error handling

Startup (config load):

- URL is absolute `http(s)`.
- Header names are valid; header values are non-empty after env expansion.
- Expiry style/value is valid: `expires_after` and `expires_in_seconds` require
  `seconds > 0`; range checks per `providerFileAPI` profile.
- MIME lists are non-empty for every `providerFileAPIs` profile; every entry is
  lower-case with a slash.
- `limits.maxBytes > 0`, `timeout > 0`.
- Every model eligible for `file` either references an exact
  `models[].providerFileAPI` entry or has the file source disabled; there is no
  default/cross-provider fallback.
- File-capable models must set `models[].providerFileAPI` **and** an explicit
  `models[].providerID`; the profile is never derived from `providerID`,
  `protocol`, model name or URL. The SDK's protocol fallback for credentials is
  not used as the media map key.
- Within one server, each `providerID` maps to exactly one `providerFileAPI`;
  using the same `providerID` with two different profiles is a startup error
  because the SDK factory is keyed by `providerID` (see §4.5.4).
- A providerFileAPI/profile name is opaque and operator-chosen; a proxy must use
  its own entry (for example `my-proxy-files`), and an OpenAI/Anthropic protocol
  name never selects a built-in profile by itself.
- A missing/unknown `providerFileAPI` reference, or a profile/model type
  mismatch, disables `file` for that model and is reported by
  `--check`/startup; it never uploads through another profile.
- If `media.delivery.prefer` contains only `providerFile` and no fallback source is
  available for that model, startup fails instead of running with no delivery
  path.
- Provider `FileTypes.AllowedMIMEs` must intersect the model's declared MIME or
  modality set; otherwise file is disabled with the offending MIME/profile
  names.
- Model `inputMIMEs`, if set, is a subset of its provider file profile (warning
  or error; start with warning, then tighten).
- The legacy flat `mediaUpload` form is rejected at startup; there is no
  compatibility translation. Configs must use `providerFileAPIs` + model
  references.
- Models that do not declare a `file` form are not required to have a
  `providerFileAPI`; unused `providerFileAPIs` entries are allowed (and listed
  by `--check`).

Runtime:

- `maxBytes` is checked before multipart construction.
- Provider upload errors include profile name, provider ID, URL, status and a
  bounded body.
- A planner mismatch (MIME not allowed) falls back according to `prefer`
  (inline/spool) or emits the existing omission event; it never attempts an
  upload the provider cannot accept.
- Never log auth header values.

### 6.1 Error message contract

Every provider/media configuration failure must be a small tutorial, not a
diagnostic code. The same message builder is used by `agentd --check`, startup
and `/api/config` validation. Examples:

Unknown profile typo:

```text
providerFileAPIs.deepsek: unknown profile "deepsek";
known profiles: anthropic, deepseek, deepseek-anthropic-files, openai
did you mean "deepseek"?
see docs/DESIGN-MEDIA-PROVIDERS.md#453-canonical-providerfileapis-configuration
```

Missing profile for a model that can use `file`:

```text
server "a": model "deepseek-flash" declares file input but has no
providerID/providerFileAPI; file upload is disabled.
Add:
providerFileAPIs:
  deepseek:
    kind: multipart-json
    url: https://api.deepseek.com/files
    auth: {headers: {Authorization: "Bearer ${DEEPSEEK_API_KEY}"}}
    fields: {purpose: user_data}
    expiry: {style: expires_after, seconds: 86400}
    mime: {allowed: [image/jpeg, image/png, image/gif, image/webp]}
models:
  - id: deepseek-flash
    providerID: deepseek
    providerFileAPI: deepseek
Or remove "providerFile" from media.delivery.prefer to silence this warning.
```

Missing required header:

```text
providerFileAPIs.anthropic.auth.headers.anthropic-version: required;
expected "2023-06-01". Add:
  headers:
    x-api-key: "${ANTHROPIC_API_KEY}"
    anthropic-version: "2023-06-01"
see docs/DESIGN-MEDIA-PROVIDERS.md#453-canonical-providerfileapis-configuration
```

Wrong file type for the profile/model:

```text
server "a": model "vision-model" declares image input, but
providerFileAPIs.deepseek.mime.allowed is [application/pdf];
file upload is disabled.
Add image MIMEs: [image/jpeg, image/png, image/gif, image/webp],
or remove "providerFile" from media.delivery.prefer.
```

Invalid expiry range:

```text
providerFileAPIs.deepseek.expiry.seconds=10 is outside [3600, 2592000];
use e.g. 86400, or set expiry.style: none for a permanent file.
```

Removed legacy flat config:

```text
mediaUpload is no longer supported; use providerFileAPIs.<name> plus
models[].providerFileAPI. This schema change is breaking.
see docs/DESIGN-MEDIA-PROVIDERS.md#45-no-legacy-mediaupload-compatibility
```

Disabled file source (warning, not fatal):

```text
media upload disabled for model "m": models[].providerFileAPI is unset or
providerFileAPIs.<name> is missing.
Add the profile/reference or set media.delivery.prefer without "providerFile".
```

Every message should be testable: the error-contract tests assert the presence
of the model/provider/profile, the full config path, at least one expected value
or range, and a remediation hint.

## 7. Alternatives considered

| Alternative | Verdict |
|---|---|
| Keep the current generic uploader and document quirks | Rejected: Anthropic auth/expiry and DeepSeek-Anthropic cannot be configured; support matrix remains implicit |
| Hardcode one Go uploader per provider | Works, but endpoint/credentials/limits become code changes and operators cannot override; still needs a capability reporter |
| Move provider uploads into the SDK | Rejected: SDK has no provider/credential/storage ownership |
| Full plugin system | Overkill for three providers; the declarative profile plus optional adapter covers it |
| Per-server uploader only | Rejected: a server may expose several providers; model-to-profile references and a per-server providerID map are required |

## 8. Migration plan

### Phase P1 — providerFileAPIs and correct auth

- Add `providerFileAPIs`, `models[].providerFileAPI`, `auth.headers`, expiry
  style, timeout/maxBytes. The old flat `mediaUpload` decoder is removed.
- Document canonical profiles for `openai`, `deepseek`, `anthropic`,
  `deepseek-anthropic-files` and ship them in `agentd.example.yaml`. There are
  no implicit built-in defaults: an operator must declare each profile.
- Add `MediaCapabilities` reporting and the per-server
  `providerID -> ProviderFileAPI` factory bridge (§4.5.4).
- Acceptance: OpenAI/DeepSeek uploads unchanged; Anthropic upload works with
  `x-api-key` + `anthropic-version`; DeepSeek-Anthropic works with beta header
  and a distinct `providerID`; conflicting profiles for one providerID fail
  startup.

### Phase P2 — MIME/type control (implemented)

- `mime.allowed`, optional model `inputMIMEs`, and the effective-capability
  intersection are enforced by the planner.
- `OpenAIFileDocument` covers OpenAI Chat PDF; OpenAI Responses covers the
  declared document types; Anthropic covers PDF and `text/plain`.
- Acceptance: DeepSeek rejects PDF before upload; Anthropic accepts PDF/text;
  OpenAI Responses supports the declared document types; unsupported types
  produce the existing omission path.

### Phase P3 — cleanup (implemented)

- The capability matrix is checked in and the canonical profiles in
  `agentd.example.yaml` are covered by a parity test.
- The generic uploader has no provider-specific branches.
- References to the old flat `mediaUpload` shape are now limited to rejection
  tests and migration notes.

## 9. Testing strategy

- **Config**: `providerFileAPIs` validation; duplicate keys; expiry ranges;
  MIME normalization; unknown profile reference.
- **Provider selection**: exact `models[].providerFileAPI` reference only; a
  file-capable model with a missing reference/profile or missing explicit
  `providerID` fails startup; a DeepSeek model with only an Anthropic profile never uses the
  Anthropic endpoint; models with different references resolve independently;
  protocol/providerID names never select a built-in profile; a proxy with
  `providerFileAPI: my-proxy-files` uses only that entry; the same `providerID`
  referencing two profiles is a startup error; image-only model vs PDF-only
  providerFileAPI disables file and names the mismatch.
- **Proxies**: an `openai-chat`/`openai-responses`/`anthropic-messages` proxy
  with no profile disables file; a partial proxy profile (custom header, images
  only, no expiry) is used exactly as declared; no vendor capability is
  inherited from the protocol.
- **File API / LLM decoupling**: `openai-chat` + custom `FileAPIKind`/multipart
  profile works; changing the File API codec does not change model-request
  lowering, and adding a model protocol does not change the uploader.
- **Uploader**: per profile, capture method/URL/headers/fields/expiry/content
  type/id; maxBytes rejection; timeout; response id path; error body bound.
- **Capabilities**: `MediaCapabilities` intersection with protocol/model;
  empty vs allow-all semantics.
- **Store capabilities**: local without `serveBaseURL` reports
  `Fetchable=false`; http/s3/cos report fetchable URLs; presign TTL and
  MIME/size limits gate `url` selection.
- **Delivery availability**: `prefer: [providerFile]` without a model
  `providerFileAPI` warns and falls back; `prefer: [url]` without a fetchable
  store warns; duplicate `prefer` entries are rejected; `allowUploads` gates
  hosting.
- **Protocols**: lowering tests for OpenAI Responses (image/PDF), OpenAI Chat
  (if enabled), Anthropic (image/PDF/text if added), DeepSeek (image).
- **Matrix**: table-driven test asserting each profile's auth/expiry/MIME/limits
  against the documented values.
- **Discoverability**: parity test for the canonical profiles in
  `agentd.example.yaml`, plus the `/api/config` redacted view; assert examples
  parse; assert unknown-profile/missing-field errors name the profile and
  field path.
- **Error contract**: table-driven failures assert each message includes the
  model/provider, full config path, observed/expected value or known values, a
  copy-paste fix or doc anchor, and no secret values.
- **Integration**: fake provider servers for all four profiles; no real
  credentials in CI.

## 10. Security notes

- Header values are secrets; env expansion, redaction and introspection must
  never expose them.
- Upload URLs are operator-configured; do not follow cross-host redirects with
  credential headers.
- File IDs are provider-scoped references: never accept a caller-supplied
  `file_id` as trusted (Anthropic explicitly warns about workspace-wide access).
- Provider-specific filename rules (for example Anthropic's forbidden
  characters/length) are an open design item (see §11); until then only
  multipart escaping is guaranteed.
- Bound response bodies and request timeouts to avoid memory/connection
  exhaustion.

## 11. Deferred / non-goals

1. **Provider-specific filename rules.** The uploader sends the attachment's
   name; a host that needs Anthropic's filename restrictions can normalize it
   before upload. No profile field is added until a concrete rule is required.
2. **Anthropic `container_upload`.** Inbound user files use the document/image
   `file_id` path. `container_upload` targets code-execution artifacts and is
   not part of the inbound media path, so it is not emitted.

## 12. Decision summary

- **D1**: Keep the three axes separate: LLM protocol, `providerFileAPIs`
  (provider Files API), model capability/delivery policy.
- **D2**: Provider uploads stay host-owned in agentd; the SDK only gets a
  capability reporter and an uploader factory.
- **D3**: `providerFileAPIs` entries are declarative, named by the operator, and
  referenced explicitly by models; the generic uploader has no provider
  `switch`.
- **D4**: Auth is a header map; expiry is an explicit style.
- **D5**: File type control is the intersection of protocol MIME, provider
  MIME and optional model MIME.
- **D6**: Extensibility is an acceptance criterion. A declarative provider is
  profile data + a matrix row + `agentd.example.yaml` example + error text +
  tests; a new wire shape is a protocol block/lowering + tests; a new model is a
  catalog entry. None of these may require planner, uploader or transport edits.
  Missing profiles/references on a file-capable model fail startup; incompatible
  MIME/protocol capabilities disable `file` with an actionable warning.
- **D7**: Capability data has one owner per axis (protocol MIME, provider MIME,
  model MIME); the planner only intersects them. Provider-specific branches in
  generic code are not allowed; an explicit adapter is the only escape hatch.
- **D8**: Configuration must be self-evident. The docs matrix, example YAML,
  startup/`--check` summary, actionable validation errors, `/api/config` redacted
  view and parity tests are all required deliverables for a provider; “works,
  but only discoverable from Go code” is not accepted.
- **D9**: Upload profile selection is exact by the **explicit**
  `models[].providerFileAPI` reference; the profile is never derived from the
  protocol, `providerID`, model name, or URL. There is **no implicit default and
  no cross-profile fallback**: a file-capable model with a missing reference,
  `providerID`, or profile fails startup; a profile whose MIME/source does not
  fit the model only disables `file` for that model with a warning. File upload
  is opt-in per explicit reference.
- **D10**: Configuration errors and warnings are remediation instructions. They
  must identify the config path, server/model/provider, observed vs expected
  values, and either a copy-paste YAML fragment or a stable doc anchor. “Invalid
  config” without a fix is a bug.
- **D11**: Protocol compatibility is not provider capability, and capabilities
  are never inherited from a vendor/protocol. A proxy may support any subset; it
  must use its own operator-chosen `providerFileAPIs` entry referenced by
  `models[].providerFileAPI`. Presence and contents of that entry are
  authoritative; a file-capable model with no reference/entry fails startup. Built-in entries are
  never inferred from a protocol name or base URL.
- **D12**: Name configuration by role: `providerFileAPIs.<name>` (reusable
  provider Files API configs), `models[].providerFileAPI` (explicit model
  reference), `media.delivery` (policy), and `media.uploads.urlHost` (our URL
  host). This is a breaking schema change: no legacy aliases.
- **D13**: `MediaStore` reports capabilities (`Fetchable`, MIME/size limits,
  URL TTL) like `MediaUploader` does. A `file://` store must never be treated as
  a provider-fetchable `url`.
- **D14**: `media.delivery.prefer` is validated against available sources at
  startup: `file` requires an explicit `providerFileAPI` reference and matching
  entry, `url` requires a fetchable store, and unavailable sources are reported
  by `--check`/logs rather than failing later at the provider. A file-only
  policy with no other source fails startup.
- **D15**: The LLM protocol and the File API protocol are separate axes. A model
  chooses an LLM protocol; a `providerFileAPIs` entry chooses a `FileAPIKind`
  (codec) plus upload data. Neither implies the other, and extensions are
  additive per axis.
- **D16**: `models[].providerFileAPI` is the config/validation/visibility key;
  the current SDK factory is keyed by `providerID`, so agentd builds a per-server
  `providerID -> profile` map. File-capable models must set an explicit
  `providerID`; the SDK credential fallback to protocol is not used for media
  lookup. One `providerID` cannot map to two profiles in one server; assign
  distinct `providerID`s when the Files API contracts differ.
