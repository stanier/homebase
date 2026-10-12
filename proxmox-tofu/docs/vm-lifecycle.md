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
   "Creating a new VM." Tofu doesn't touch inventory.

## Destroying a VM

```
scripts/tofu-with-vault-secrets.sh <env> destroy \
  -target='module.vm["<name>"].proxmox_virtual_environment_vm.this'
```

Then remove its `hosts.ini` line / `host_vars/` file in
`ansible-playbooks`.

**No `confirm_delete`-style safety check exists here yet** — `-target`
plus reading the plan output carefully is the only guard right now.
**Never run a bare `destroy` (no `-target`) against a shared
environment's state.**
