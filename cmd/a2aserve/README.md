# a2aserve

`a2aserve` runs an A2A (Agent2Agent) server backed by go-agent-sdk. By default it
exposes the built-in `build` agent. Configuration comes from a YAML file and CLI
flags; **no environment variables are read**.

## Usage

```sh
a2aserve --config a2aserve.yaml
# or flags only (credentials still need the file)
a2aserve --root ~/.go-agent-sdk --model deepseek-flash \
  --protocol openai-compatible-chat --model-base-url https://api.deepseek.com \
  --config-dir ./agents
```

Flags (each overrides the config file; a repeatable flag replaces the file's list):

| Flag | Meaning |
|---|---|
| `--config <path>` | YAML config file |
| `--listen <host:port>` | listen address (default `127.0.0.1:9099`) |
| `--base-url <url>` | the A2A server's public base URL advertised in cards |
| `--root <dir>` | SDK state directory (required) |
| `--working-dir <dir>` | project root (default: process cwd) |
| `--model <id>` | default model (required) |
| `--protocol <id>` | wire protocol, e.g. `openai-responses` (required) |
| `--model-base-url <url>` | model API base URL (protocol default when empty) |
| `--keep-alive <dur>` | SSE keep-alive interval, e.g. `15s` |
| `--config-dir <dir>` | agent/skill config directory (repeatable) |
| `--agent <name>` | agent to expose (repeatable, default `build`) |
| `--allow-insecure` | allow a non-loopback bind without auth |

## Config file

A complete annotated example lives at
[`a2aserve.example.yaml`](a2aserve.example.yaml); copy it to `a2aserve.yaml`
(git-ignored) and edit. The essentials:

```yaml
listen: 127.0.0.1:9099
baseURL: ""                         # optional; the A2A server's public base URL (cards)
root: ~/.go-agent-sdk               # required
workingDirectory: /srv/project      # optional
configDirs:
  - /srv/project/.agents            # agents/skills loaded from files
model: deepseek-flash               # required; the model id sent to the provider
protocol: openai-compatible-chat    # required
modelBaseURL: https://api.deepseek.com   # model API base URL (see below)
credentials:                        # required; the only secret source (no env)
  apiKey: sk-...                    # the provider API key (bearer/header protocols)
  # aws:                            # Amazon Bedrock (SigV4)
  #   region: us-east-1
  #   accessKeyId: ...
  #   secretAccessKey: ...
  #   sessionToken: ...             # optional
agents: [build]                     # existing agent names to expose
cards:                              # optional per-agent card overrides
  build: { name: Build Agent, description: "..." }
contextLimit: 128000                # optional; drives automatic compaction
outputLimit: 8192                   # optional
inboundPermissions:                 # default: SDK deny-by-default ruleset
  - { action: bash, resource: "*", effect: deny }
keepAlive: 15s
# auth:                             # optional; when set, RPC requires a bearer token
#   bearer: <token>
# allowInsecure: false              # required to bind non-loopback without auth
# tls:                              # optional; serve HTTPS when both are set
#   certFile: /etc/a2aserve/tls.crt
#   keyFile: /etc/a2aserve/tls.key
```

Configuration precedence is **defaults < file < flags**. Unknown file keys are
rejected. Credentials and the auth token live only in the file (flags would leak
into the process list).

## Model endpoint and key

- **Key** — `credentials.apiKey` (bearer/header protocols) or `credentials.aws`
  (Amazon Bedrock SigV4). This is the only place a secret is read; no
  environment variables are consulted.
- **URL** — `modelBaseURL` (flag `--model-base-url`):
  - set it for a provider that is not the protocol's default host (DeepSeek,
    a self-hosted OpenAI-compatible gateway, ...);
  - leave it empty to use the protocol's built-in endpoint (OpenAI
    `https://api.openai.com/v1`, Anthropic, Gemini);
  - for `bedrock-converse` it is derived from `credentials.aws.region` when empty.

DeepSeek `deepseek-flash` over each protocol it exposes:

| `model` | `protocol` | `modelBaseURL` |
|---|---|---|
| `deepseek-flash` | `openai-compatible-chat` | `https://api.deepseek.com` |
| `deepseek-flash` | `openai-responses` | `https://api.deepseek.com` |
| `deepseek-flash` | `anthropic-messages` | `https://api.deepseek.com/anthropic` |

## Agents

`agents` lists the **names of already-registered agents** to expose. Agents can be:

- built-in (`build`, `plan`, `general`, `explore`, ...),
- registered in code, or
- loaded from files under `configDirs` (`{agent,agents}/**/*.md`, `{mode,modes}/*.md`).

An unknown name is an error. Only listed agents are exposed; each gets its own
card at `/agents/<name>/.well-known/agent-card.json` (the first is also served at
`/.well-known/agent-card.json`).

## Security

- Binds loopback by default and requires no authentication.
- **Binding a non-loopback address without `auth.bearer` is refused** unless
  `allowInsecure: true` (or `--allow-insecure`) is set explicitly.
- Inbound sessions run under the SDK deny-by-default ruleset unless overridden
  with `inboundPermissions`.
- When `auth.bearer` is set, the card declares an HTTP bearer scheme and every
  JSON-RPC request must send `Authorization: Bearer <token>` (cards stay public).
