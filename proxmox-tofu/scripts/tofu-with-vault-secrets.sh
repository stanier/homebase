#!/usr/bin/env bash
set -euo pipefail

# Runs `tofu` against one environment with its Proxmox API token secrets
# pulled straight out of the *existing* ansible-vault, the same
# vault_<node>_proxmox_api_token_secret entries roles/hypervisor already
# uses (see ansible-playbooks/docs/VAULT.md's "Example: the Proxmox API
# tokens") -- one vault stays the single source of truth for these
# instead of forking a second copy into a tfvars file. Which nodes exist
# and what order they get node1/node2/... aliased in (matching
# providers.tf/scripts/proxmox-nodes.py) both come from this same
# environment's hosts.ini [proxmox] group, not a fixed list here.
#
# "Never written to disk" trick: VAULT_PASS only ever lives in this
# process's own environment, handed to ansible-vault via
# ansible-playbooks' committed, non-secret vault_pass_from_env.sh, and a
# trap clears it on exit either way. ansible-playbooks/testrun.sh now
# runs a single ansible-playbook invocation and just uses
# --ask-vault-pass directly instead of this same trick -- this script is
# still its own separate process, so it still needs it.
#
#   scripts/tofu-with-vault-secrets.sh testzone plan
#   scripts/tofu-with-vault-secrets.sh testzone apply
#   scripts/tofu-with-vault-secrets.sh dangerzone import module.vm_node1.proxmox_virtual_environment_vm.this[\"gitea\"] 5202

if [[ $# -lt 2 ]]; then
    echo "usage: $0 <testzone|dangerzone> <tofu args...>" >&2
    exit 1
fi

ENVIRONMENT="$1"
shift

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

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
if [[ ! -x "$ANSIBLE_VAULT_BIN" ]]; then
    ANSIBLE_VAULT_BIN="ansible-vault"
fi

if [[ -z "${VAULT_PASS:-}" ]]; then
    read -rsp 'Vault password: ' VAULT_PASS
    echo
    export VAULT_PASS
    trap 'unset VAULT_PASS' EXIT
fi

DECRYPTED="$("$ANSIBLE_VAULT_BIN" view --vault-password-file="$VAULT_PASS_SCRIPT" "$VAULT_FILE")"

extract_secret() {
    # vault.yml lines look like: vault_<node>_proxmox_api_token_secret: "..."
    grep -E "^${1}:" <<<"$DECRYPTED" | sed -E "s/^${1}:[[:space:]]*[\"']?([^\"']*)[\"']?[[:space:]]*$/\1/"
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

# Tells scripts/vm-hostvars.py and scripts/proxmox-nodes.py (invoked by
# environments/data.tf as Tofu `external` data sources) which
# inventory/<env> to read -- without this they'd default to testzone's,
# same environment-mixup this wrapper's own ENVIRONMENT arg exists to
# avoid everywhere else.
export ANSIBLE_INVENTORY_ENV="$ENVIRONMENT"

# One generic root config now (environments/, no more environments/<env>/
# subdirectories) -- the workspace, not the directory, is what selects
# testzone vs dangerzone. locals.tf's `pool = terraform.workspace` reads
# this same name back.
cd "$REPO_ROOT/environments"
tofu workspace select "$ENVIRONMENT"
exec tofu "$@"
