#!/usr/bin/env python3
"""Terraform `external` data source program shared by every
environments/<env>/providers.tf.

Reads hosts.ini's [proxmox] group (ansible-playbooks/inventory/<env>,
ANSIBLE_INVENTORY_ENV -- defaults to "testzone", see vm-hostvars.py's own
comment) and hands back each node's real name and API/SSH address, keyed
by a
generic "node1"/"node2"/... alias assigned in file order -- the same
aliases scripts/vm-hostvars.py assigns each VM's `node_alias`, and the
same ones providers.tf's provider blocks use. That keeps every actual
Proxmox node name out of the .tf files entirely: providers.tf just picks
data.external.proxmox_nodes.result["node1"], never the node's real name.

Also reads [proxmox:vars]'s ansible_user (required) and
ansible_ssh_private_key_file (optional, defaults to the same
~/.ssh/id_ed25519 onboarding itself uses) -- the operator account
providers.tf's ssh block connects to each node as. Keeps the operator's
real username out of providers.tf the same way the node names are kept
out: it comes back as each node's own ssh_user/ssh_private_key_file
instead of a literal in the .tf file.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<alias -> {name, host, ssh_user,
ssh_private_key_file} as a JSON string>"} -- same nested-string
convention as vm-hostvars.py, for the same reason (`external` only
allows string values in its result).
"""
import json
import os
import sys
from pathlib import Path


def parse_hosts_ini_group(hosts_ini: Path, group: str) -> dict:
    hosts = {}
    in_group = False
    for line in hosts_ini.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            in_group = line == f"[{group}]"
            continue
        if not in_group:
            continue
        tokens = line.split()
        name, kv_tokens = tokens[0], tokens[1:]
        # See vm-hostvars.py's identical copy of this function for why
        # this merges rather than overwrites -- a repeated group header
        # (dangerzone's hosts.ini does this for "[vm]") must not blank
        # out a host's vars from its first appearance.
        hosts.setdefault(name, {}).update(
            tok.split("=", 1) for tok in kv_tokens
        )
    return hosts


def parse_hosts_ini_group_vars(hosts_ini: Path, group: str) -> dict:
    in_section = False
    group_vars = {}
    for line in hosts_ini.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            in_section = line == f"[{group}:vars]"
            continue
        if not in_section:
            continue
        key, value = line.split("=", 1)
        group_vars[key.strip()] = value.strip().strip("'\"")
    return group_vars


def main():
    sys.stdin.read()  # discard the query object on stdin

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    ansible_playbooks_dir = Path(
        os.environ.get("ANSIBLE_PLAYBOOKS_DIR", repo_root.parent / "ansible-playbooks")
    )
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "testzone")
    hosts_ini = ansible_playbooks_dir / "inventory" / inventory_env / "hosts.ini"

    if not hosts_ini.is_file():
        print(
            f"hosts.ini not found: {hosts_ini} (set ANSIBLE_PLAYBOOKS_DIR if "
            "ansible-playbooks isn't a sibling checkout)",
            file=sys.stderr,
        )
        sys.exit(1)

    nodes = parse_hosts_ini_group(hosts_ini, "proxmox")
    group_vars = parse_hosts_ini_group_vars(hosts_ini, "proxmox")

    if "ansible_user" not in group_vars:
        print(
            f"[proxmox:vars] in {hosts_ini} has no ansible_user -- "
            "providers.tf's ssh block needs one to know who to connect as.",
            file=sys.stderr,
        )
        sys.exit(1)

    ssh_user = group_vars["ansible_user"]
    ssh_private_key_file = group_vars.get(
        "ansible_ssh_private_key_file", "~/.ssh/id_ed25519"
    )

    result = {
        f"node{i + 1}": {
            "name": name,
            "host": inline["ansible_host"],
            "ssh_user": ssh_user,
            "ssh_private_key_file": ssh_private_key_file,
        }
        for i, (name, inline) in enumerate(nodes.items())
    }

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
