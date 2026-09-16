# Typical Procedure

* onboard
* baseline
* logging
* update
* podman
* firewall

## Creating a new VM

First time only, per Proxmox cluster: install the collections this needs
(`ansible-galaxy collection install -r collections/requirements.yml`) and
set up the Proxmox API token (see `docs/VAULT.md`).

If `management_mac_prefix` is set (group_vars/vm.yml -- drives the mgmt
NIC rename to `management_interface`, e.g. mgmt1; see
`roles/hypervisor/defaults/main.yml`), also enable the "Snippets"
content type on the node's `proxmox_snippet_storage` storage first
(Proxmox UI: Datacenter -> Storage -> \<id\> -> Edit -> Content) -- a
one-time change neither Tofu nor Ansible does for you.

Build/refresh golden cloud-init templates -- rare, only needed once per
OS/version (`roles/hypervisor/defaults/main.yml`'s `proxmox_templates`).
Needed either way, regardless of which path below actually provisions
the VM -- both clone from these same templates:

```
ansible-playbook plays/vm/build_proxmox_templates.yml -i inventory/testzone --tags templates
```

Both testzone and dangerzone provision their VMs through `proxmox-tofu`
now (see `../proxmox-tofu/README.md`) -- that's the supported default:

1. Add the new host: a line in that environment's `hosts.ini` (same as
   any other host today -- `management_ip`/`app_ip`, group memberships,
   plus `proxmox_vmid` -- the real Proxmox vmid to give this VM) plus its
   `host_vars/<name>.yml`, including a `proxmox_vm` block for everything
   else about how to build it. Same inventory entries as any other
   host -- `proxmox-tofu/scripts/vm-hostvars.py` reads them the same way
   snapshotting does:

   ```yaml
   proxmox_vm:
     node: turkey          # which Proxmox host (must be in [proxmox])
     template: debian-13   # key into proxmox_templates
     cores: 2
     memory: 2048
     # disk_resize: 20G
   ```

2. Mirror the same values into that environment's
   `environments/<env>/locals.tf` `vms` map (see `proxmox-tofu/README.md`'s
   "Adding a new VM"), then clone and boot it:

   ```
   cd ../proxmox-tofu && scripts/tofu-with-vault-secrets.sh <env> apply
   ```

3. Bring up the management VLAN on it. Every play (`./onboard` included)
   connects via `ansible_host`, which is `management_ip` -- but that
   address isn't live yet: sshd doesn't bind to it and the split-routing
   rules that make replies over it work don't exist until
   `roles/management_network` actually runs. So this first connection
   has to go over `app_ip` instead (the address the VM's only default
   route makes reachable at boot), overriding `ansible_host` for just
   this one run:

   ```
   ansible-playbook plays/network/management_network.yml -i inventory/testzone -l <newhost> \
     -e ansible_host=<newhost's app_ip> --ask-become-pass \
     -e ansible_user=keyton -e ansible_ssh_private_key_file=~/.ssh/id_ed25519
   ```

   From here on, `management_ip` (the inventory default) works for every
   subsequent play against this host, `./onboard` included.

4. Provision it, same as always:

   ```
   ./onboard -i inventory/testzone -l <newhost>
   ```

5. Optional but recommended: add `proxmox_node` to its `host_vars` (see
   "VM maintenance" below) so it's covered by automatic pre-update
   snapshots and can be snapshotted on demand -- `proxmox_vmid` is
   already set from step 1.

### Legacy: creating a VM without Tofu

The original, Ansible-only VM lifecycle (`plays/vm/legacy_create_vm.yml` /
`plays/vm/legacy_delete_vm.yml`, `roles/hypervisor`'s own
`legacy_create_vm.yml`/`legacy_delete_vm.yml` task files) still exists as
a fallback -- e.g. a Proxmox node not yet wired into `proxmox-tofu`, or
Tofu/its provider being unavailable -- but isn't the supported default
anymore now that both testzone and dangerzone are fully Tofu-managed.

1. Add the new host to inventory, same as step 1 above.

2. Clone and boot it from the template:

   ```
   ansible-playbook plays/vm/legacy_create_vm.yml -i inventory/testzone -l <newhost> --tags legacy_create_vm
   ```

3. Bring up the management VLAN and provision it -- same as steps 3-4
   above.

