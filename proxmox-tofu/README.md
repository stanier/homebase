# proxmox-tofu

OpenTofu replacement for `ansible-playbooks`' VM *lifecycle* steps —
cloning a VM from a golden template, sizing it, wiring up its two NICs
and cloud-init, and powering it on. This is the supported default for
both `testzone` and `dangerzone` now; the original Ansible-native path
(`plays/legacy_create_vm.yml` / `plays/legacy_delete_vm.yml`, i.e.
`roles/hypervisor/tasks/legacy_create_vm.yml` / `legacy_delete_vm.yml`)
still exists only as a fallback — see "Once this was trusted" below.

**Out of scope, deliberately, for now:**
- Building the golden templates themselves
  (`plays/build_proxmox_templates.yml`) — still Ansible. This repo only
  ever clones an existing template vmid.
- Everything after boot — onboarding, config management, snapshots,
  updates. Still Ansible, unchanged. See
  `ansible-playbooks/docs/Typical_Procedure.md`.

Also published as a [Zensical](https://zensical.org) docs site at
`docs.apps.testzone.internal/proxmox-tofu/` (`zensical.toml`,
`Containerfile`, `.gitea/workflows/deploy.yml` — same pattern as
`homebase`'s docs site).

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

`testzone` and `dangerzone` are OpenTofu **workspaces**, not directories --
`environments/locals.tf`'s `pool = terraform.workspace` is the only thing
that varies between them (`turkey`/`homelab` are shared physical nodes, so
everything else is identical). Adding a third environment later needs no
new tracked files here: `tofu workspace new <name>` plus a matching
`inventory/<name>` is the whole story. `scripts/vm-hostvars.py`/
`scripts/proxmox-nodes.py` (invoked by `environments/data.tf`) read
`inventory/<env>` via `ANSIBLE_INVENTORY_ENV`, set automatically by
`scripts/tofu-with-vault-secrets.sh`/`scripts/import.sh`'s own
`<environment>` argument -- never hardcoded, and always kept in sync with
whichever workspace that same argument selects.

State lives outside this repo entirely, next to the inventory it's derived
from: `ansible-playbooks/inventory/.tofu-state/env/<workspace>/terraform.tfstate`
(that whole `inventory/` tree is already gitignored in `ansible-playbooks`,
so nothing extra to ignore). `environments/.terraform.lock.hcl` (the
provider version pin) is the one thing that *is* committed here -- it's
metadata for reproducible `tofu init`, not state.

## Prerequisites

1. Install OpenTofu (`tofu` on `$PATH`).
2. One-time init, pointing the backend at the external state location
   (adjust `ANSIBLE_PLAYBOOKS_DIR` if `ansible-playbooks` isn't a sibling
   checkout):
   ```
   cd environments
   tofu init \
     -backend-config="path=$ANSIBLE_PLAYBOOKS_DIR/inventory/.tofu-state/terraform.tfstate" \
     -backend-config="workspace_dir=$ANSIBLE_PLAYBOOKS_DIR/inventory/.tofu-state/env"
   tofu workspace new testzone
   tofu workspace new dangerzone
   ```
   (`path` is only ever used for the unused `default` workspace -- the
   local backend requires some value there regardless.)
3. The Proxmox API tokens this needs already exist — same
   `ansible@pve`/`ansible-automation` token set up per
   `ansible-playbooks/docs/VAULT.md`'s "Example: the Proxmox API tokens".
   Nothing new to create in Proxmox itself.

## Running it

Always go through the wrapper so secrets come from the vault, not a
tfvars file:

```
scripts/tofu-with-vault-secrets.sh testzone plan
scripts/tofu-with-vault-secrets.sh testzone apply
```

It prompts once for the vault password (same prompt as
`ansible-playbooks/testrun.sh`), exports one `TF_VAR_<alias>_api_token_secret`
per Proxmox node (`node1`, `node2`, ... in that environment's `hosts.ini`
`[proxmox]` group order — `turkey`/`homelab` today, matching
`providers.tf`'s aliases) for that one process, and never writes the
password or the tokens to disk. Assumes `ansible-playbooks` is a sibling
checkout; set `ANSIBLE_PLAYBOOKS_DIR` if yours lives elsewhere.

## Importing the VMs that already exist

Both environments have already been imported (testzone's 9 hosts,
dangerzone's 6 real hosts as of this writing — `authentik`/`glassroom`/
`tail1`/`net2` are declared in dangerzone's inventory but not actually
deployed yet, so nothing to import for them until they're created).
`scripts/import.sh <env>` is still here for onboarding a *new*
environment the same way, or re-running after adding a host to an
already-Tofu-managed one, e.g. after adding a tenth. Every host in
`inventory/<env>/hosts.ini`'s `[vm]` group that already exists as a real
VM needs this (both the VM itself and, since `management_mac_prefix` is
set for both environments, its uploaded mgmt-NIC-rename snippet) before
its first `apply`, or `apply` will try to clone a vmid that's already
taken and fail loudly (which is safe — it just fails, it won't touch the
existing VM — but importing first is cleaner):

```
scripts/import.sh <testzone|dangerzone>
```

One password prompt, then a `tofu import` call per host (VM + snippet,
where the snippet already exists) — see the script for the exact
resource addresses if you need to import just one host by hand instead.
Safe to re-run: importing an address already in state just fails with
"Resource already managed", it won't touch the live VM; a host whose
snippet was never uploaded (predates `management_mac_prefix`, or was
never actually cloned) is reported and skipped rather than aborting the
whole run.

After it finishes, `plan` should come back clean (no changes) for every
host that already existed, and a *create* for anything genuinely not
deployed yet. If an already-existing host shows anything else, that's
`locals.tf` and the live VM disagreeing about something real — reconcile
before applying anything.

**Provider import-ID syntax changes between `bpg/proxmox` releases** —
double check the exact ID format against whatever version `tofu init`
actually pulled (`environments/.terraform.lock.hcl` once it exists)
before trusting the examples above verbatim.

## Adding a new VM

`locals.tf`'s `vms` map isn't a hand-maintained literal -- it's
`jsondecode(data.external.vm_hostvars.result.json)`, read dynamically
from `ansible-playbooks` inventory by `scripts/vm-hostvars.py`. There's
no separate Tofu-side list to keep in sync:

1. Add the new host entirely through inventory -- `hosts.ini`'s `[vm]`
   line (`management_ip`/`app_ip`/`proxmox_vmid`) plus its
   `host_vars/<name>.yml`'s `proxmox_vm` block (node/template/cores/
   memory) -- same as any other host.
2. `scripts/tofu-with-vault-secrets.sh <env> apply`.
3. Everything from here on is unchanged: bring up the management VLAN,
   then `./onboard` — see `docs/Typical_Procedure.md`'s "Creating a new
   VM". Tofu doesn't touch inventory, same as `legacy_create_vm.yml`
   never did.

## Destroying a VM

```
scripts/tofu-with-vault-secrets.sh <env> destroy \
  -target='module.vm_node1["<name>"].proxmox_virtual_environment_vm.this'
```

(`vm_node1`/`vm_node2` match the host's `node_alias` -- see
`scripts/vm-hostvars.py` -- use whichever one it's actually on.)

Then remove its `hosts.ini` line / `host_vars/` file in
`ansible-playbooks`, same as `legacy_delete_vm.yml`'s existing "remember
to also remove the host's inventory entries" note.

**No `confirm_delete`-style safety check exists here yet** — `-target`
plus reading the plan output carefully is the only guard right now. Don't
run a bare `destroy` (no `-target`) against a shared environment's state.

## Status: legacy path demoted, not deleted

Both testzone and dangerzone are fully Tofu-managed now (`dangerzone`'s 6
real hosts were imported and proven out via a full destroy/recreate/
reprovision cycle). `ansible-playbooks`' original Ansible-native path is
renamed rather than removed — `plays/legacy_create_vm.yml` /
`plays/legacy_delete_vm.yml` and `roles/hypervisor/tasks/legacy_create_vm.yml`
/ `legacy_delete_vm.yml` — kept as a fallback (e.g. a Proxmox node not yet
wired into `proxmox-tofu`, or Tofu/its provider being unavailable), but
no longer the documented default; everything else in that role
(`build_templates.yml`, `repo_hygiene.yml`, `snapshot.yml`, `snippet_permissions.yml`)
is unaffected either way. Don't run both tools against the same VM's
lifecycle in parallel: whichever one didn't do the most recent
clone/config change has a stale view of that VM's state.

One gap the legacy path still covers that Tofu's own `destroy` doesn't:
an explicit `confirm_delete` safety check (see "Destroying a VM" above).
