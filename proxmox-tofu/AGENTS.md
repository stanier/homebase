# proxmox-tofu

OpenTofu module + root config that clones VMs from golden templates,
wires up networking/cloud-init, and boots them. This is the supported
default for VM *creation/deletion* in both `testzone` and `dangerzone`;
everything after boot (onboarding, config management, snapshots,
updates) is still `../ansible-playbooks`.

Read `README.md` in full before running anything here — it covers the
workspace model, state location, and every documented command
(`plan`/`apply`/`import`/`destroy`). It also documents real gaps (no
`confirm_delete`-style safety check on `destroy`); don't assume
protections exist that the README doesn't mention.

## Conventions

- `testzone` and `dangerzone` are OpenTofu **workspaces**, not
  directories — there's one `environments/` root config shared by both.
  Always confirm which workspace is selected (`tofu workspace show`)
  before `plan`/`apply`/`destroy`.
- **Always run through `scripts/tofu-with-vault-secrets.sh <env> <cmd>`**,
  never bare `tofu` — it's what pulls API token secrets from
  `../ansible-playbooks`' vault and selects the workspace. A bare `tofu
  apply` will be missing credentials or (worse) run against whatever
  workspace was last selected.
- `locals.tf`'s `vms` map is generated from `../ansible-playbooks`
  inventory via `scripts/vm-hostvars.py` — it is not a hand-maintained
  list. To add or remove a VM, edit inventory (`hosts.ini` +
  `host_vars/<name>.yml`) in `ansible-playbooks`, not a `.tf` file here.
- State lives **outside this repo**, under
  `ansible-playbooks/inventory/.tofu-state/...` (already gitignored
  there). Only `environments/.terraform.lock.hcl` (the provider version
  pin) is committed here.
- Never run a bare `destroy` (no `-target`) against a shared
  environment's state — there's no safety check preventing it from
  taking out every VM in that workspace. Always scope with `-target`
  and read the plan output first.
- Don't run this tool and the legacy Ansible-native path
  (`plays/legacy_create_vm.yml`/`legacy_delete_vm.yml` in
  `ansible-playbooks`) against the same VM in parallel — whichever one
  didn't make the most recent change has a stale view of that VM's
  state.
- Provider (`bpg/proxmox`) import-ID syntax has changed between
  releases — check `environments/.terraform.lock.hcl` for the actual
  pinned version before trusting README import examples verbatim.

## Before making changes

- New VM: edit inventory in `../ansible-playbooks`, not here — see
  README's "Adding a new VM".
- Deleting a VM: use `-target`, then remove the host's inventory entries
  in `../ansible-playbooks` afterward — see README's "Destroying a VM".
- A host that already exists as a real VM needs `scripts/import.sh
  <env>` before its first `apply`, or `apply` will fail trying to clone
  an already-taken vmid.