4. Optional but recommended: add `proxmox_node` to its `host_vars`, same
   as step 5 above. `legacy_create_vm.yml` doesn't do this for you --
   it doesn't touch inventory files at all, hence step 1.

## VM maintenance

Any VM -- new or existing, cloned via Tofu or created by hand --
gets this once `proxmox_node` is set in its `host_vars` and `proxmox_vmid`
is set on its `hosts.ini` line (see "Creating a new VM" above, or add
them to an existing host any time by hand from the Proxmox UI):

* `ansible-playbook plays/system/update.yml` automatically snapshots the VM
  first (`roles/hypervisor/tasks/snapshot.yml`), before the normal
  package update runs. Hosts without `proxmox_node`/`proxmox_vmid` set
  just skip this step -- it's not required.
* Snapshot on demand any time, e.g. before a risky manual change:

  ```
  ansible-playbook plays/vm/snapshot_vm.yml -i inventory/testzone -l <host> --tags snapshot
  ```

  Old ansible-taken snapshots beyond `proxmox_snapshot_retention` (5 by
  default) are pruned automatically each time.
* Destroy the VM entirely -- for a Tofu-managed host (both environments,
  today), target it explicitly (see `proxmox-tofu/README.md`'s
  "Destroying a VM"):

  ```
  cd ../proxmox-tofu && scripts/tofu-with-vault-secrets.sh <env> destroy \
    -target='module.vm_node1["<name>"].proxmox_virtual_environment_vm.this'
  ```

  Unlike the legacy path below, this has no `confirm_delete`-style safety
  check yet -- `-target` plus reading the plan output carefully is the
  only guard. Remember to also remove the host's `hosts.ini` line,
  `host_vars/` file, and its entry from `environments/<env>/locals.tf`'s
  `vms` map.

  Legacy fallback (stops the VM first if still running, and clears it
  out of any backup/replication job configs) -- irreversible short of a
  pre-existing snapshot, so it requires an explicit confirmation
  matching the target host:

  ```
  ansible-playbook plays/vm/legacy_delete_vm.yml -i inventory/testzone -l <host> --tags legacy_delete_vm -e confirm_delete=<host>
  ```

  Remember to also remove the host's `hosts.ini` line and `host_vars/`
  file, and drop it from any group memberships -- this only destroys the
  Proxmox VM, it doesn't touch inventory (same as `legacy_create_vm.yml` not
  adding it in the first place).

Separately, `plays/system/update.yml` also fixes up the Proxmox nodes themselves
(`roles/hypervisor/tasks/repo_hygiene.yml`, `[proxmox]` hosts only):
Proxmox VE ships pointed at the paywalled enterprise apt repo by default,
which 401s without a paid subscription and breaks `apt update` on
turkey/homelab. This switches them to the free no-subscription repo, and
self-heals on every run if it's ever reverted.

### Restoring a container host's data after a wipe/rebuild

`roles/backup` (applied to every `[containers]` host by `plays/apps/podman.yml`)
takes a daily restic backup of every volume/bind-mount belonging to
whatever `roles/containerapps` has actually deployed there -- see that
role's own comments for how it discovers what to back up. Each host has
its own restic repository (keyed by hostname), so recovering after a full
VM wipe-and-rebuild is:

1. Rebuild the VM and re-run onboarding/`plays/apps/podman.yml` as normal --
   this brings `roles/backup` back up too, pointed at the *same*
   per-host repo path (it's derived from `inventory_hostname`, which
   doesn't change across a rebuild).
2. Restore each service's data before starting it back up, e.g.:

   ```
   restic -r s3:https://.../homelab-backups/<host> restore latest \
     --tag service=<service> --target /
   ```

   (`--target /` because the backed-up paths are already absolute --
   bind mounts restore straight back to their real path;
   `roles/containerapps`-created named volumes need their `podman volume
   inspect --format {{.Mountpoint}}` path checked first if the volume
   didn't already exist with the same name pre-rebuild.)
3. Re-run `plays/apps/podman.yml` once more so `roles/containerapps` starts
   the quadlet units on top of the now-restored data.

List what's actually in a host's repo, or restore just one snapshot,
with `restic -r <repo> snapshots` / `restic -r <repo> restore <snapshot-id> ...`.
`app-host`'s CI-deployed apps aren't covered by this -- see
`roles/backup`'s own notes on why that's out of scope for now.