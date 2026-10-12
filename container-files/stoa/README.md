# stoa

Quadlet units for [stoa](https://github.com/stanier/stoa), an AI agent
platform (`agentd`), its Svelte web UI (`webd`), and the auth stack that
fronts both (`authd` + `local-idp` + `apikeyd` + per-service Postgres).
Ported from stoa's own `compose.yaml` at the point this was added --
re-diff against upstream occasionally.

This directory does **not** vendor stoa's application source or
Containerfile -- `container-files` only holds Quadlet units/config, not
app code (see `container-files/AGENTS.md`). Each `stoa-*.build` unit's
`SetWorkingDirectory=` instead points `podman build` straight at
`https://github.com/stanier/stoa.git#main` as a live git-context build:
Podman clones it fresh on every build. `#main` floats with upstream's
default branch; pin to a specific commit there instead if these builds
ever need to stop moving when upstream does.

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

The `stoa` host in `../ansible-playbooks`' `testzone` inventory is already
wired up end to end: `host_vars/stoa.yml` sets
`containerapps_quadlet_services: [stoa]`, composes the full `.env` this
directory's units read via `containerapps_env` (note this goes through
`containerapps_env`, not a `.env.example` copy -- that mechanism only
writes one `.env` per service directory, which is why the four Postgres
passwords are separate `containerapps_secret_files` entries instead,
keyed to the `postgres-<svc>.env` filenames each `stoa-postgres-*.container`
expects), seeds `/srv/stoa/squid/squid.conf`'s deny-all default, and adds
Caddy routes on `container-sandbox` for `stoa`/`stoa-authd`/`stoa-apikeyd`/
`stoa-idp`/`stoa-infgw.{{ base_domain }}`. `roles/containerapps` also
creates `/srv/stoa/{traces,logs,sessions,garage}` before first start (its
`containerapps_service_dirs` default).

If you're pointing this at a different host or environment, replicate
that host_vars file and Caddyfile block -- `container-files/AGENTS.md`'s
"a host only runs services listed in `containerapps_quadlet_services`"
still applies.

What that wiring does **not** cover -- do these by hand:

1. Add the `vault_stoa_*` secrets (DB passwords, `AUTHD_ADMIN_TOKEN`,
   `APIKEYD_ADMIN_TOKEN`, `AUTHD_OAUTH_LOCAL_CLIENT_SECRET`,
   `ATTACHMENT_SECRET`, Garage's RPC/admin/access/secret keys) to
   `group_vars/all/vault.yml` via `ansible-vault edit` -- see
   `../ansible-playbooks/docs/VAULT.md`. `group_vars/all/vars.yml` already
   references all of them under their plain `stoa_*` names.
2. Add the LLM keys (see "LLM keys" below): `vault_stoa_anthropic_api_key`
   (the real Anthropic key), `vault_stoa_openrouter_api_key`, and
   `vault_stoa_gateway_api_key` (issued by apikeyd after first boot).
   `roles/containerapps` turns them into Podman secrets via
   `containerapps_podman_secrets` before starting any unit;
   `plays/apps/stoa_secrets.yml` replaces them later.
3. agentd's `gateway` provider points at inference-gateway
   (`GATEWAY_API_URL` in host_vars), not straight at Anthropic.
4. After first boot, Garage needs a one-time bucket/key setup matching the
   access/secret key pair from step 1 (upstream's own `garage-init` is
   `profiles: disabled` too, i.e. manual by design) -- run stoa's
   `scripts/garage-init.sh` equivalent commands against the running
   `stoa-garage` container.

## LLM keys

agentd never holds a provider key. Its `gateway` provider talks
Anthropic's API to inference-gateway, which checks the caller's
`stoa-infgw_*` key against apikeyd and then forwards with the real
upstream key (`anthropic_api_key`, `openrouter_api_key`). Web sessions
use the per-user key authd provisions at login; agentd's own
`gateway_api_key` covers model discovery and sessions with no user.
Pointing agentd straight at api.anthropic.com breaks web chat with a 401:
agentd sends the user's `stoa-infgw_*` key there.

`gateway_api_key` can only be issued once apikeyd is running. On the
stoa host, as the containers user:

    curl -s -X POST -H "Authorization: Bearer $APIKEYD_ADMIN_TOKEN" \
      http://127.0.0.1:8889/admin/users/agentd/key

Put the returned `key` in `vault_stoa_gateway_api_key`, then run
`plays/apps/stoa_secrets.yml`.

## Login (Keycloak)

authd brokers every login. Its config isn't in `data/config/`: it's
templated per environment as `stoa/authd.yaml` by `host_vars/stoa.yml`'s
`containerapps_secret_files`, because the issuer URL needs `base_domain`.
It has a single provider, `keycloak` (`https://keycloak.<base_domain>/realms/master`),
so FreeIPA users log in through Keycloak's LDAP federation. The `stoa`
client itself comes from `plays/apps/keycloak.yml`, and its secret is
`vault_keycloak_stoa_client_secret`, used by both sides.

Keep it at one provider: the web UI doesn't pass `?provider=`, and authd
only falls back to a default when exactly one is configured.
`local-idp` (and its Postgres) still runs but is no longer used for login.

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
`/srv/stoa/` on the target host. `roles/containerapps`' "Ensure stoa's
persistent-data directories exist" task creates these automatically
(gated on `'stoa' in containerapps_quadlet_services`, same as gitea's
equivalent task) -- nothing to do by hand:

- `/srv/stoa/traces`, `/srv/stoa/logs`, `/srv/stoa/sessions` -- agentd's
  file-based state (session *metadata* itself is in Postgres).
- `/srv/stoa/squid/squid.conf` -- shared between agentd (writer) and squid
  (reader); seeded from `host_vars/stoa.yml`'s `containerapps_secret_files`
  entry, whose parent-directory task creates `/srv/stoa/squid` itself.
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
