# agentd-supervisor

A reusable **host supervisor** for [`agentd`](../agentd). A host that manages
project instances through a `supervisor_command` (an IM bridge, an orchestrator,
a sidecar) invokes this binary with **one JSON request on stdin** and reads **one
JSON response on stdout**; it satisfies the request by calling a running agentd's
admin (control) API — the host itself never starts Go processes.

```
acquire  {"action":"acquire","project_id":"...","readonly":false,"transport":"http"}
      →  {"project_id":"...","transport":"http","server_base":"http://127.0.0.1:XXXXX",
          "working_directory":"/srv/source","ephemeral":true}
release  {"action":"release","project_id":"..."}
      →  {"ok":true}
```

- **Config**: see [`supervisor.example.yaml`](./supervisor.example.yaml);
  `agentd.baseURL`/`token` point at an agentd with `control.listen` set, and
  `project` is the template cloned per project (agentd derives the isolated view).
- **Remote / container**: `agentd.baseURL` may point at any reachable agentd —
  including one in a container or on another host; the supervisor never starts it.
  Container/VM orchestration is a deployment concern
  (`docs/DESIGN-ISOLATION.md` §15).
- **Run**: `agentd-supervisor -config supervisor.yaml` (reads the request on stdin).
- A **read-only** conversation shares the source directory; a **writable** one gets
  the configured isolated view (`worktree`/`overlay`/`copy`), prepared by agentd.
- `${VAR}` / `${VAR:-default}` are expanded by the agentd config loader.
