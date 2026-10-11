# Typical Procedure

Every command needs `-i inventory/<env>` and, for anything touching
secrets, `--ask-vault-pass`. Validate against `testzone` before
`dangerzone`.

## Bringing up an environment

`plays/testrun.yml` is the canonical order (and `testrun.sh` runs it
against a fixed host list). The steps that matter:

1. `plays/vm/provision_vms.yml` — Tofu clones and boots the VMs.
2. `plays/system/onboarding.yml` — interpreter, hostname, root password,
   the `automation` account, base packages.
3. `plays/apps/dns.yml` — **before anything else touches a non-DNS
   VM**: `roles/common` refuses to configure a VM until dnsguard answers.
4. `plays/system/baseline_packages.yml`, `plays/network/firewall.yml`,
   `plays/network/management_network.yml`, `plays/system/update.yml`
   (`testrun.yml` runs these as the single free-strategy play
   `plays/system/host_baseline.yml`).
5. `plays/apps/mail_server.yml`, `plays/apps/freeipa.yml`, then
   `plays/apps/podman.yml` (all containers).
6. The API-configured apps: `plays/apps/keycloak.yml`,
   `plays/apps/gitea_ldap_auth.yml`.
7. Monitoring and logging: `node_exporter`, `podman_exporter`,
   `logging`, `auditd`, `wazuh_agent`, then `plays/apps/mail.yml`
   (`testrun.yml` runs these as `plays/system/host_agents.yml`).

The merged plays exist so a fast host doesn't wait for the slowest one
at every play boundary. Each step keeps its own tag (e.g. `./testrun.sh
--tags firewall`), but the standalone plays are still what you run on
their own, so keep both in sync when adding a role to either.

FreeIPA client enrollment and SSH policy aren't part of `testrun.yml`;
see [FreeIPA-backed SSH access](#freeipa-backed-ssh-access) below.

## Golden templates

First time per Proxmox cluster: `ansible-galaxy collection install -r
collections/requirements.yml` and the Proxmox API token (see
`docs/VAULT.md`). If `management_mac_prefix` is set (group_vars/vm.yml,
drives the management NIC rename), also enable the "Snippets" content
type on the node's snippet storage (Datacenter → Storage → \<id\> → Edit
→ Content) — neither Tofu nor Ansible does that.

Tofu clones every VM from templates built by:

```
ansible-playbook -i inventory/<env> plays/vm/build_templates_pve.yml
```

That builds only missing templates. Add `-e proxmox_template_rebuild=true`
every week or two to rebuild from a fresh upstream image: the build
bakes in a full package upgrade plus `proxmox_template_bake_packages`, so
a stale template makes each fresh clone's first update slow. Existing
VMs are full clones and unaffected. The bake needs internet egress from
the Proxmox node itself — check that first if a build fails at the bake
step.

## Creating a new VM

1. Add the host to inventory: a `hosts.ini` line (`management_ip`,
   `app_ip`, `proxmox_vmid`, groups) and `host_vars/<name>.yml` with a
   `proxmox_vm` block:

   ```yaml
   proxmox_vm:
     node: turkey        # must be in [proxmox]
     template: rocky-10  # key into proxmox_templates
     cores: 2
     memory: 2048
     # disk_resize: 20G
     # usb_mappings: [meshcore]  # Proxmox USB resource mappings to pass through
   ```

   proxmox-tofu reads this straight from inventory; there's no separate
   Tofu-side list.

2. Clone and boot it, either through `plays/vm/provision_vms.yml -l
   <newhost>` or directly:

   ```
   cd ../proxmox-tofu && scripts/tofu-with-vault-secrets.sh <env> apply
   ```

   Cloud-init brings up both NICs and the `automation` account, so every
   play can reach it over `management_ip` straight away.

3. Run the plays from "Bringing up an environment" with `-l <newhost>`.

4. Set `proxmox_node` in its `host_vars` too, for pre-update snapshots
   (see below).

## VM maintenance

For hosts with `proxmox_node` and `proxmox_vmid` set:

