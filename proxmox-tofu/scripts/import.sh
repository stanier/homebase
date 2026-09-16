#!/usr/bin/env bash
set -euo pipefail

# One-time (per environment): imports every existing [vm] host from
# ansible-playbooks/inventory/<environment>/hosts.ini (and its
# mgmt-NIC-rename snippet, since management_mac_prefix is set for both
# environments today) into that environment's Tofu state, so the first
# `apply` reconciles them instead of trying to re-clone an already-taken
# vmid. Safe to re-run -- `tofu import` on an address already in state
# just fails with "Resource already managed", it won't touch the live VM.
#
# Which module (vm_node1/vm_node2/...) and real node each host imports
# against comes from scripts/vm-hostvars.py itself, not a fixed
# assumption here -- works the same however many hosts live on each
# node, or if that ever changes.
#
# Loads the vault secrets itself (one password prompt for every import in
# this environment) rather than shelling out to tofu-with-vault-secrets.sh
# per import, which would re-prompt every time -- same VAULT_PASS/trap
# pattern, just done once up front here.
#
#   scripts/import.sh testzone
#   scripts/import.sh dangerzone

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <testzone|dangerzone>" >&2
  exit 1
fi

ENVIRONMENT="$1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_DIR="$REPO_ROOT/environments"

if [[ "$ENVIRONMENT" != "testzone" && "$ENVIRONMENT" != "dangerzone" ]]; then
  echo "no such environment: $ENVIRONMENT (expected testzone or dangerzone)" >&2
  exit 1
fi

ANSIBLE_PLAYBOOKS_DIR="${ANSIBLE_PLAYBOOKS_DIR:-$REPO_ROOT/../ansible-playbooks}"
HOSTS_INI="$ANSIBLE_PLAYBOOKS_DIR/inventory/$ENVIRONMENT/hosts.ini"
VAULT_FILE="$ANSIBLE_PLAYBOOKS_DIR/inventory/$ENVIRONMENT/group_vars/all/vault.yml"
VAULT_PASS_SCRIPT="$ANSIBLE_PLAYBOOKS_DIR/scripts/vault_pass_from_env.sh"

if [[ ! -f "$HOSTS_INI" ]]; then
  echo "hosts.ini not found: $HOSTS_INI (set ANSIBLE_PLAYBOOKS_DIR if ansible-playbooks isn't a sibling checkout)" >&2
  exit 1
fi

if [[ ! -f "$VAULT_FILE" ]]; then
  echo "vault file not found: $VAULT_FILE (set ANSIBLE_PLAYBOOKS_DIR if ansible-playbooks isn't a sibling checkout)" >&2
  exit 1
fi

# node1/node2/... in [proxmox]'s file order, same convention as
# scripts/proxmox-nodes.py and scripts/vm-hostvars.py's node_alias.
mapfile -t PROXMOX_NODES < <(awk '/^\[proxmox\]/{f=1;next} /^\[/{f=0} f && NF {print $1}' "$HOSTS_INI")

ANSIBLE_VAULT_BIN="$ANSIBLE_PLAYBOOKS_DIR/venv/bin/ansible-vault"
[[ -x "$ANSIBLE_VAULT_BIN" ]] || ANSIBLE_VAULT_BIN="ansible-vault"

read -rsp 'Vault password: ' VAULT_PASS
echo
export VAULT_PASS
trap 'unset VAULT_PASS' EXIT

DECRYPTED="$("$ANSIBLE_VAULT_BIN" view --vault-password-file="$VAULT_PASS_SCRIPT" "$VAULT_FILE")"

extract_secret() {
  # A sed capture like [^"']* would silently truncate any secret that
  # happens to contain a quote character -- parse the YAML properly
  # instead so that can't happen.
  python3 -c '
import sys, yaml
value = yaml.safe_load(sys.stdin).get(sys.argv[1])
if value is not None:
    print(value)
' "$1" <<<"$DECRYPTED"
}

for i in "${!PROXMOX_NODES[@]}"; do
  node_name="${PROXMOX_NODES[$i]}"
  alias="node$((i + 1))"
  vault_key="vault_${node_name}_proxmox_api_token_secret"
  secret="$(extract_secret "$vault_key")"
  if [[ -z "$secret" ]]; then
    echo "$vault_key not found in $VAULT_FILE" >&2
    exit 1
  fi
  export "TF_VAR_${alias}_api_token_secret=$secret"
done

cd "$ENV_DIR"
tofu workspace select "$ENVIRONMENT"

# Read once up front so re-running this script after a partial/failed
# earlier run (e.g. one host's VM imported but its snippet import
# failed) only imports what's actually missing, instead of
# re-attempting addresses already in state -- `tofu import` itself
# refuses those with a hard "Resource already managed" error, which
# would otherwise abort the whole loop (set -e) partway through the
# remaining hosts. `tofu state list` on an empty/nonexistent state
# prints nothing and exits 0, so this is also safe on a completely
# fresh state.
EXISTING="$(tofu state list 2>/dev/null || true)"

already_imported() {
  grep -Fxq "$1" <<<"$EXISTING"
}

