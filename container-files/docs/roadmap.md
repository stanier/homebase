# Roadmap

Services planned for addition to the baseline inventory, in build order.
Each gets its own `container-files/<service>/` subdirectory following the
pattern in [Services](services.md#adding-a-service) once its turn comes
up; this page just tracks status until then.

| Service | What it adds | Status |
|---|---|---|
| [Uptime Kuma](https://github.com/louislam/uptime-kuma) | External-facing uptime/status monitoring, complementing Grafana's internal metrics view | Added — see `uptime-kuma/` |
| [MinIO](https://min.io/) | Self-hosted S3-compatible object storage, for backups/exports that currently go straight to external S3 | Added — see `minio/` |
| [Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/) | Alert routing/notification, pairing with `victoriametrics` (needs a rule evaluator — `vmalert` or Grafana alerting — pointed at it; out of scope for this pass) | Added — see `alertmanager/` |
| [Vaultwarden](https://github.com/dani-garcia/vaultwarden) | Self-hosted Bitwarden-compatible password manager | Added — see `vaultwarden/` |
| [Trivy](https://github.com/aquasecurity/trivy) | Vulnerability/SBOM scanning, run in server mode so `gitea-runner` CI doesn't re-pull the DB every job | Added — see `trivy/` |
| [WireGuard](https://www.wireguard.com/) (`wg-easy`) | VPN entry point for remote access, complementing the FreeIPA-backed personal SSH/sudo access | Added — see `wireguard/` |

## Open questions per service

- **MinIO** — shipped on the default bridge with published ports
  (`9002` API, `9001` console — `9000` was already taken by
  `authentik`), matching `grafana`/`victoriametrics`. Nothing needs to
  reach it by container DNS name yet; revisit with a dedicated network
  if that changes (e.g. a future Loki/VictoriaMetrics S3 backend on the
  same host). Its upstream image is also mid-migration off free Docker
  Hub pushes — see the comment in `minio/minio.container`.
- **Alertmanager** — needs `data/alertmanager.yml` for routing config
  and receiver secrets (webhook URLs, SMTP creds) templated in via
  Ansible, same as other services' `.env` files.
- **Vaultwarden** — shipped with `SIGNUPS_ALLOWED=false` and default
  SQLite (fine at homelab scale, no Postgres sidecar). `DOMAIN` and
  `ADMIN_TOKEN` go in the Ansible-templated `.env`; use the `/admin`
  panel (gated by `ADMIN_TOKEN`) to invite the first account rather
  than opening signups.
- **Trivy** — pinned to `0.74.0`, well past the March 2026
  `aquasec/trivy` supply-chain compromise (v0.69.4-0.69.6 backdoored
  across Docker Hub, GHCR, and ECR — GHSA-69fq-xp46-6x23). Server mode
  is gated by a `TRIVY_TOKEN` in the Ansible-templated `.env`; whichever
  `gitea-runner` job calls it needs the same token via `--token`. Check
  Aqua's advisories before ever bumping this tag, not just the
  changelog.
- **WireGuard** — shipped as `wg-easy:15` (its on-disk config format
  isn't compatible with `14`) on the default bridge with published
  ports (`51820/udp` tunnel, `51821/tcp` admin UI) plus `NET_ADMIN`/
  `SYS_MODULE` and the two sysctls its NAT/masquerade setup needs —
  same shape as upstream's own reference compose, no `Network=host`
  needed. v15 dropped `WG_HOST`/`PASSWORD_HASH` as env vars entirely —
  both get set through the web UI's first-login setup wizard instead,
  so there's no `.env`/vault entry for this service at all.

## Ansible wiring (testzone)

`trivy` and `wireguard` each got their own dedicated `[vm]`/`[containers]`
host in `ansible-playbooks/inventory/testzone` (`.215`/`.216`, vmids
`5215`/`5216` — next in sequence after `dnsguard`), rather than sharing
an existing host, so each one's firewall scoping stays simple. Trivy
still needs `vault_trivy_token` added to `group_vars/all/vault.yml` by
hand (`ansible-vault edit` — that file is encrypted and not something
this pass could fill in) before `plays/apps/podman.yml` can actually
bring it up. WireGuard needs no vault entry at all — its admin
account/`WG_HOST` get set on first login instead, at
`https://<wireguard host>:51821` over the app network (see the
`firewall_rules` in its `host_vars`). Whatever router-level port-forward
gets `51820/udp` to that host from the real internet is still outside
what either repo can express.

Uptime Kuma, Alertmanager, and Vaultwarden are now wired into
`container-sandbox`'s `containerapps_quadlet_services`, sharing that
host with `code-server`/`caddy` rather than getting dedicated hosts —
each also picked up a `uptime-kuma`/`alertmanager`/`vaultwarden.{{
base_domain }}` Caddy route in the same host's Caddyfile. Alertmanager's
`data/alertmanager.yml` is still a placeholder (`route.receiver` points
at a no-op receiver) — no real webhook/SMTP receiver config exists yet.
Vaultwarden's `ADMIN_TOKEN` needs `vault_vaultwarden_admin_token` added
to `group_vars/all/vault.yml` by hand, same blocker as Trivy's token
below — its container won't actually come up without it.

Wazuh's dashboard got a Caddy route too (`wazuh.{{ base_domain }}`,
cross-host to the `wazuh` host's `app_ip:8443`) — skip-verify TLS to the
backend since its dashboard cert is signed by `wazuh_root_ca_cert`, not
the fleet's own Caddy-trusted CA.

MinIO is still container-files-only — not yet wired into
`containerapps_quadlet_services` on any host.
