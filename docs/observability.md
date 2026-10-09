# Observability

All monitoring and logging services live in `container-files` and are
deployed to whichever `[containers]` hosts need them via
`ansible-playbooks`' `roles/containerapps`, same as every other
service — nothing observability-specific about the deploy path itself.

## Metrics

- **Grafana** (`container-files/grafana`) — dashboards.
- **VictoriaMetrics** (`container-files/victoriametrics`) —
  Prometheus-remote-write-compatible time-series store; the primary
  metrics backend.
- **InfluxDB** (`container-files/influxdb`) — a second time-series
  store, used where something specifically needs it.
- **`node-exporter`** / **`podman-exporter`**
  (`container-files/node-exporter`, `podman-exporter`) — host and
  Podman-level Prometheus exporters. `ansible-playbooks`' own
  `node_exporter`/`podman_exporter` roles handle the non-containerized
  install path for hosts that need exporters without the full
  container stack. node_exporter runs with its systemd collector on
  (`node_exporter_systemd_collector`), so failed units show up too.
- **`blackbox-exporter`** (`container-files/blackbox-exporter`) — runs
  on metrics1 and probes services from the outside: HTTPS to every
  Caddy vhost (cert verified against the fleet's root CA, expiry
  tracked), TCP to non-HTTP ports (SMTP, IMAPS, LDAPS, Kerberos, Gitea
  SSH), and an SOA query against each `[dns]` server.
- **Application metrics**, scraped over each host's `app_ip` with a
  `firewall_rules` entry opening the port to metrics1 alone: Caddy
  (`:9180`, per-vhost requests/status/latency), Gitea (`/metrics` on
  `:3000`, hidden from its public vhost), Keycloak (management port
  `:9000`, including login success/failure counts), plus Loki, promtail,
  Grafana, VictoriaMetrics and vmagent on metrics1 itself.

### What gets scraped

vmagent's config lives in `host_vars/metrics1.yml`
(`vmagent-config.yaml` in `containerapps_secret_files`). The exporter
targets are generated from the inventory, not listed by hand:
node-exporter on every host in `all`, podman-exporter on
`[containers]` + `[apphost]` (the same groups the exporter plays run
against), minus `vmagent_scrape_exclude`. Probe targets are the
`vmagent_probe_http` (Caddy vhost names) and `vmagent_probe_tcp` lists
in the same file. Add a vhost to the Caddyfile, add it there too.

Firewalls: exporter scrapes go to each host's management IP, which
lands in the management interface's firewalld zone, so the exporter
roles open their ports there themselves. Everything else (app metrics,
syslog) goes over `app_ip` and uses ordinary `firewall_rules`.

## Logging

- **Loki** + **Promtail** (`container-files/loki`,
  `container-files/promtail`) — log aggregation and shipping.
- **`syslog`** (`container-files/syslog`) — a central rsyslog receiver
  for the fleet, for anything not shipping through Promtail.
- `ansible-playbooks`' `logging` role wires up the fleet-wide logging
  setup on the Ansible side (what ships where). Every host's rsyslog
  forwards to `syslog_server_host`, metrics1's **`app_ip`**: metrics1
  only accepts 514/tcp from `app_subnet`, so pointing it at the
  management IP gets every message rejected and queued on the sender
  forever. The Monitoring health dashboard's "Log shipping by host"
  table shows which hosts are getting through.

## Dashboards

Grafana's dashboards are provisioned from files, not built in the UI:
`container-files/grafana/dashboards/*.json`, loaded by
`grafana/provisioning/dashboards/homebase.yaml` into a "Homebase"
folder. The JSON in git is authoritative (`allowUiUpdates: false`): to
change one, edit it in the UI, export the JSON over the file, and
commit.

- **Fleet overview** — every host vmagent scrapes: up/down, CPU,
  memory, root disk, load, failing services and systemd units, plus
  containers that aren't running or are failing their healthcheck.
  Host names link to Host detail.
- **Host detail** — one host (`$instance`): CPU by mode, memory, swap,
  filesystems with a time-until-full estimate, disk and network I/O,
  its containers and failed systemd units, and its logs.
- **Services** — is each service reachable the way users reach it:
  every probe's status, HTTP code, response time and cert expiry, a
  probe history timeline, Caddy traffic/5xx/latency per vhost, and
  Gitea/Keycloak basics.
- **Monitoring health** — the monitoring stack itself: every scrape
  target with its status, which hosts are (and aren't) shipping logs,
  VictoriaMetrics ingestion/series/disk, vmagent's remote-write
  backlog, Loki ingestion.
- **Security** — from syslog and Keycloak: SSH failures (by source and
  username) and logins, sudo use (Ansible's become runs counted
  separately), Keycloak login failures by reason, and an auth log.
- **Syslog explorer** — Loki's `job="syslog"` stream filtered by host,
  program and a free-text regex.

The dashboards depend on vmagent's job names (`node-exporter`,
`podman-exporter`, `blackbox-*`, `caddy`, `gitea`, `keycloak`, …) and
the per-target `instance` label being the inventory hostname, both set
in `host_vars/metrics1.yml`'s `vmagent-config.yaml`. The log panels
match syslog's `hostname` label against that same name.

## Where to look first

For "is X up / what does X look like right now," Grafana is the
front door. For chasing a specific incident across hosts, Loki (via
Grafana's Explore view) plus `journalctl`/`podman logs` on the host
itself — the CI-specific debugging steps in
`ansible-playbooks/docs/GITEA_ACTIONS.md` (SELinux `ausearch`, sshd
logs) are a good model for how to dig past the containerized services
into the host layer when the dashboards alone aren't enough.
