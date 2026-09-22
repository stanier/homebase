# container-files

Podman [Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit definitions and config for every containerized service in the
homelab — one subdirectory per service. This repo has no deployment logic
of its own: `ansible-playbooks`' `roles/containerapps` (invoked by
`plays/apps/podman.yml`) clones or rsyncs this whole tree onto each
`[containers]` host and turns each named subdirectory into a systemd
unit. See `ansible-playbooks/docs/VAULT.md` for how secrets reach these
services and `ansible-playbooks/docs/Typical_Procedure.md` for restoring
a host's data after a rebuild.

Also published as a [Zensical](https://zensical.org) docs site at
`docs.apps.testzone.internal/container-files/` (`zensical.toml`,
`Containerfile`, `.gitea/workflows/deploy.yml` — same pattern as
`homebase`'s docs site).

## Layout

Each service subdirectory holds:

- `<service>.container` — the Quadlet unit (`[Container]`/`[Service]`/`[Install]`
  sections), generated into a real systemd service by `podman-systemd`
  on the target host.
- `Containerfile` + `<service>.build` (only where the service needs a
  custom image, e.g. `caddy`, `dns`, `gitea-runner`, `syslog`) — `Image=`
  in the unit then points at the built image tag instead of a registry
  ref.
- `data/` (where present) — bind-mounted config: static files committed
  here, or ones Ansible templates in at deploy time (secrets, host-specific
  values). `.env` files and a few generated paths (`dns/data/keys/`,
  `grafana/provisioning/datasources/`, `adguardhome/data/conf/`) are
  gitignored — see `.gitignore` and `ansible-playbooks/docs/VAULT.md`'s
  "container-files secrets".

A host only runs the services listed for it in
`containerapps_quadlet_services` (its `host_vars`, in `ansible-playbooks`)
— this repo just holds the superset every host draws from.

## Services

| Service | What it is |
|---|---|
| `caddy` | Reverse proxy / TLS termination for every public vhost, using a private offline CA |
| `dns` (BIND) | Authoritative DNS, RFC2136 dynamic updates for ACME DNS-01 |
| `adguardhome` | Recursive DNS + ad/tracker blocking |
| `alertmanager` | Alert routing/notification (pairs with `victoriametrics`/`grafana`) |
| `authentik` | SSO / identity provider (server + worker + Postgres + Redis) |
| `keycloak` | SSO / identity provider (server + Postgres) |
| `gitea` | Git hosting |
| `gitea-runner` | Gitea Actions CI runner (`act_runner`) |
| `code-server` | Browser-based VS Code |
| `roundcube` + `dovecot` | Webmail + IMAP/LMTP (pairs with native Postfix, see `ansible-playbooks/roles/mail_server`) |
| `grafana` | Dashboards |
| `influxdb` | Time-series metrics store |
| `victoriametrics` | Time-series metrics store (Prometheus-remote-write compatible) |
| `loki` + `promtail` | Log aggregation + shipping |
| `minio` | Self-hosted S3-compatible object storage |
| `syslog` | Central rsyslog receiver for the fleet |
| `node-exporter` / `podman-exporter` | Prometheus metrics exporters (host / podman) |
| `uptime-kuma` | External-facing uptime/status monitoring |
| `wazuh` | Endpoint security (HIDS): manager + indexer + dashboard (agents installed by `ansible-playbooks/roles/wazuh_agent`) |
| `vaultwarden` | Self-hosted Bitwarden-compatible password manager |
| `windows` | A full Windows VM-in-a-container (`dockur/windows`), for one-off Windows-only needs |

See [`docs/roadmap.md`](docs/roadmap.md) for services planned but not
yet added.

## Networking conventions

- Most services use `Network=host` — either because they need a
  privileged/well-known port, or because they need to reach a native
  (non-containerized) service over loopback (`dovecot`/`roundcube` talking
  to host Postfix; see the comments in those `.container` files).
- `authentik` and `keycloak` are the exceptions: each has its own
  dedicated Quadlet network (`authentik.network` / `keycloak.network`)
  so its containers can address each other by `NetworkAlias` without
  publishing internal ports. `wazuh` follows the same pattern
  (`wazuh.network`) for its manager/indexer/dashboard, while still
  publishing the manager's agent-facing ports (1514/1515) since agents
  connect cross-host.
- Named volumes (`Volume=<service>_<name>:/path`) are Podman-managed and
  host-local; bind mounts under `/srv/containers/container-files/...`
  come from this repo's sync. A few services (`gitea`'s
  `/srv/gitea/{data,config}`, `dovecot`'s `/srv/dovecot/...`) deliberately
  bind-mount *outside* the synced tree — that data would otherwise be
  destroyed by the `containerapps_source=local` rsync's `--delete` on
  every deploy.

## Making a change

Edit the relevant `.container`/`data/` file and commit. On the next
`plays/apps/podman.yml` run, `roles/containerapps` re-syncs this tree and
restarts any service whose unit file or bind-mounted config actually
changed (see that role's own comments for exactly how it detects
non-unit-file changes). There's nothing to run in this repo directly —
it's config, not code.
