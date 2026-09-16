#!/usr/bin/env bash
# ansible.cfg/env-configured vault_password_file target: Ansible runs this
# executable and uses its stdout as the vault password (see ansible.cfg's
# vault_password_file comment). Reads VAULT_PASS rather than holding a
# password itself -- this file has no secret in it and is safe to commit --
# so whatever exported VAULT_PASS (testrun.sh) controls the actual value.
set -euo pipefail

if [[ -z "${VAULT_PASS:-}" ]]; then
    echo "VAULT_PASS is not set -- this script is meant to be run as" \
         "ANSIBLE_VAULT_PASSWORD_FILE from testrun.sh, not directly." >&2
    exit 1
fi

printf '%s' "$VAULT_PASS"
