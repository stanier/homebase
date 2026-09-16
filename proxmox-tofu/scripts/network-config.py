#!/usr/bin/env python3
"""Terraform `external` data source program shared by every
environments/<env>/locals.tf.

Reads the fleet's real network topology straight out of the Ansible
inventory (ansible-playbooks/inventory/<env>, ANSIBLE_INVENTORY_ENV --
defaults to "dangerzone", see vm-hostvars.py's own comment) instead of
it being duplicated as Tofu literals:

  - group_vars/proxmox/main.yml's proxmox_app_bridge/proxmox_app_gateway/
    proxmox_mgmt_bridge -- the real bridge names and app-VLAN gateway on
    turkey/homelab.
  - group_vars/vm.yml's management_mac_prefix/management_interface --
    the locally-administered MAC prefix stamped onto every new VM's
    management NIC, and the guest interface name it gets renamed to.

Same rationale as vm-hostvars.py: parses the plain YAML directly rather
than shelling out to `ansible-inventory --list`, since that also
resolves group_vars/all/vault.yml and would demand a vault password
just to read values that are neither secret nor templated.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<{app_bridge, app_gateway, mgmt_bridge,
management_mac_prefix, management_interface} as a JSON string>"} --
same nested-string convention as vm-hostvars.py, for the same reason
(`external` only allows string values in its result).
"""
import json
import os
import sys
from pathlib import Path

import yaml


def load_yaml(path: Path) -> dict:
    if not path.is_file():
        return {}
    return yaml.safe_load(path.read_text()) or {}


def require(source: dict, key: str, path: Path) -> str:
    if key not in source:
        print(f"{path} has no {key}", file=sys.stderr)
        sys.exit(1)
    return source[key]


def main():
    sys.stdin.read()  # discard the query object on stdin

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    ansible_playbooks_dir = Path(
        os.environ.get("ANSIBLE_PLAYBOOKS_DIR", repo_root.parent / "ansible-playbooks")
    )
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "dangerzone")
    inventory_dir = ansible_playbooks_dir / "inventory" / inventory_env

    proxmox_vars_path = inventory_dir / "group_vars" / "proxmox" / "main.yml"
    vm_vars_path = inventory_dir / "group_vars" / "vm.yml"

    proxmox_vars = load_yaml(proxmox_vars_path)
    vm_vars = load_yaml(vm_vars_path)

    result = {
        "app_bridge": require(proxmox_vars, "proxmox_app_bridge", proxmox_vars_path),
        "app_gateway": require(proxmox_vars, "proxmox_app_gateway", proxmox_vars_path),
        "mgmt_bridge": require(proxmox_vars, "proxmox_mgmt_bridge", proxmox_vars_path),
        "management_mac_prefix": require(
            vm_vars, "management_mac_prefix", vm_vars_path
        ),
        "management_interface": require(
            vm_vars, "management_interface", vm_vars_path
        ),
    }

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
