# Architecture

## Hosts and environments

Every host is a VM on one of two physical Proxmox nodes (`turkey`,
`homelab`), split across two environments that exist end to end —
Proxmox resource pool, Ansible inventory group, and Tofu workspace all
share the same two names:

- **`testzone`** — dev/staging
- **`dangerzone`** — prod

Each VM has two NICs: a management interface (Ansible's
`management_ip`, used for provisioning/SSH) and an app-network
interface (`app_ip`, used for actual service traffic). Both are wired
up by `proxmox-tofu` at clone time; `ansible-playbooks`'
`management_network` role brings the mgmt VLAN interface up afterward.

## DNS

Every VM's app-network DNS points at **AdGuardHome**
(`container-files/adguardhome`) rather than DHCP defaults — this was a
fleet-wide gap (VMs falling back to a resolver that couldn't see
internal names/ACME DNS-01 records) fixed across every host's
inventory and template config. `container-files/dns` (BIND) is
authoritative for the zone and handles RFC2136 dynamic updates for
ACME DNS-01 issuance; AdGuardHome forwards to it for internal names and
does ad/tracker blocking + recursive resolution for everything else.

Rootless Podman containers (notably `gitea-runner` and its job
containers) don't inherit the host resolver by default — both the
runner service and the ephemeral job containers it spawns needed an
explicit DNS fix. See `ansible-playbooks/docs/GITEA_ACTIONS.md`.

## Edge and identity

- **Caddy** (`container-files/caddy`) terminates TLS for every public
  vhost, using a private offline CA (root CA trust is distributed to
  every host by the `common` Ansible role). `roles/appdeploy_caddy`
  generates vhost config for deployed apps.
- **Authentik** (`container-files/authentik`) is the SSO/identity
  provider — server + worker + Postgres + Redis, the one service that
  gets its own dedicated Quadlet network (`authentik.network`) instead
  of `Network=host`, so its containers can address each other by
  `NetworkAlias`.

## Git hosting and CI

**Gitea** (`container-files/gitea`) hosts every repo in this family,
including `homebase` itself. **`gitea-runner`**
(`container-files/gitea-runner`, running `act_runner`) executes Gitea
Actions workflows — see [CI/CD](ci-cd.md) for the full push-to-deploy
path.

## Config, state, and secrets

- **Secrets** live in `ansible-playbooks/group_vars/all/vault.yml`,
  encrypted with Ansible Vault — never in `container-files` or
  `proxmox-tofu` directly. See
  `ansible-playbooks/docs/VAULT.md` for the full inventory of what's
  encrypted and where it's consumed.
- **Inventory** (`ansible-playbooks/inventory/`) is the single source
  of truth for which hosts exist — gitignored, not committed, because
  it holds real IPs, vmids, and the vault. `proxmox-tofu`'s VM list
  (`locals.tf`'s `vms` map) is generated *from* this inventory
  (`scripts/vm-hostvars.py`), not maintained separately.
- **Tofu state** lives outside `proxmox-tofu` entirely, alongside the
  inventory it's derived from
  (`ansible-playbooks/inventory/.tofu-state/...`).

## What lives where

| Concern | Repo |
|---|---|
| VM clone/boot/network/cloud-init | `proxmox-tofu` |
| Host onboarding, baseline, updates, secrets, backups | `ansible-playbooks` |
| Deploying containers onto `[containers]` hosts | `ansible-playbooks` (`roles/containerapps`), reading from `container-files` |
| Service definitions (Quadlet units, images, config) | `container-files` |
| Native (non-containerized) mail stack | `ansible-playbooks` (`roles/mail_server`/`postfix`), paired with `container-files/dovecot`+`roundcube` |
| Docs, architecture, cross-repo overview | `homebase` (this repo) |

If a change is about *what a service does or how it's configured*, it
belongs in `container-files`. If it's about *which hosts run what, how
they get there, or fleet-wide policy*, it belongs in
`ansible-playbooks`. If it's about *a VM existing at all*, it belongs
in `proxmox-tofu`.
