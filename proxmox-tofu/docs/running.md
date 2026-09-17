# Running it

## Prerequisites

1. Install OpenTofu (`tofu` on `$PATH`).
2. One-time init, pointing the backend at the external state location
   (adjust `ANSIBLE_PLAYBOOKS_DIR` if `ansible-playbooks` isn't a
   sibling checkout):
   ```
   cd environments
   tofu init \
     -backend-config="path=$ANSIBLE_PLAYBOOKS_DIR/inventory/.tofu-state/terraform.tfstate" \
     -backend-config="workspace_dir=$ANSIBLE_PLAYBOOKS_DIR/inventory/.tofu-state/env"
   tofu workspace new testzone
   tofu workspace new dangerzone
   ```
   (`path` is only ever used for the unused `default` workspace — the
   local backend requires some value there regardless.)
3. The Proxmox API tokens this needs already exist — same
   `ansible@pve`/`ansible-automation` token set up per
   `ansible-playbooks`'
   [Vault & secrets](https://docs.apps.testzone.internal/ansible-playbooks/VAULT/)
   page's "Example: the Proxmox API tokens". Nothing new to create in
   Proxmox itself.

## Always go through the wrapper

```
scripts/tofu-with-vault-secrets.sh testzone plan
scripts/tofu-with-vault-secrets.sh testzone apply
```

Never run bare `tofu` — it's what pulls API token secrets from the
vault and selects the workspace. A bare `tofu apply` will be missing
credentials or (worse) run against whatever workspace was last
selected. The wrapper prompts once for the vault password (same
prompt as `ansible-playbooks/testrun.sh`), exports one
`TF_VAR_<alias>_api_token_secret` per Proxmox node for that one
process, and never writes the password or the tokens to disk. Assumes
`ansible-playbooks` is a sibling checkout; set
`ANSIBLE_PLAYBOOKS_DIR` if yours lives elsewhere.

## Importing VMs that already exist

`scripts/import.sh <env>` is for onboarding a *new* environment, or
re-running after adding a host to an already-Tofu-managed one. Every
host in `inventory/<env>/hosts.ini`'s `[vm]` group that already exists
as a real VM needs this (both the VM itself and, since
`management_mac_prefix` is set for both environments, its uploaded
mgmt-NIC-rename snippet) before its first `apply`, or `apply` will try
to clone a vmid that's already taken and fail loudly (safe — it just
fails, it won't touch the existing VM — but importing first is
cleaner):

```
scripts/import.sh <testzone|dangerzone>
```

Safe to re-run: importing an address already in state just fails with
"Resource already managed," it won't touch the live VM; a host whose
snippet was never uploaded is reported and skipped rather than
aborting the whole run.

After it finishes, `plan` should come back clean for every host that
already existed, and a *create* for anything genuinely not deployed
yet. If an already-existing host shows anything else, that's
`locals.tf` and the live VM disagreeing about something real —
reconcile before applying anything.

**Provider import-ID syntax changes between `bpg/proxmox` releases** —
double check the exact ID format against whatever version `tofu init`
actually pulled (`environments/.terraform.lock.hcl`) before trusting
examples verbatim.
