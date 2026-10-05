# a2aserve

`a2aserve` runs an A2A (Agent2Agent) server backed by go-agent-sdk. By default it
exposes the built-in `build` agent. Configuration comes from a YAML file and CLI
flags; **no environment variables are read**.

## Usage

```sh
a2aserve --config a2aserve.yaml
# or flags only
a2aserve --root ~/.go-agent-sdk --model gpt-4o-mini --protocol openai-responses \
  --config-dir ./agents
```

Flags (each overrides the config file; a repeatable flag replaces the file's list):

| Flag | Meaning |
|---|---|
| `--config <path>` | YAML config file |
| `--listen <host:port>` | listen address (default `127.0.0.1:9099`) |
| `--base-url <url>` | public base URL advertised in cards |
| `--root <dir>` | SDK state directory (required) |
| `--working-dir <dir>` | project root (default: process cwd) |
| `--model <id>` | default model (required) |
| `--protocol <id>` | wire protocol, e.g. `openai-responses` (required) |
| `--keep-alive <dur>` | SSE keep-alive interval, e.g. `15s` |
| `--config-dir <dir>` | agent/skill config directory (repeatable) |
| `--agent <name>` | agent to expose (repeatable, default `build`) |

## Config file

```yaml
listen: 127.0.0.1:9099
baseURL: ""                         # optional; cards fall back to the request origin
root: ~/.go-agent-sdk               # required
workingDirectory: /srv/project      # optional
configDirs:
  - /srv/project/.agents            # agents/skills loaded from files
model: gpt-4o-mini                  # required
protocol: openai-responses          # required
credentials:                        # required; the only secret source (no env)
  openai: { apiKey: sk-... }
agents: [build]                     # existing agent names to expose
inboundPermissions:                 # default: SDK deny-by-default ruleset
  - { action: bash, resource: "*", effect: deny }
keepAlive: 15s
# auth:                             # optional; when set, RPC requires a bearer token
#   bearer: <token>
```

Configuration precedence is **defaults < file < flags**. Unknown file keys are
rejected. Credentials and the auth token live only in the file (flags would leak
into the process list).

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
- Inbound sessions run under the SDK deny-by-default ruleset unless overridden
  with `inboundPermissions`.
- When `auth.bearer` is set, the card declares an HTTP bearer scheme and every
  JSON-RPC request must send `Authorization: Bearer <token>` (cards stay public).
