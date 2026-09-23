# Services

| Service | What it is |
|---|---|
| `caddy` | Reverse proxy / TLS termination for every public vhost, using a private offline CA |
| `caddy-l4` | Same as `caddy`, plus a custom `xcaddy` build (`caddy-l4`) that adds layer4 SSH-over-HTTPS routing for appdeploy'd SSH apps — only needed on the fleet's edge proxy (`app-proxy`); everywhere else runs plain `caddy` |
| `dns` (BIND) | Authoritative DNS, RFC2136 dynamic updates for ACME DNS-01 |
| `adguardhome` | Recursive DNS + ad/tracker blocking; also the resolver every VM's app-network DNS points at |
| `alertmanager` | Alert routing/notification (pairs with `victoriametrics`/`grafana`) |
| `authentik` | SSO / identity provider (server + worker + Postgres + Redis) |
| `gitea` | Git hosting |
| `gitea-runner` | Gitea Actions CI runner (`act_runner`) |
| `code-server` | Browser-based VS Code |
| `roundcube` + `dovecot` | Webmail + IMAP/LMTP (pairs with native Postfix — see `ansible-playbooks`' `mail_server` role) |
| `grafana` | Dashboards |
| `influxdb` | Time-series metrics store |
| `victoriametrics` | Time-series metrics store (Prometheus-remote-write compatible) |
| `loki` + `promtail` | Log aggregation + shipping |
| `minio` | Self-hosted S3-compatible object storage |
| `syslog` | Central rsyslog receiver for the fleet |
| `node-exporter` / `podman-exporter` | Prometheus metrics exporters (host / podman) |
| `trivy` | Vulnerability/SBOM scanner (server mode), for `gitea-runner` CI to scan images against |
| `uptime-kuma` | External-facing uptime/status monitoring |
| `vaultwarden` | Self-hosted Bitwarden-compatible password manager |
| `wireguard` (`wg-easy`) | VPN entry point + web UI, for remote access into the homelab |
| `windows` | A full Windows VM-in-a-container (`dockur/windows`), for one-off Windows-only needs |

## Adding a service

1. Create a subdirectory named after the service, with its
   `<service>.container` Quadlet unit (and a `Containerfile` +
   `<service>.build` if it needs a custom image).
2. Decide bind-mounted vs. named-volume state (see
   [Networking](networking.md#state) for the distinction that matters
   here) and, if it needs secrets, template them in via Ansible rather
   than committing anything real to `data/`.
3. Add it to `containerapps_quadlet_services` in the target host's
   `host_vars` in `ansible-playbooks` — nothing here deploys anywhere
   until that list says so.
4. If it needs a public route, that's `roles/appdeploy_caddy` /
   Caddy's config, not this repo.
