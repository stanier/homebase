# Networking

## `Network=host` is the default

Most services use `Network=host` — either because they need a
privileged/well-known port, or because they need to reach a native
(non-containerized) service over loopback (`dovecot`/`roundcube`
talking to host Postfix; see the comments in those `.container`
files).

`authentik` is the deliberate exception: its four units share a
dedicated `authentik.network` Quadlet network so its containers can
address each other by `NetworkAlias` without publishing internal
ports. Don't reach for `Network=host` on a new service without first
checking whether it actually needs a privileged/well-known port or
loopback access to a native service — prefer a dedicated network
otherwise, same as `authentik`.

## DNS

`adguardhome` is the resolver every VM's app-network DNS is pointed
at (set fleet-wide by `ansible-playbooks`' `common` role, not by
anything in this repo) — see the
[homebase architecture page](https://docs.apps.testzone.internal/architecture/#dns)
for the full picture, including the rootless-Podman DNS quirks that
hit `gitea-runner` specifically.

## State

- Named volumes (`Volume=<service>_<name>:/path`) are Podman-managed
  and host-local.
- Bind mounts under `/srv/containers/container-files/...` come from
  this repo's sync — anything there is wiped and replaced by the next
  `containerapps_source=local` rsync (`--delete`).
- A few services deliberately bind-mount **outside** the synced tree
  because that `--delete` would otherwise destroy persistent data on
  every deploy: `gitea`'s `/srv/gitea/{data,config}`, `dovecot`'s
  `/srv/dovecot/...`. If you add a service with state that must
  survive redeploys, follow this same pattern rather than putting it
  under the synced `data/` directory.
