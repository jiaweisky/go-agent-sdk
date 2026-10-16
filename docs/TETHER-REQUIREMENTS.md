# Tether 媒体接入改造需求（迁移到 agentd 单一策略）

> 交接文档（面向 Tether 项目）。目标：**删除 Tether 侧自实现的媒体交付策略，改由 agentd 的 `servers[].media` 策略单一拥有**；Tether 只保留"连接器相关、只有它才能做"的部分。
>
> 背景：Tether 实现 `engine/media_ingest.py` 时，SDK 还不支持 inline/file/url 的选择。现在 agentd（内嵌 SDK）已具备该能力，Tether 应复用它、并删除重复策略，避免"双重策略"（重复 down 采样、重复上传等）。

## 1. 职责划分（目标态）

| 环节 | 归属 | 说明 |
|---|---|---|
| 从 IM 平台**下载字节** | **Tether** | 只有连接器能拿到 IM 附件 |
| 文件名解析（Content-Disposition 等） | **Tether** | 连接器细节 |
| **下载前**按平台声明大小的预拦 | **Tether** | agentd 看不到下载前的大小；防止拉取巨物 |
| 下载失败处理（记录/提示，不静默丢） | **Tether** | 连接器细节 |
| **选源**：inline / provider file / url | **agentd** | `servers[].media.prefer` 等 |
| **上传**到 provider Files API | **agentd** | 由 `media.upload` + `mediaUpload` 决定 |
| **下采样**（图片进程内；音视频 ffmpeg） | **agentd** | 图片 SDK 内纯 Go；音视频 host 接 ffmpeg |
| **spool 落盘 + 路径引用** | **agentd** | 落在 `{Root}/data/spool`，并对 read 放行 |
| **模态门禁**（模型能否消费） | **agentd** | 按 `models[].media.forms` + 协议 |
| **尺寸硬上限**（超限跳过） | **agentd** | `media.maxBytes`（按模态） |

## 2. Tether 要**删除**的逻辑

在 `MediaIngest._deliver_supported` / `_ingest_one` 中删除：

1. `inline_limit_bytes` 的 inline 判定；
2. 调用 `client.upload_file(...)`（`POST /api/session/{id}/file`）——**不再由 Tether 直接上传**；
3. `downsample`（`util/media.py` 的 Pillow / ffmpeg 缩水）；
4. `_spool(...)`（写盘 + 路径 note）；
5. 模态门禁（`kind not in inputs` → spool）；
6. `max_bytes` 的硬跳过（保留"预拦"，见 §3.1，但用途改为"下载前保护"而非交付策略）。

## 3. Tether 要**保留/新增**的逻辑

### 3.1 保留：连接器相关
- `connector.download(attachment)`；
- 文件名解析；
- **下载前的声明大小预拦**：若平台已给出 `attachment.size` 且超过**安全上限**，直接提示并跳过（不下载）。这个上限是 **Tether 的连接器保护参数**（例如 `connector.max_attachment_bytes`），**不等于**交付策略，也**不应**再决定 inline/upload。
- 下载失败的提示。

### 3.2 新增：把字节交给 agentd，让它决策
对每个可下载的附件，**作为 inline 附件**提交（不再自行选源）：

```jsonc
// POST /api/session/{id}/prompt
{
  "prompt": "<user text + 任何 Tether 自有的提示>",
  "files": [
    { "uri": "data:<mime>;base64,<bytes>", "mime": "<mime>", "name": "<name>" }
  ]
}
```

- **不要**设置 `source`/`fileId`（除非确实要覆盖策略，见 §5.2）。留空即由 agentd 按 `media:` 决定 inline / 上传 / 下采样 / spool。
- 多附件同上，逐个放入 `files`。

### 3.3 可选：超大媒体改走 url（仅当连接器可能放行巨物）
若担心"把大字节 base64 送进 agentd"（IM 一般不会，见 §7），Tether 可自行上传到**自己的对象存储**并传 `url`：

```jsonc
{ "files": [ { "source": "url", "uri": "https://…", "mime": "<mime>", "name": "<name>" } ] }
```

- 这仅在 agentd 运营方开启 `media.fetchURL` 时被接受；否则会被省略（并给 `url_not_allowed`）。
- **默认不必做**：IM→Tether 的尺寸通常很小，走 §3.2 即可。

## 4. 配置迁移（Tether → agentd）

原先属于 Tether 的策略项**移到 agentd**（`servers[].media`）：

