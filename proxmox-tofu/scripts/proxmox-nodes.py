#!/usr/bin/env python3
"""Terraform `external` data source program shared by every
environments/<env>/providers.tf.

Reads hosts.ini's [proxmox] group (ansible-playbooks/inventory/<env>,
ANSIBLE_INVENTORY_ENV -- defaults to "testzone", see vm-hostvars.py's own
comment) and hands back each node's real name and API/SSH address, keyed
by a generic "node1"/"node2"/... alias assigned in [proxmox] group
order -- the same aliases scripts/vm-hostvars.py assigns each VM's
`node_alias`, and the same ones providers.tf's provider blocks use. That
keeps every actual Proxmox node name out of the .tf files entirely:
providers.tf just picks data.external.proxmox_nodes.result["node1"],
never the node's real name.

Also reads each node's effective ansible_user (required) and
ansible_ssh_private_key_file (optional, defaults to the same
~/.ssh/id_ed25519 onboarding itself uses) -- the operator account
providers.tf's ssh block connects to each node as.

Parses hosts.ini/group_vars/host_vars directly (plain YAML/text, no
templating) rather than shelling out to `ansible-inventory --list`.
This script used to do exactly that hand-rolled parsing and it silently
fell behind a real inventory refactor once already (ansible_user moving
from hosts.ini's inline [proxmox:vars] into group_vars/proxmox/ broke
it, because it hardcoded *where* that var lived, not because it avoided
Ansible's resolver as such). Switching to `ansible-inventory --list`
fixed that but introduced a worse problem: it eagerly renders *every*
host's vars, including other hosts' Jinja references to
group_vars/all/vault.yml secrets that have nothing to do with what this
script reads (e.g. host_vars/turkey.yml's own
proxmox_api_token_secret), so it always demands a vault password even
though nothing this script actually wants is secret. This version keeps
`ansible-inventory`'s fix (discover group_vars/proxmox/*.yml by glob
instead of assuming a fixed file) without its cost (never touches
group_vars/all at all, so no vault password needed).

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<alias -> {name, host, ssh_user,
ssh_private_key_file} as a JSON string>"} -- same nested-string
convention as vm-hostvars.py, for the same reason (`external` only
allows string values in its result).
"""
import json
import os
import re
import shlex
import sys
from pathlib import Path

import yaml


def parse_hosts_ini(hosts_ini: Path) -> dict:
    """Returns {group_name: [(hostname, {var: value, ...}), ...]}, in
    file order. Appends to a group across repeated headers instead of
    letting a later one win -- dangerzone's hosts.ini has hit a
    repeated "[vm]" header before, and real Ansible merges rather than
    overwrites in that case."""
    groups: dict = {}
    current = None
    for raw_line in hosts_ini.read_text().splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        header = re.match(r"^\[([^\]:]+)(:vars)?\]$", line)
        if header:
            current = None if header.group(2) else groups.setdefault(header.group(1), [])
            continue
        if current is None:
            continue
        tokens = shlex.split(line)
        name, pairs = tokens[0], tokens[1:]
        current.append((name, dict(pair.split("=", 1) for pair in pairs)))
    return groups


def load_yaml_vars(path: Path) -> dict:
    """group_vars/<group> and host_vars/<host> can each be a single
    <name>.yml file or a directory of *.yml files (Ansible merges every
    file inside a directory, in alphabetical order) -- handle both,
    returning {} if neither exists."""
    if path.is_dir():
        merged = {}
        for f in sorted(path.glob("*.yml")):
            merged.update(yaml.safe_load(f.read_text()) or {})
        return merged
    yml_path = path.with_suffix(".yml")
    if yml_path.is_file():
        return yaml.safe_load(yml_path.read_text()) or {}
    return {}


def effective_hostvars(inventory_dir: Path, group: str, name: str, inline_vars: dict) -> dict:
    """Ansible's real precedence for the vars these scripts touch:
    group_vars/<group> < hosts.ini inline vars < host_vars/<host>.
    group_vars/all is deliberately never consulted -- see this script's
    own docstring."""
    merged = {}
    merged.update(load_yaml_vars(inventory_dir / "group_vars" / group))
    merged.update(inline_vars)
    merged.update(load_yaml_vars(inventory_dir / "host_vars" / name))
    return merged


def main():
    sys.stdin.read()  # discard the query object on stdin

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    ansible_playbooks_dir = Path(
        os.environ.get("ANSIBLE_PLAYBOOKS_DIR", repo_root.parent / "ansible-playbooks")
    )
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "testzone")
    inventory_dir = ansible_playbooks_dir / "inventory" / inventory_env
    hosts_ini = inventory_dir / "hosts.ini"
    if not hosts_ini.is_file():
        print(
            f"hosts.ini not found under {inventory_dir} (set ANSIBLE_PLAYBOOKS_DIR "
            "if ansible-playbooks isn't a sibling checkout)",
            file=sys.stderr,
        )
        sys.exit(1)

    groups = parse_hosts_ini(hosts_ini)
    proxmox_hosts = groups.get("proxmox", [])
    if not proxmox_hosts:
        print(
            f"No hosts in the [proxmox] group for inventory/{inventory_env}",
            file=sys.stderr,
        )
        sys.exit(1)

    result = {}
    for i, (name, inline_vars) in enumerate(proxmox_hosts):
        hv = effective_hostvars(inventory_dir, "proxmox", name, inline_vars)
        if "ansible_user" not in hv:
            print(
                f"{name} (in [proxmox]) has no effective ansible_user -- "
                "providers.tf's ssh block needs one to know who to connect as. "
                "Set it in group_vars/proxmox/ or host_vars.",
                file=sys.stderr,
            )
            sys.exit(1)
        result[f"node{i + 1}"] = {
            "name": name,
            "host": hv["ansible_host"],
            "ssh_user": hv["ansible_user"],
            "ssh_private_key_file": hv.get(
                "ansible_ssh_private_key_file", "~/.ssh/id_ed25519"
            ),
        }

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
