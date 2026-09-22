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

None of these are wired into `containerapps_quadlet_services` in
`ansible-playbooks` yet — that's a separate follow-up per service once
its `.container` unit lands here.
