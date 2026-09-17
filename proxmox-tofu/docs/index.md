---
icon: lucide/server-cog
---

# proxmox-tofu

OpenTofu replacement for
[`ansible-playbooks`](https://docs.apps.testzone.internal/ansible-playbooks/)'
VM *lifecycle* steps — cloning a VM from a golden template, sizing it,
wiring up its two NICs and cloud-init, and powering it on. This is the
supported default for both `testzone` and `dangerzone`; the original
Ansible-native path (`plays/legacy_create_vm.yml` /
`plays/legacy_delete_vm.yml`) still exists only as a fallback. See the
[homebase overview](https://docs.apps.testzone.internal/) for how this
fits into the wider provision → onboard → deploy pipeline.

**Out of scope, deliberately, for now:**

- Building the golden templates themselves
  (`plays/build_proxmox_templates.yml`) — still Ansible. This repo only
  ever clones an existing template vmid.
- Everything after boot — onboarding, config management, snapshots,
  updates. Still Ansible, unchanged — see `ansible-playbooks`'
  [Typical procedure](https://docs.apps.testzone.internal/ansible-playbooks/Typical_Procedure/).

## Layout

```
modules/proxmox_vm/      reusable module -- one instance = one cloned VM
environments/             the one generic root config, shared by both
                           environments -- no per-environment subdirectory
scripts/tofu-with-vault-secrets.sh   pulls API token secrets out of the
                                      existing ansible-vault, selects the
                                      right workspace, and runs tofu
scripts/import.sh                    one-time (per environment) import of
                                      its already-existing VMs into Tofu state
```

`testzone` and `dangerzone` are OpenTofu **workspaces**, not
directories — `environments/locals.tf`'s `pool = terraform.workspace`
is the only thing that varies between them (`turkey`/`homelab` are
shared physical nodes, so everything else is identical). Adding a
third environment later needs no new tracked files here:
`tofu workspace new <name>` plus a matching `inventory/<name>` in
`ansible-playbooks` is the whole story.

State lives outside this repo entirely, next to the inventory it's
derived from:
`ansible-playbooks/inventory/.tofu-state/env/<workspace>/terraform.tfstate`.
`environments/.terraform.lock.hcl` (the provider version pin) is the
one thing that *is* committed here — metadata for reproducible
`tofu init`, not state.

Read next: [Running it](running.md) for prerequisites and the
plan/apply/import wrapper, [VM lifecycle](vm-lifecycle.md) for adding
and destroying a VM.