# name node_real node_alias vmid, sourced from vm-hostvars.py so this
# never has to know which node a host is on, or how many hosts there
# are, by itself. ANSIBLE_INVENTORY_ENV picks which inventory/<env>
# vm-hostvars.py reads -- without it, it'd default to testzone's
# regardless of which environment this script was invoked for, same
# environment-mixup ANSIBLE_INVENTORY_ENV exists everywhere else to avoid.
VM_HOSTVARS="$(ANSIBLE_PLAYBOOKS_DIR="$ANSIBLE_PLAYBOOKS_DIR" ANSIBLE_INVENTORY_ENV="$ENVIRONMENT" "$SCRIPT_DIR/vm-hostvars.py" <<<'{}')"
mapfile -t HOSTS < <(VM_HOSTVARS="$VM_HOSTVARS" python3 <<'PYEOF'
import json, os
data = json.loads(json.loads(os.environ["VM_HOSTVARS"])["json"])
for name, vm in data.items():
    print(f"{name} {vm['node']} {vm['node_alias']} {vm['vmid']}")
PYEOF
)

NOT_DEPLOYED=()
NO_SNIPPET=()

for entry in "${HOSTS[@]}"; do
  read -r name node_real node_alias vmid <<<"$entry"

  vm_addr="module.vm_${node_alias}[\"$name\"].proxmox_virtual_environment_vm.this"
  snippet_addr="module.vm_${node_alias}[\"$name\"].proxmox_virtual_environment_file.mgmt_rename[0]"

  echo "=== $name (vmid $vmid, $node_real) ===" >&2

  vm_present=true
  if already_imported "$vm_addr"; then
    echo "  vm: already in state, skipping" >&2
  else
    # Not every host in hosts.ini's [vm] group has actually been cloned
    # yet -- inventory can list a host before create_vm.yml (or now,
    # `tofu apply`) has ever run for it. Rather than aborting the whole
    # loop (set -e) the first time that happens, treat "no such vmid on
    # this node" as "not deployed yet, apply will create it" and move
    # on; any other failure (auth, network, wrong vmid typo) still needs
    # a human look, so it's surfaced but doesn't stop the remaining
    # hosts either -- re-run this script after fixing it.
    set +e
    import_output="$(tofu import "$vm_addr" "$node_real/$vmid" 2>&1)"
    import_status=$?
    set -e
    echo "$import_output" >&2
    if [[ $import_status -ne 0 ]]; then
      vm_present=false
      if grep -q "Cannot import non-existent remote object" <<<"$import_output"; then
        echo "  vm: no such vmid on $node_real -- not deployed yet, 'tofu apply' will create it" >&2
      else
        echo "  vm: import failed for an unexpected reason -- see above, needs a look" >&2
      fi
      NOT_DEPLOYED+=("$name")
    fi
  fi

  # A VM that was never cloned can't have an uploaded snippet either --
  # skip it too rather than attempting (and failing) that import as well.
  if ! $vm_present; then
    continue
  fi

  if already_imported "$snippet_addr"; then
    echo "  mgmt-rename snippet: already in state, skipping" >&2
  else
    # A VM created before management_mac_prefix/the mgmt-rename snippet
    # existed (plays/create_vm.yml never re-runs against an
    # already-existing host on its own) has no snippet file to import
    # yet -- same "not deployed yet" treatment as the VM import above:
    # surface it and move on to the next host instead of letting set -e
    # abort the whole run over one host's missing snippet.
    set +e
    snippet_import_output="$(tofu import "$snippet_addr" "$node_real/local:snippets/$name-mgmt-rename.yaml" 2>&1)"
    snippet_import_status=$?
    set -e
    echo "$snippet_import_output" >&2
    if [[ $snippet_import_status -ne 0 ]]; then
      if grep -q "Cannot import non-existent remote object" <<<"$snippet_import_output"; then
        echo "  mgmt-rename snippet: not uploaded yet -- 'tofu apply' will create it (review the plan first: this changes the live VM's vendor_data_file_id)" >&2
        NO_SNIPPET+=("$name")
      else
        echo "  mgmt-rename snippet: import failed for an unexpected reason -- see above, needs a look" >&2
      fi
    fi
  fi
done

echo >&2
echo "Done. Run 'scripts/tofu-with-vault-secrets.sh $ENVIRONMENT plan' next." >&2
if [[ ${#NOT_DEPLOYED[@]} -gt 0 ]]; then
  echo "Expect a *create* for: ${NOT_DEPLOYED[*]} (not deployed yet)." >&2
fi
if [[ ${#NO_SNIPPET[@]} -gt 0 ]]; then
  echo "Expect an in-place update (new mgmt-rename snippet + the VM's" >&2
  echo "vendor_data_file_id pointing at it) for: ${NO_SNIPPET[*]} -- read that" >&2
  echo "part of the plan carefully before applying, these are live VMs." >&2
fi
if [[ ${#NOT_DEPLOYED[@]} -eq 0 && ${#NO_SNIPPET[@]} -eq 0 ]]; then
  echo "It should come back with no changes for all ${#HOSTS[@]} hosts." >&2
fi
