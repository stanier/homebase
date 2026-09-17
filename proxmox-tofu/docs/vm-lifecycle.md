# VM lifecycle

## Adding a new VM

`locals.tf`'s `vms` map isn't a hand-maintained literal — it's read
dynamically from `ansible-playbooks` inventory by
`scripts/vm-hostvars.py`. There's no separate Tofu-side list to keep
in sync:

1. Add the new host entirely through inventory — `hosts.ini`'s `[vm]`
   line (`management_ip`/`app_ip`/`proxmox_vmid`) plus its
   `host_vars/<name>.yml`'s `proxmox_vm` block (node/template/cores/
   memory) — same as any other host.
2. `scripts/tofu-with-vault-secrets.sh <env> apply`.
3. Everything from here on is unchanged: bring up the management VLAN,
   then `./onboard` — see `ansible-playbooks`'
   [Typical procedure](https://docs.apps.testzone.internal/ansible-playbooks/Typical_Procedure/)
   "Creating a new VM." Tofu doesn't touch inventory, same as
   `legacy_create_vm.yml` never did.

## Destroying a VM

```
scripts/tofu-with-vault-secrets.sh <env> destroy \
  -target='module.vm_node1["<name>"].proxmox_virtual_environment_vm.this'
```

(`vm_node1`/`vm_node2` match the host's `node_alias` — see
`scripts/vm-hostvars.py` — use whichever one it's actually on.)

Then remove its `hosts.ini` line / `host_vars/` file in
`ansible-playbooks`.

**No `confirm_delete`-style safety check exists here yet** — `-target`
plus reading the plan output carefully is the only guard right now.
**Never run a bare `destroy` (no `-target`) against a shared
environment's state.**

## Status: legacy path demoted, not deleted

Both `testzone` and `dangerzone` are fully Tofu-managed
(`dangerzone`'s 6 real hosts were imported and proven out via a full
destroy/recreate/reprovision cycle). `ansible-playbooks`' original
Ansible-native path is renamed rather than removed —
`plays/legacy_create_vm.yml` / `plays/legacy_delete_vm.yml` and
`roles/hypervisor/tasks/legacy_create_vm.yml` /
`legacy_delete_vm.yml` — kept as a fallback (e.g. a Proxmox node not
yet wired into this repo, or Tofu/its provider being unavailable), but
no longer the documented default. Don't run both tools against the
same VM's lifecycle in parallel: whichever one didn't do the most
recent clone/config change has a stale view of that VM's state.

One gap the legacy path still covers that Tofu's own `destroy`
doesn't: an explicit `confirm_delete` safety check.
