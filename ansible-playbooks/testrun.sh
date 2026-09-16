#!/usr/bin/env bash
set -euo pipefail

ANSIBLE_INVENTORY="inventory/dangerzone"
ANSIBLE_HOSTS_ARR=(metrics1 gitea gitea-runner-1 app-host app-proxy container-sandbox)
ANSIBLE_HOSTS=$(IFS=,; echo "${ANSIBLE_HOSTS_ARR[*]}")

TOFU_CLEAN=false
if [[ "${1:-}" == "--clean" ]]; then
    shift
    TOFU_CLEAN=true
fi

# containerapps_local_src/containerapps_source come from inventory's
# hosts.ini [all:vars] -- not hardcoded here, since that path is
# specific to wherever this operator's container-files checkout lives.
ansible-playbook --ask-vault-pass -i "$ANSIBLE_INVENTORY" -l "$ANSIBLE_HOSTS" \
    -e tofu_clean="$TOFU_CLEAN" \
    plays/testrun.yml "$@"
