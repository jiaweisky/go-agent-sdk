# agentd

A standalone multi-transport server for go-agent-sdk: one process runs one agent
client per configured **server**, each over HTTP/SSE and/or A2A.

- **Config**: see [`agentd.example.yaml`](./agentd.example.yaml); the full design is
  in [`docs/DESIGN-AGENTD.md`](../../docs/DESIGN-AGENTD.md).
- **Run**: `agentd -config agentd.yaml` — `-check` validates the config and exits,
  `-version` prints the version.
- Secrets live in the config file (keep it mode 0600) or are referenced from it
  with `${VAR}` / `${VAR:-default}`; any string value supports this, and no
  environment is read implicitly.

```yaml
models:                              # the model catalog, defined once
  - id: deepseek-chat
    model: deepseek-chat             # wire model name (required)
    protocol: openai-compatible-chat
    modelBaseURL: https://api.deepseek.com
    credentials: { apiKey: sk-... }
    inputModalities: [text]

servers:
  - name: http
    kind: http
    listen: 127.0.0.1:8787
    root: ./var/http
    workingDirectory: .
    configDirs: [./agents]
    agents: [build]
    auth: { type: bearer, token: replace-me }
```

Servers with the same `listen` share one listener and are routed by `host` (a
virtual host) and `mount` (a path prefix); an A2A server marked `default` owns the
origin-root `/.well-known/agent-card.json`, and `/.well-known/agents.json` lists
every A2A agent on that listener.

## CodeBuddy

Point CodeBuddy at the A2A listener's aggregate discovery endpoint. agentd prints
the exact URL at startup, e.g.:

```
agentd: a2a discovery: http://127.0.0.1:8788/.well-known/agents.json (all cards, codebuddy)
```

```sh
CODEBUDDY_A2A_OUTBOUND_ENABLED=1 \
CODEBUDDY_REMOTE_AGENTS_URL=http://127.0.0.1:8788/.well-known/agents.json \
codebuddy
```

