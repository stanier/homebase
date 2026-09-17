---
icon: lucide/terminal
---

# ansible-playbooks

Ansible plays and roles for the homelab fleet: onboarding hosts,
keeping them updated, and deploying everything that isn't
containerized (plus the Ansible side of everything that is — see
[`container-files`](https://docs.apps.testzone.internal/container-files/)).
VM *creation* itself goes through
[`proxmox-tofu`](https://docs.apps.testzone.internal/proxmox-tofu/);
this repo covers everything before and after that. See the
[homebase overview](https://docs.apps.testzone.internal/) for how all
three repos fit together.

Two environments live in `inventory/` — `testzone` (dev/staging) and
`dangerzone` (prod). Every play needs `-i inventory/<env>` explicitly;
there's no default. `inventory/` itself is gitignored, not committed —
it holds real IPs, vmids, and the encrypted vault, and won't exist in a
fresh checkout.

## Layout

```
plays/          entry points, grouped by concern (system/ network/ vm/ apps/)
roles/          the actual task logic, one role per concern
inventory/      hosts.ini + group_vars/host_vars per environment -- gitignored
docs/           this site's source -- read before the roles' own source
scripts/        vault password plumbing, CA generation
collections/    external Galaxy collection pins
```

## Key roles

| Role | Purpose |
|---|---|
| `common` | Baseline every host gets: root password, `automation` service account, root CA trust |
| `boxship` | Shared provisioning base most other roles depend on |
| `hypervisor` | Proxmox: golden template builds, legacy (non-Tofu) VM create/delete, snapshots, repo hygiene |
| `management_network` | Brings up the mgmt VLAN interface/routing on a VM |
| `firewall` | Host firewall rules |
| `containerapps` | Syncs `container-files` onto `[containers]` hosts and runs it as systemd Quadlet units |
| `appdeploy` / `appdeploy_caddy` | Forced-command CI deploy key + Gitea Actions wiring for app repos; Caddy vhost generation |
| `backup` | Daily restic backups of container volumes/bind-mounts, per host |
| `mail_server` / `postfix` | Native mail stack, pairs with `container-files/dovecot` + `roundcube` |
| `logging` | Central logging setup |
| `node_exporter` / `podman_exporter` | Prometheus exporters for non-container-managed hosts |
| `tailscale` | Tailscale enrollment |
| `workstation` | Personal workstation provisioning (not fleet infra) |

## Running things

Standalone (prompts for the vault password):

```
ansible-playbook --ask-vault-pass -i inventory/testzone plays/system/update.yml
```

Or the whole provision → onboard → deploy sequence in one shot, one
vault prompt covering all of it:

```
./testrun.sh
```

(`testrun.sh` targets `dangerzone` against a fixed host list and passes
`containerapps_source=local` so it deploys from a local `container-files`
checkout instead of pulling from Gitea.)

## Read next

- [Typical procedure](Typical_Procedure.md) — creating a new VM
  (Tofu-first, legacy Ansible-only as fallback), VM maintenance/
  snapshots, restoring a container host after a wipe.
- [Vault & secrets](VAULT.md) — Ansible Vault conventions, every secret
  in the fleet and where it's consumed, the `automation` service
  account.
- [Package management](PACKAGE_MANAGEMENT.md) — package manager
  coverage status for the update plays.
- [Gitea Actions](GITEA_ACTIONS.md) — gitea-runner DNS/SELinux
  gotchas, appdeploy CI setup and debugging, referencing private
  action repos from a workflow.