* `plays/system/update.yml` snapshots the VM before updating packages.
  Older ansible-taken snapshots beyond `proxmox_snapshot_retention` (5)
  are pruned.
* On-demand snapshot:

  ```
  ansible-playbook -i inventory/<env> plays/vm/snapshot_vm.yml -l <host>
  ```

* `plays/vm/reboot_vms.yml` reboots through Proxmox, one host at a time,
  so pending hardware changes (e.g. a new balloon device) apply.
* Destroying a VM goes through Tofu with an explicit target (see
  proxmox-tofu's README). There's no confirmation prompt — read the plan.
  Then remove the host's `hosts.ini` line and `host_vars/` file.

  ```
  cd ../proxmox-tofu && scripts/tofu-with-vault-secrets.sh <env> destroy \
    -target='module.vm["<name>"].proxmox_virtual_environment_vm.this'
  ```

`plays/system/update.yml` also keeps the Proxmox nodes themselves in
shape: the no-subscription apt repo, the snippet-upload permissions
proxmox-tofu needs, and KSM tuning.

## Restoring a container host after a rebuild

`roles/backup` (daily restic backups of every container volume into a
per-host repo) isn't wired into any play yet. Once it is, recovery
after a rebuild is:

1. Rebuild and re-run onboarding and `plays/apps/podman.yml`; the backup
   role points at the same per-host repo.
2. Restore each service's data before starting it:

   ```
   restic -r s3:https://.../homelab-backups/<host> restore latest \
     --tag service=<service> --target /
   ```

   Bind mounts restore to their real absolute path; for named volumes,
   check `podman volume inspect --format {{.Mountpoint}}` first.
3. Re-run `plays/apps/podman.yml` to start the services on the restored
   data.

## Moving FreeIPA users between environments

`plays/apps/freeipa_export_users.yml` and `freeipa_import_users.yml` copy
user accounts (identity attributes, SSH keys, group memberships) between
separate FreeIPA realms. It's a one-off copy, not replication, and
passwords can't move (Kerberos keys are realm-specific): each imported
account gets a one-time password it must change at first login.

```
ansible-playbook --ask-vault-pass -i inventory/<source-env> \
  plays/apps/freeipa_export_users.yml -e freeipa_export_file=/path/to/users.json

ansible-playbook --ask-vault-pass -i inventory/<target-env> \
  plays/apps/freeipa_import_users.yml -e freeipa_import_file=/path/to/users.json \
  -e freeipa_import_password="$(openssl rand -base64 18)"
```

Both are safe to re-run. The export file holds names, emails and SSH
keys; its default location is under the gitignored `inventory/`.

## FreeIPA-backed SSH access

Gives personal FreeIPA accounts SSH and sudo across the fleet. The
`automation` account stays outside IPA, so a FreeIPA outage never
blocks Ansible.

1. `plays/network/firewall.yml` applies `host_vars/freeipa.yml`'s rule
   opening LDAP/Kerberos to the whole app network.
2. `plays/apps/freeipa_client.yml` enrolls every `[vm]` host except
   `freeipa` and `dns` (SSSD, plus sshd serving each user's
   `ipasshpubkey` and refusing passwords). Proxmox nodes are left out.
   Enrolled hosts use their FQDN as hostname; `roles/common` keeps it
   that way.
3. `plays/apps/freeipa_ssh_access.yml` creates the accounts in
   `freeipa_admin_users` (`host_vars/freeipa.yml`) and a `fleet-admins`
   group, replaces IPA's allow-all HBAC rule with SSH for `fleet-admins`
   on the `freeipa-server` and `fleet-vms` hostgroups, and grants sudo
   with a password (no NOPASSWD). Re-run it after enrolling new hosts.

```
ansible-playbook --ask-vault-pass -i inventory/<env> plays/apps/freeipa_client.yml
ansible-playbook --ask-vault-pass -i inventory/<env> plays/apps/freeipa_ssh_access.yml
```

Each admin's initial password goes in the vault
(`vault_freeipa_<uid>_password`, see `docs/VAULT.md`) and is referenced
from their `freeipa_admin_users` entry. FreeIPA forces a change at first
login.
