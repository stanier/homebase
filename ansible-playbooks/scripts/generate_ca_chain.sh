#!/usr/bin/env bash
# Generates/maintains the offline root CA and per-environment intermediate
# CAs that back Caddy's internal PKI (see docs/VAULT.md and
# host_vars/container-sandbox.yml's Caddyfile `pki` block in dangerzone and
# testzone).
#
# Deliberately NOT an Ansible task: the whole point of an "offline" root is
# that its private key never touches a host or a git repo, so it only ever
# lives in $CA_DIR below (default ~/.homelab-ca, outside this checkout) and
# this script only ever runs by hand on an operator's own machine.
#
# Usage:
#   scripts/generate_ca_chain.sh root                  # one-time, or to rotate the root
#   scripts/generate_ca_chain.sh intermediate <env>     # <env> = dangerzone | testzone
#
# After generating an intermediate, this prints the exact commands to
# vault-encrypt its key and where the (non-secret) cert and key need to be
# wired into that environment's Ansible vars -- it does not touch this repo
# or the Ansible vault itself, since that requires the vault password,
# which this script never asks for or handles.
set -euo pipefail

CA_DIR="${CA_DIR:-$HOME/.homelab-ca}"
ROOT_KEY="$CA_DIR/root.key"
ROOT_CERT="$CA_DIR/root-ca.crt"
ROOT_CN="Homelab Offline Root CA"
ROOT_DAYS=3650
INTERMEDIATE_DAYS=1825

usage() {
    echo "Usage: $0 root" >&2
    echo "       $0 intermediate <dangerzone|testzone>" >&2
    exit 1
}

gen_root() {
    mkdir -p "$CA_DIR"
    chmod 700 "$CA_DIR"

    if [[ -f "$ROOT_KEY" ]]; then
        echo "Root key already exists at $ROOT_KEY -- refusing to overwrite." >&2
        echo "Delete it yourself first if you really intend to rotate the root" \
             "(this reissues trust for every host in the fleet)." >&2
        exit 1
    fi

    openssl ecparam -name prime256v1 -genkey -noout -out "$ROOT_KEY"
    chmod 600 "$ROOT_KEY"

    openssl req -x509 -new -key "$ROOT_KEY" -sha256 -days "$ROOT_DAYS" \
        -subj "/CN=$ROOT_CN" \
        -addext "basicConstraints=critical,CA:TRUE,pathlen:1" \
        -addext "keyUsage=critical,keyCertSign,cRLSign" \
        -addext "subjectKeyIdentifier=hash" \
        -out "$ROOT_CERT"

    echo "Offline root CA generated:"
    echo "  key:  $ROOT_KEY   (never leaves this machine -- do not commit, copy to a host, or vault-encrypt into the repo)"
    echo "  cert: $ROOT_CERT  (public -- copy it to replace all of:"
    echo "                       inventory/dangerzone/group_vars/files/root-ca.crt"
    echo "                       inventory/testzone/group_vars/files/root-ca.crt"
    echo "                       container-files/caddy/data/root-ca.crt"
    echo "                       container-files/caddy-l4/data/root-ca.crt"
    echo "                       container-files/gitea-runner/data/root-ca.crt)"
}

gen_intermediate() {
    local env="$1"
    case "$env" in
        dangerzone|testzone) ;;
        *) usage ;;
    esac

    if [[ ! -f "$ROOT_KEY" || ! -f "$ROOT_CERT" ]]; then
        echo "No offline root found at $CA_DIR -- run '$0 root' first." >&2
        exit 1
    fi

    local key="$CA_DIR/$env-intermediate.key"
    local csr="$CA_DIR/$env-intermediate.csr"
    local cert="$CA_DIR/$env-intermediate.crt"

    if [[ -f "$key" ]]; then
        echo "Intermediate key already exists at $key -- refusing to overwrite." >&2
        echo "Delete it yourself first if you really intend to rotate this environment's intermediate." >&2
        exit 1
    fi

    openssl ecparam -name prime256v1 -genkey -noout -out "$key"
    chmod 600 "$key"

    openssl req -new -key "$key" -sha256 \
        -subj "/CN=Homelab $env Intermediate CA" \
        -out "$csr"

    openssl x509 -req -in "$csr" -CA "$ROOT_CERT" -CAkey "$ROOT_KEY" -CAcreateserial \
        -days "$INTERMEDIATE_DAYS" -sha256 \
        -extfile <(printf 'basicConstraints=critical,CA:TRUE,pathlen:0\nkeyUsage=critical,keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid:always\n') \
        -out "$cert"
    rm -f "$csr"

    echo "Intermediate CA generated for $env:"
    echo "  key:  $key"
    echo "  cert: $cert"
    echo
    echo "Next steps (this script does not touch the repo or the vault):"
    echo "  1. Vault-encrypt the key, from the ansible-playbooks directory:"
    echo "       ansible-vault encrypt_string --vault-password-file scripts/vault_pass_from_env.sh \\"
    echo "         --stdin-name vault_caddy_intermediate_key < '$key'"
    echo "     (needs VAULT_PASS exported the same way testrun.sh does)"
    echo "  2. Paste the resulting block into inventory/$env/group_vars/all/vault.yml"
    echo "  3. Copy '$cert' into inventory/$env/group_vars/files/caddy-intermediate.crt (non-secret, committed --"
    echo "     under group_vars/ specifically, not a sibling files/ dir, so Ansible's directory inventory"
    echo "     loader doesn't try to parse the .crt as an inventory source)"
}

case "${1:-}" in
    root) gen_root ;;
    intermediate) gen_intermediate "${2:-}" ;;
    *) usage ;;
esac
