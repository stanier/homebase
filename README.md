# Homebase

Greenfield homelab infrastructure playbooks and floorplans for reproducible network environments.

## Layout

* [`proxmox-tofu/`](proxmox-tofu/) — OpenTofu modules and per-environment
  configs that provision the VMs/networking on Proxmox VE.
* [`ansible-playbooks/`](ansible-playbooks/) — configures provisioned
  hosts: roles, per-app plays, inventory, and Vault-encrypted secrets.
* [`container-files/`](container-files/) — Containerfiles and Quadlet
  units for every app in the stack.
* [`docs/`](docs/) — source for the Zensical documentation site (system
  architecture, environments, how the three pieces above hand off to
  each other); built and served via the `Containerfile`/`zensical.toml`
  at the repo root. Detail specific to one piece lives in that
  directory's own `docs/` instead.

Start with [`ansible-playbooks/docs/Typical_Procedure.md`](ansible-playbooks/docs/Typical_Procedure.md)
for creating a new VM end to end.

## Requirements

* [OpenTofu](https://opentofu.org) >= 1.7
* Ansible, with the collections in
  [`ansible-playbooks/collections/requirements.yml`](ansible-playbooks/collections/requirements.yml)
  installed (`ansible-galaxy collection install -r collections/requirements.yml`)
* Access to the repo's Ansible Vault password (see
  [`ansible-playbooks/docs/VAULT.md`](ansible-playbooks/docs/VAULT.md))
* A Proxmox VE 9.2 backend to provision against

## Status

Personal homelab infrastructure, built and maintained for my own
environments. Not seeking external contributions, but issues/ideas are
welcome.

Pushes to `main` build this repo's `docs/` with Zensical and deploy it
via the `.gitea/workflows/deploy.yml` pipeline.

## Principles

* Multi-tenant by default
    * Environments are livestock, not pets.
* Owning the entire lifecycle
    * What goes up must eventually come down.
* Stay nimble
    * Don't reject equipables that could assist in your performance, but don't wear heavy garments either.

## Features

* Applications packaged as Quadlet-managed rootless Podman containers where applicable
* Supported guest operating systems:
    * Rocky 10
* Supported backends:
    * Proxmox VE 9.2
* Supported applications:
    * DNS server
    * DNS policy-based resolver (AdGuardHome)
    * SSL-terminating reverse proxy (Caddy)
    * ACME self-signed TLS cert manager
    * Metrics stack
        * Grafana
        * VictoriaMetrics
        * Prometheus
        * Loki
        * PromTail
        * InfluxDB
    * Private internal mail stack
        * Internal-only
        * Postfix
        * Dovecot
        * RoundCube
    * Identity, Authorization and Authentication
        * FreeIPA
        * Keycloak
        * Authentik
    * Software Foundry
        * Gitea
        * Gitea CI Runner
        * Coder
        * AppDeploy
            * Simple Heroku/Dokku-like push-to-deploy
            * Pages-like functionality baked in

## License

[GPLv3](LICENSE)
