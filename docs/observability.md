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
  container stack.

## Logging

- **Loki** + **Promtail** (`container-files/loki`,
  `container-files/promtail`) — log aggregation and shipping.
- **`syslog`** (`container-files/syslog`) — a central rsyslog receiver
  for the fleet, for anything not shipping through Promtail.
- `ansible-playbooks`' `logging` role wires up the fleet-wide logging
  setup on the Ansible side (what ships where).

## Where to look first

For "is X up / what does X look like right now," Grafana is the
front door. For chasing a specific incident across hosts, Loki (via
Grafana's Explore view) plus `journalctl`/`podman logs` on the host
itself — the CI-specific debugging steps in
`ansible-playbooks/docs/GITEA_ACTIONS.md` (SELinux `ausearch`, sshd
logs) are a good model for how to dig past the containerized services
into the host layer when the dashboards alone aren't enough.
