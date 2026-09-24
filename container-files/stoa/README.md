# stoa

Quadlet units for [stoa](https://github.com/stanier/stoa), an AI agent
platform (`agentd`), its Svelte web UI (`webd`), and the auth stack that
fronts both (`authd` + `local-idp` + `apikeyd` + per-service Postgres).
Vendored from stoa's own `compose.yaml`/`Containerfile` at the point this
was added -- re-diff against upstream occasionally, especially the
`Containerfile`'s build stages.

## Scope

This is the **core** stack only:

- `agentd`, `webd`, `authd`, `local-idp`, `apikeyd`, `inference-gateway`
- one Postgres per auth-stack service (`agentd`, `authd`, `local-idp`,
  `apikeyd`)
- `nginx` (single ingress for `agentd`+`webd`), `garage` (S3 storage for
  attachments/uploads), `squid` (egress proxy for agentd's sandboxed MCP
  tool containers)

Deliberately **not** included, to avoid duplicating what this repo already
runs fleet-wide or adding scope this pass didn't need:

- stoa's own Prometheus/Loki/Grafana/Promtail -- this repo already has
  `victoriametrics`/`loki`/`grafana`/`promtail`/`alertmanager` for the whole
  fleet.
- `presidio-analyzer` + `pii-filter` (PII redaction sidecar for
  `inference-gateway`).
- The chat bridges (`zulip-bridge`, `slack-bridge`, `discord-bridge`,
  `matrix-bridge`) -- all `profiles: disabled` upstream too.
- `chat` (interactive TUI client) and the various `mcp-*-net` isolated
  subnets upstream declares for agentd's *dynamically created* MCP tool
  containers (agentd spins these up itself via the Podman socket at
  runtime -- see "The Podman socket" below). If a given MCP tool ends up
  needing its own pre-created isolated network the way upstream's compose
  sets up (`mcp-shell-net`, `mcp-netbox-net`, etc.), add `.network` units
  for it following the `stoa-*.network` pattern here.

Add any of the above later by porting the matching `compose.yaml` service
into a new `.container`/`.build` unit, same pattern as the rest of this
directory.

## Before deploying

1. Add `stoa` to the target host's `containerapps_quadlet_services` in
   `../ansible-playbooks` (`host_vars/<host>.yml`) -- nothing here runs
   until that's done (see `container-files/AGENTS.md`).
2. Wire secrets via ansible-vault, same pattern as every other
   `container-files` service -- see
   `../ansible-playbooks/docs/VAULT.md`'s "container-files secrets", and:
   - `.env.example` -> `containerapps_secret_files` dest `stoa/.env`
   - `postgres-<svc>.env.example` -> one `containerapps_secret_files`
     entry each, dest `stoa/postgres-<svc>.env`
   - `podman secret create gateway_api_key -` (and `venice_api_key`,
     `arcee_api_key`, `openrouter_api_key` if those upstreams are enabled
     in `data/config/inference-gateway.yaml`) on the target host directly
     -- Podman secrets aren't part of the synced tree.
3. Pick an LLM provider: `.env`'s `LLM_PROVIDER`/`GATEWAY_API_URL` and
   `data/config/config.yaml`'s `llm.providers` are left at their upstream
   defaults (Anthropic) but commented as a stub -- fill in for real before
   starting `stoa-agentd`.
4. After first boot, Garage needs a one-time bucket/key setup (upstream's
   own `garage-init` is `profiles: disabled` too, i.e. manual by design) --
   run stoa's `scripts/garage-init.sh` equivalent commands against the
   running `stoa-garage` container.
5. Seed `/srv/stoa/squid/squid.conf` from `data/config/squid.conf.default`
   once, on hosts running this service (see "Persistent data" below) --
   agentd overwrites it with real per-container ACLs on its own first
   start.

## The Podman socket

`stoa-agentd.container` bind-mounts the host's rootless Podman socket
(`%t/podman/podman.sock`) read-only into the container so agentd can spin
up sandboxed MCP tool containers on demand. This is a real privilege: a
container with access to the Podman socket can, at minimum, start other
containers as the same user. Only deploy `stoa` on a host where that
tradeoff is acceptable, and don't add `Network=host` or drop the
`ro` on that mount.

## Persistent data

Following the `gitea`/`dovecot` pattern in `container-files/AGENTS.md`,
state that must survive a redeploy lives outside the synced tree at
`/srv/stoa/` on the target host (create it out-of-band, e.g. via the
Ansible role that provisions `/srv/gitea` and `/srv/dovecot` today):

- `/srv/stoa/traces`, `/srv/stoa/logs`, `/srv/stoa/sessions` -- agentd's
  file-based state (session *metadata* itself is in Postgres).
- `/srv/stoa/squid/squid.conf` -- shared between agentd (writer) and squid
  (reader); see step 5 above.
- `/srv/stoa/garage/{meta,data}` -- Garage's object store.

`data/config/` is read-only, non-secret config committed to this repo (or
ansible-templated in place, same as other services) and safe to live
inside the synced tree.

## Networking

One dedicated network per trust boundary, `NetworkName=` pinned to the
exact names upstream's `compose.yaml` uses (`agentd-db-net`, `idp-net`,
`postgres-<svc>-net`, `agent-external`) in case agentd's own code assumes
those names when it creates sibling MCP containers:

- `stoa-external.network` (`agent-external`) -- agentd, authd, apikeyd,
  inference-gateway, webd, nginx, garage, squid. Not internal-only: several
  of these need real internet egress (LLM calls, OAuth).
- `stoa-idp.network` (`idp-net`) -- authd <-> local-idp only, internal.
- `stoa-db-<svc>.network` (`<svc>` = agentd/authd/localidp/apikeyd) --
  each service and only its own Postgres, internal.

`stoa-squid` is aliased `stoa-squid` (not `squid`) on purpose --
`stoa-agentd.container`'s `MCP_PROXY_ADDRESS` points at that literal
hostname, matching the `container_name` upstream's compose file gives it.
