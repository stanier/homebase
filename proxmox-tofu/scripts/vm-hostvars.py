#!/usr/bin/env python3
"""Terraform `external` data source program shared by every
environments/<env>/locals.tf.

Reads each [vm] host's effective config straight out of the Ansible
inventory (ansible-playbooks/inventory/<env>, ANSIBLE_INVENTORY_ENV --
defaults to "testzone" since that's the only environment actually wired
up to run this by hand today, see plays/provision_vms.yml's own comment
for how a Tofu-driven Ansible run passes the real one through) instead of
it being duplicated as Tofu literals: ansible_host/management_ip/app_ip/
proxmox_vmid from hosts.ini's [vm] lines, the proxmox_vm block (node/
template/cores/memory/memory_min/disk_resize/usb_mappings), proxmox_enabled/proxmox_protected
and app_network_prefix/management_network_prefix from group_vars/vm.yml,
overridden per-host by host_vars/<name>.yml.

A host whose proxmox_vm.node isn't in hosts.ini's [proxmox] group is a
hard error here -- vms.tf picks each VM's provider by that node name, so
there'd be no provider for it.

Parses hosts.ini/group_vars/host_vars directly (plain YAML/text, no
templating) rather than shelling out to `ansible-inventory --list` --
same rationale as scripts/network-config.py: `ansible-inventory --list`
eagerly renders *every* host's vars, including other hosts' Jinja
references to group_vars/all/vault.yml secrets (e.g. host_vars/
turkey.yml's proxmox_api_token_secret) that have nothing to do with
what this script reads, so it would demand a vault password this script
otherwise has no use for. This isn't the original hand-rolled parser
resurrected, though -- that one hit a real bug (ansible_user moving
from hosts.ini's inline [proxmox:vars] into group_vars/proxmox/ silently
broke it) because it hardcoded *where* a var lived. This one discovers
every group_vars/<group>/*.yml file by glob and parses real YAML
instead, so a var moving between files in that directory can't drop out
silently the same way.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<hostname -> {node,
template, cores, memory, memory_min, disk_resize, enabled, protected, vmid, app_ip,
management_ip, management_network, app_network_prefix, management_network_prefix,
usb_mappings}
as a JSON string>"} -- `external` only allows string
values in its result, so the real payload is nested one level down and
locals.tf jsondecode()s it back out.
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


def effective_hostvars(
    inventory_dir: Path, group: str, name: str, inline_vars: dict, extra_groups=()
) -> dict:
    """Ansible's real precedence for the vars these scripts touch:
    group_vars/<group> < group_vars/<each extra group, alphabetical> <
    hosts.ini inline vars < host_vars/<host>. extra_groups is every other
    group the host is listed under (hosts.ini + groups.ini) -- e.g.
    groups.ini's [vm_turkey], whose group_vars/vm_turkey.yml is what
    overrides group_vars/vm.yml's proxmox_vm.node: homelab default.
    Skipping those used to put every VM on homelab here while
    provision_vms.yml (real Ansible precedence) targeted turkey's VMs as
    vm_node1[...] -- addresses Tofu then silently matched nothing for.
    group_vars/all is deliberately never consulted -- see this script's
    own docstring."""
    merged = {}
    merged.update(load_yaml_vars(inventory_dir / "group_vars" / group))
    for extra in sorted(set(extra_groups) - {group}):
        merged.update(load_yaml_vars(inventory_dir / "group_vars" / extra))
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
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "dangerzone")
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
    # groups.ini holds the per-node [vm_<node>] groups (and app groups);
    # only consulted for membership, so it can't add hosts to [vm].
    groups_ini = inventory_dir / "groups.ini"
    membership = dict(groups)
    if groups_ini.is_file():
        for group, members in parse_hosts_ini(groups_ini).items():
            membership[group] = membership.get(group, []) + members
    proxmox_hosts = [name for name, _ in groups.get("proxmox", [])]

    result = {}
    for name, inline_vars in groups.get("vm", []):
        host_groups = [g for g, members in membership.items() if any(m == name for m, _ in members)]
        hv = effective_hostvars(inventory_dir, "vm", name, inline_vars, host_groups)
        vm = hv.get("proxmox_vm", {})
        memory = vm.get("memory", 2048)

        if vm["node"] not in proxmox_hosts:
            print(
                f"{name}'s proxmox_vm.node is {vm['node']!r}, which isn't in "
                f"inventory/{inventory_env}/hosts.ini's [proxmox] group "
                f"({', '.join(proxmox_hosts)}). Typo, or add the node to [proxmox].",
                file=sys.stderr,
            )
            sys.exit(1)

        host = {
            "node": vm["node"],
            "template": vm["template"],
            "cores": vm.get("cores", 2),
            "memory": memory,
            # Balloon floor -- 0.5 mirrors roles/hypervisor's
            # proxmox_vm_balloon_ratio default, which this script can't
            # see (role defaults aren't inventory).
            "memory_min": vm.get(
                "memory_min",
                int(memory * hv.get("proxmox_vm_balloon_ratio", 0.5)),
            ),
            "enabled": hv.get("proxmox_enabled", True),
            "protected": hv.get("proxmox_protected", True),
            "vmid": int(hv["proxmox_vmid"]),
            "app_ip": hv["app_ip"],
            "management_ip": hv["management_ip"],
            # Same flag roles/management_network gates on. False drops
            # net1 and its cloud-init config entirely (e.g. a node with
            # no management VLAN).
            "management_network": hv.get("management_network_enabled", True),
            # CIDR prefix lengths for net0/net1 -- group_vars/vm.yml,
            # overridable per host. 24 matches the old hardcoded value.
            "app_network_prefix": int(hv.get("app_network_prefix", 24)),
            "management_network_prefix": int(hv.get("management_network_prefix", 24)),
        }
        usb_mappings = vm.get("usb_mappings", [])
        if not isinstance(usb_mappings, list) or not all(isinstance(m, str) for m in usb_mappings):
            print(f"{name}'s proxmox_vm.usb_mappings must be a list of mapping names", file=sys.stderr)
            sys.exit(1)
        # Proxmox resource mapping names (Datacenter → Resource Mappings
        # → USB) to pass through, e.g. ["meshcore"].
        host["usb_mappings"] = usb_mappings

        if vm.get("disk_resize") is not None:
            host["disk_resize"] = vm["disk_resize"]

        result[name] = host

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
