#!/usr/bin/env python3
"""Terraform `external` data source program shared by every
environments/<env>/locals.tf.

Reads each [vm] host's effective config straight out of the Ansible
inventory (ansible-playbooks/inventory/<env>, ANSIBLE_INVENTORY_ENV --
defaults to "testzone" since that's the only environment actually wired
up to run this by hand today, see plays/provision_vms.yml's own comment
for how a Tofu-driven Ansible run passes the real one through) instead of
it being duplicated as Tofu literals:

  - hosts.ini's inline vars (ansible_host, management_ip, app_ip,
    proxmox_vmid) -- unique per host, declared directly on its [vm] line.
  - group_vars/vm.yml's `proxmox_vm` block (node/template/cores/memory/
    disk_resize) and standalone `proxmox_enabled`/`proxmox_protected` --
    the same `proxmox_vm` block roles/hypervisor's create_vm.yml/
    delete_vm.yml already read to clone/destroy VMs the Ansible-native
    way (still live for the dangerzone environment, which hasn't moved
    to proxmox-tofu).
  - host_vars/<name>.yml, for any host that overrides the group default.

Also assigns each host a `node_alias` ("node1", "node2", ...) based on
the order its real Proxmox node name appears in hosts.ini's [proxmox]
group -- providers.tf's provider aliases are the same generic names, so
vms.tf can select a VM's provider/module without either file ever
naming an actual node. The real name (`node`) is still passed through
for the module's own node_name/SSH-node use, which does have to match
Proxmox reality.

This parses the plain YAML/INI directly rather than shelling out to
`ansible-inventory --list`, since that also resolves
group_vars/all/vault.yml and would demand a vault password just to read
values that are neither secret nor templated.

A host_vars override of `proxmox_vm` replaces the whole dict rather than
merging individual keys -- matches Ansible's own hash_behaviour=replace
default (ansible.cfg leaves the `merge` alternative commented out, and
the Ansible project itself recommends against turning it on), so this
script and create_vm.yml never disagree about a host's effective
node/template/cores/memory. `proxmox_enabled`/`proxmox_protected` are
plain scalars for exactly this reason too -- see group_vars/vm.yml's
comment on why they're not nested inside proxmox_vm.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<hostname -> {node, node_alias,
template, cores, memory, disk_resize, enabled, protected, vmid, app_ip,
management_ip} as a JSON string>"} -- `external` only allows string
values in its result, so the real payload is nested one level down and
locals.tf jsondecode()s it back out.
"""
import json
import os
import sys
from pathlib import Path

import yaml


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
        # dangerzone's hosts.ini repeats "[vm]" as a second, separate
        # section later in the file (group-membership-only, no inline
        # vars) -- Ansible's own inventory parser merges repeated group
        # headers like this, so this has to too, or a host listed in
        # both sections would have its first section's proxmox_vmid/
        # app_ip/management_ip silently overwritten by the second,
        # var-less one.
        hosts.setdefault(name, {}).update(
            tok.split("=", 1) for tok in kv_tokens
        )
    return hosts


def load_yaml(path: Path) -> dict:
    if not path.is_file():
        return {}
    return yaml.safe_load(path.read_text()) or {}


def main():
    sys.stdin.read()  # discard the query object on stdin

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    ansible_playbooks_dir = Path(
        os.environ.get("ANSIBLE_PLAYBOOKS_DIR", repo_root.parent / "ansible-playbooks")
    )
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "dangerzone")
    inventory_dir = ansible_playbooks_dir / "inventory" / inventory_env
    hosts_ini = inventory_dir / "hosts.ini"

    if not hosts_ini.is_file():
        print(
            f"hosts.ini not found: {hosts_ini} (set ANSIBLE_PLAYBOOKS_DIR if "
            "ansible-playbooks isn't a sibling checkout)",
            file=sys.stderr,
        )
        sys.exit(1)

    inline_vars = parse_hosts_ini_group(hosts_ini, "vm")
    group_vars = load_yaml(inventory_dir / "group_vars" / "vm.yml")

    node_aliases = {
        node_name: f"node{i + 1}"
        for i, node_name in enumerate(parse_hosts_ini_group(hosts_ini, "proxmox"))
    }

    result = {}
    for name, inline in inline_vars.items():
        host_vars = load_yaml(inventory_dir / "host_vars" / f"{name}.yml")

        vm = host_vars.get("proxmox_vm", group_vars.get("proxmox_vm", {}))

        host = {
            "node": vm["node"],
            "node_alias": node_aliases[vm["node"]],
            "template": vm["template"],
            "cores": vm.get("cores", 2),
            "memory": vm.get("memory", 2048),
            "enabled": host_vars.get(
                "proxmox_enabled", group_vars.get("proxmox_enabled", True)
            ),
            "protected": host_vars.get(
                "proxmox_protected", group_vars.get("proxmox_protected", False)
            ),
            "vmid": int(inline["proxmox_vmid"]),
            "app_ip": inline["app_ip"],
            "management_ip": inline["management_ip"],
        }
        if vm.get("disk_resize") is not None:
            host["disk_resize"] = vm["disk_resize"]

        result[name] = host

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
