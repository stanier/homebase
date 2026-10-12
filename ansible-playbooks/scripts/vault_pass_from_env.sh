#!/usr/bin/env bash
# Vault password "file" for --vault-password-file /
# ANSIBLE_VAULT_PASSWORD_FILE: prints $VAULT_PASS, so the password itself
# never touches disk. Used by proxmox-tofu's wrapper scripts and the
# installer.
set -euo pipefail

if [[ -z "${VAULT_PASS:-}" ]]; then
    echo "VAULT_PASS is not set -- this script is meant to be run as" \
         "a vault password file (ANSIBLE_VAULT_PASSWORD_FILE), not directly." >&2
    exit 1
fi

printf '%s' "$VAULT_PASS"
