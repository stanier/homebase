# ansible-playbooks

Ansible plays and roles for the homelab fleet: onboarding hosts, keeping
them updated, and deploying everything that isn't containerized (plus the
Ansible side of everything that is — see `../container-files`). VM
*creation* itself now goes through `../proxmox-tofu`; this repo covers
everything before and after that.

Two environments live in `inventory/` (gitignored — see below):
`testzone` (dev/staging) and `dangerzone` (prod). Every play needs `-i
inventory/<env>` explicitly; there's no default.

## Layout

```
plays/          entry points, grouped by concern (system/ network/ vm/ apps/)
roles/          the actual task logic, one role per concern
inventory/      hosts.ini + group_vars/host_vars per environment -- gitignored,
                not committed (contains real IPs, vmids, and the encrypted vault)
docs/           deeper docs -- read these before the roles' source
scripts/        vault password plumbing, CA generation
collections/    external Galaxy collection pins
```

`inventory/` isn't in this checkout — it's local, per-operator state
(hosts, vault, Tofu's state file all live under it). If you're setting
this up fresh, you'll need to create it yourself (`hosts.ini`,
`group_vars/all/vars.yml` + `vault.yml`, `host_vars/<host>.yml` per host)
following the conventions described in `docs/`.

## Key roles

| Role | Purpose |
|---|---|
| `common` | Baseline every host gets: root password, `automation` service account, root CA trust |
| `boxship` | Shared provisioning base most other roles depend on |
| `hypervisor` | Proxmox: golden template builds, legacy (non-Tofu) VM create/delete, snapshots, repo hygiene |
| `management_network` | Brings up the mgmt VLAN interface/routing on a VM |
| `firewall` | Host firewall rules |
| `containerapps` | Syncs `../container-files` onto `[containers]` hosts and runs it as systemd Quadlet units |
| `appdeploy` / `appdeploy_caddy` | Forced-command CI deploy key + Gitea Actions wiring for app repos; Caddy vhost generation for deployed apps |
| `backup` | Daily restic backups of container volumes/bind-mounts, per host |
| `mail_server` / `postfix` | Native (non-containerized) mail stack, pairs with `container-files/dovecot` + `roundcube` |
| `freeipa` | Native (non-containerized) FreeIPA install -- directory/Kerberos source of truth, DNS deliberately left to AdGuardHome/bind |
| `logging` | Central logging setup |
| `node_exporter` / `podman_exporter` | Prometheus exporters for non-container-managed hosts |
| `tailscale` | Tailscale enrollment |
| `ssh_config` | Generates `~/.ssh/config.d/homebase-<env>.conf` Host entries from inventory (operator's machine, not fleet infra) |
| `workstation` | Personal workstation provisioning (not fleet infra) |

## Running things

Standalone (prompts for the vault password):

```
ansible-playbook --ask-vault-pass -i inventory/testzone plays/system/update.yml
```

Or the whole provision → onboard → deploy sequence in one shot, one vault
prompt covering all of it:

```
./testrun.sh
```

(`testrun.sh` targets `dangerzone` against a fixed host list and passes
`containerapps_source=local` so it deploys from a local
`container-files` checkout instead of pulling from Gitea — see the
script itself.)

## Docs

Also published as a [Zensical](https://zensical.org) site
(`zensical.toml`, `Containerfile`, `.gitea/workflows/deploy.yml` —
same pattern as `homebase`'s docs site) at
`docs.apps.testzone.internal/ansible-playbooks/`. Start here before
digging into role source:

- [`docs/Typical_Procedure.md`](docs/Typical_Procedure.md) — creating a
  new VM (Tofu-first, with the legacy Ansible-only path as fallback), VM
  maintenance/snapshots, restoring a container host after a wipe.
- [`docs/VAULT.md`](docs/VAULT.md) — Ansible Vault conventions, every
  secret in the fleet and where it's consumed, the `automation` service
  account.
- [`docs/PACKAGE_MANAGEMENT.md`](docs/PACKAGE_MANAGEMENT.md) — package
  manager coverage status for the update plays.
- [`docs/GITEA_ACTIONS.md`](docs/GITEA_ACTIONS.md) — gitea-runner
  (DNS/SELinux gotchas), appdeploy CI setup and debugging, and
  referencing private action repos from a workflow.

See also [`../proxmox-tofu/README.md`](../proxmox-tofu/README.md) for VM
lifecycle (the supported default for both environments) and
[`../container-files/README.md`](../container-files/README.md) for what
actually gets deployed by `roles/containerapps`.
