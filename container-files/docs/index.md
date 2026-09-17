---
icon: lucide/container
---

# container-files

Podman [Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit definitions and config for every containerized service in the
homelab — one subdirectory per service. This repo has **no deployment
logic of its own**:
[`ansible-playbooks`](https://docs.apps.testzone.internal/ansible-playbooks/)'
`roles/containerapps` (invoked by `plays/apps/podman.yml`) clones or
rsyncs this whole tree onto each `[containers]` host and turns each
named subdirectory into a systemd unit. See that repo's
[Vault & secrets](https://docs.apps.testzone.internal/ansible-playbooks/VAULT/)
page for how secrets reach these services, and see the
[homebase overview](https://docs.apps.testzone.internal/) for how this
repo fits into the wider pipeline.

## Layout

Each service subdirectory holds:

- `<service>.container` — the Quadlet unit (`[Container]`/`[Service]`/
  `[Install]` sections), generated into a real systemd service by
  `podman-systemd` on the target host.
- `Containerfile` + `<service>.build` (only where the service needs a
  custom image, e.g. `caddy`, `dns`, `gitea-runner`, `syslog`) —
  `Image=` in the unit then points at the built image tag instead of a
  registry ref.
- `data/` (where present) — bind-mounted config: static files
  committed here, or ones Ansible templates in at deploy time (secrets,
  host-specific values). `.env` files and a few generated paths
  (`dns/data/keys/`, `grafana/provisioning/datasources/`,
  `adguardhome/data/conf/`) are gitignored.

A host only runs the services listed for it in
`containerapps_quadlet_services` (its `host_vars`, in
`ansible-playbooks`) — this repo just holds the superset every host
draws its subset from.

Read next: [Services](services.md) for what's here and what each one
does, [Networking](networking.md) for how they talk to each other.

## Making a change

Edit the relevant `.container`/`data/` file and commit. On the next
`plays/apps/podman.yml` run, `roles/containerapps` re-syncs this tree
and restarts any service whose unit file or bind-mounted config
actually changed. There's nothing to run in this repo directly — it's
config, not code.