```yaml
# agentd 配置（示例）
servers:
  - name: tether
    media:
      inlineLimitBytes: 262144          # 或按模态 { default:, video: }
      prefer: [file, inline]            # 首选，一次可行；url 仅由调用方给
      upload: true                      # 允许上传到 provider
      downsample: true                  # 图片进程内；音视频 ffmpeg（agentd 默认接）
      spool: { dir: "{Root}/data/spool" }
      fetchURL: true                    # 若允许 Tether 传 url
      maxBytes: 52428800                # 或按模态 { default:, image:, video: }
    mediaUpload:                        # provider Files API（如需 file_id）
      url: https://api.deepseek.com/files
      header: Authorization
      value: "Bearer ${DEEPSEEK_API_KEY}"
      fields: { purpose: user_data }
      idField: id
      expiresAfter: 86400               # provider 侧有效期（可选）
```

Tether 侧配置**只保留**：连接器参数 + 下载前安全上限（§3.1）。删除 `inline_limit_bytes` / `max_bytes`（交付语义）/ `spool_dir` / `downsample`。

## 5. 与 agentd 的接口契约

### 5.1 提交
- `POST /api/session/{id}/prompt`，`files[]` 项：`{ uri | fileId, mime, name }`（inline 用 `uri` 为 `data:` URL）。
- **不再使用** `POST /api/session/{id}/file`（agentd 需要时会自行上传）。

### 5.2 覆盖（可选）
- 调用方**声明的源**在**可行**时优先于策略：想强制 inline/url/file 时设 `source`；否则留空交给策略。

### 5.3 结果与错误（要能透传给用户）
- 请求级错误（4xx，JSON）：
  - `413` `request body exceeds the <N>-byte limit`（整体请求体超限）；
  - `415` `unsupported media type "<mime>"; allowed: <patterns>`（A2A ingest）；
  - `400` `invalid JSON body` 等。
- **附件被省略**（请求 200，但附件未送达模型）：
  - 事件 `session.next.attachment.omitted`，含 `reason`（如 `too_large` / `unsupported_modality` / `unsupported_source` / `url_not_allowed`）与 **`detail`**（如 `limit <N> bytes`）。
  - 模型侧看到等价的文本 note；Tether 应把该事件**呈现给用户**（例如"图片过大，已跳过（上限 N 字节）"）。
- 会话侧已 `Accept`：**生命周期**：上传文件由 provider 侧 TTL 管理；**Tether 不负责删除**。

## 6. 非目标

- Tether 不再实现任何"选源/上传/下采样/spool/模态/尺寸"策略（单一所有者是 agentd）。
- Tether 不做 provider Files API 的生命周期（删除/过期）管理。
- 不改变 Tether 的连接器、会话编排、消息渲染等其它职责。

## 7. 尺寸与传输的现实约束（决策依据）

- **IM 平台（尤其 bot API）对媒体卡得很死**：图片 ~2MB、语音 ~2MB/60s、视频 ~10MB、文档几十 MB；很多 bot 下载上限 ~20MB。
- 因此 **1GB 级附件经 IM→Tether 基本不成立**；Tether↔agentd 的"整块字节 + base64"在现实尺寸（且 Tether 自身有安全上限）下可接受。
- agentd 有**请求体上限**（默认 128 MiB，`servers[].maxBodyBytes`）；Tether 的**下载前安全上限应 ≤ 该值**，避免 413。
- 若个别连接器确能放行巨物，用 §3.3 的 url 路径绕开 agentd 的内存。

## 8. 验收标准

1. **小图**：Tether 传 inline → agentd inline 给模型，模型能描述。
2. **大图**：Tether 传 inline → agentd 按 `prefer:[file,inline]`+`upload` 上传 → 模型通过 provider file 引用看到（DeepSeek `file_id` 等）。
3. **超大**：超过 `media.maxBytes` → 被省略，产生 `attachment.omitted`（`reason: too_large`，`detail: limit N bytes`），Tether 呈现给用户。
4. **不支持模态**：如模型不收该模态 → 按 `media.spool` spool（或省略），模型拿到路径/note，且 read 可访问。
5. **不重复**：Tether 不再上传/下采样；一次请求内 agentd 也不重复处理。
6. **配置**：删除 Tether 的策略配置后行为仍符合 1–4。

## 9. 依赖与风险

- **依赖**：agentd 的 `servers[].media`（及需要时的 `mediaUpload`/`storage`/`fetchURL`）已按运营方意图配置；未配置时 agentd 退化为 inline-first（Tether 的"上传/缩水"保证消失）。
- **依赖**：agentd 接好 ffmpeg（音视频下采样）；否则音视频不会缩水（会走 spool/省略）。
- **风险**：若某连接器放行大文件且 Tether 未设安全上限 → 请求体可能触发 413；用 §3.1 上限兜底。
- **风险**：Tether 与服务端若"两边都做策略"会重复；本改造以"服务端单一拥有"为准。
