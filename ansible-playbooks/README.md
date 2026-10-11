# ansible-playbooks

Ansible plays and roles for the homelab fleet: onboarding hosts, keeping
them updated, and deploying everything that isn't containerized (plus the
Ansible side of everything that is — see `../container-files`). VM
*creation* itself goes through `../proxmox-tofu`; this repo covers
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
| `common` | Baseline for every host: hostname, root password, `automation` account, base packages, package mirror client (dnf baseurls / apt proxy hook, when `package_mirror_url` is set), root CA trust, internal DNS; also package updates |
| `boxship` | The rootless-podman `containers` user every container role builds on |
| `containerapps` | Syncs `container-files` onto `[containers]` hosts and runs it as Quadlet units |
| `appdeploy` / `appdeploy_caddy` | CI deploys of app repos to app-host, and their Caddy routes |
| `hypervisor` | Proxmox: golden templates, snapshots, reboots, node upkeep |
| `management_network` | Moves sshd and firewalld onto a VM's management NIC |
| `firewall` | firewalld rules |
| `freeipa` / `freeipa_client` | Native FreeIPA server; SSSD enrollment for IPA-backed SSH logins |
| `mail_server` / `postfix` | Receiving MTA on mail1 (with the dovecot/roundcube containers); relay satellites everywhere else |
| `logging` / `auditd` / `wazuh_agent` | Remote syslog, audit rules, Wazuh agent enrollment |
| `node_exporter` / `podman_exporter` | Prometheus exporters scraped by metrics1 |
| `ssh_config` | Writes `~/.ssh/config.d/homebase-<env>.conf` on the operator's machine |
| `backup` | Restic backups of container volumes -- not wired into any play yet |
| `workstation` | Empty placeholder |

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

(`testrun.sh` targets `dangerzone` with a fixed host list; pass `--clean`
to destroy and recreate the VMs first. Extra arguments go to
`ansible-playbook`, e.g. `--tags podman`.)

## Docs

Also published as a [Zensical](https://zensical.org) site
(`zensical.toml`, `Containerfile`, `.gitea/workflows/deploy.yml` —
same pattern as `homebase`'s docs site) at
`docs.apps.testzone.internal/ansible-playbooks/`. Start here before
digging into role source:

- [`docs/Typical_Procedure.md`](docs/Typical_Procedure.md) — play order
  for a new environment, golden templates, new VMs, VM maintenance,
  FreeIPA user moves and FreeIPA-backed SSH access.
- [`docs/VAULT.md`](docs/VAULT.md) — Ansible Vault conventions, every
  secret in the fleet and where it's consumed, the `automation` service
  account.
- [`docs/GITEA_ACTIONS.md`](docs/GITEA_ACTIONS.md) — gitea-runner
  (DNS/SELinux gotchas), appdeploy CI setup and debugging, and
  referencing private action repos from a workflow.

See also [`../proxmox-tofu/README.md`](../proxmox-tofu/README.md) for VM
lifecycle and
[`../container-files/README.md`](../container-files/README.md) for what
actually gets deployed by `roles/containerapps`.
