# container-files

Podman [Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit definitions and config for every containerized service in the
homelab — one subdirectory per service. This repo has **no deployment
logic of its own**: `../ansible-playbooks`' `roles/containerapps`
(invoked by `plays/apps/podman.yml`) clones or rsyncs this whole tree
onto each `[containers]` host and turns each named subdirectory into a
systemd unit.

Read `README.md` for the full layout, the service table, and networking
conventions before making changes.

## Conventions

- Each service subdirectory holds a `<service>.container` Quadlet unit,
  optionally a `Containerfile` + `<service>.build` for services with a
  custom image, and optionally `data/` for bind-mounted config.
- This repo is config, not code — there's nothing to build or run here
  directly. Changes take effect on the next `plays/apps/podman.yml` run
  in `../ansible-playbooks`, which re-syncs the tree and restarts any
  service whose unit file or bind-mounted config changed.
- `.env` files and a few generated paths (`dns/data/keys/`,
  `grafana/provisioning/datasources/`,
  `adguardhome/data/conf/`) are gitignored — see `.gitignore` and
  `../ansible-playbooks/docs/VAULT.md`'s "container-files secrets".
  Never commit a real secret into a `data/` file; template it via
  Ansible instead, following that doc's pattern.
- Most services use `Network=host`; `authentik` is the deliberate
  exception (shares a Quadlet network so its four containers can
  address each other by `NetworkAlias`). Don't add `Network=host` to a
  new service without checking whether it actually needs a
  privileged/well-known port or loopback access to a native service —
  prefer a dedicated network otherwise.
- A few services (`gitea`'s `/srv/gitea/{data,config}`, `dovecot`'s
  `/srv/dovecot/...`) deliberately bind-mount *outside* the synced tree
  because `containerapps_source=local`'s rsync `--delete` would
  otherwise destroy that data on every deploy. If you add a service
  with persistent state that must survive redeploys, follow this same
  pattern rather than putting it under the synced `data/` directory.
- A host only runs the services listed in its `containerapps_quadlet_services`
  (`host_vars`, in `../ansible-playbooks`) — adding a subdirectory here
  doesn't deploy it anywhere until that list is updated too.

## Before making changes

- Check `README.md`'s service table to see if a service you're touching
  has a companion (e.g. `roundcube` + `dovecot`, or `authentik`'s four
  units) that needs to change in lockstep.
- Check `../ansible-playbooks/docs/VAULT.md` before adding any secret,
  API key, or credential to a service's config.
