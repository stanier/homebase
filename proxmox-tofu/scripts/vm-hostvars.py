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
template/cores/memory/disk_resize) and proxmox_enabled/proxmox_protected
from group_vars/vm.yml, overridden per-host by host_vars/<name>.yml.

Also assigns each host a `node_alias` ("node1", "node2", ...) based on
the order its real Proxmox node name appears in hosts.ini's [proxmox]
group -- providers.tf's provider aliases are the same generic names
(see scripts/proxmox-nodes.py), so vms.tf can select a VM's provider/
module without either file ever naming an actual node. The real name
(`node`) is still passed through for the module's own node_name/SSH-node
use, which does have to match Proxmox reality.

This shells out to `ansible-inventory --list` rather than hand-parsing
hosts.ini/group_vars/host_vars -- see scripts/proxmox-nodes.py's
docstring for the full rationale (that script used to be the hand-rolled
one and it silently fell behind a real inventory refactor; this script
already leaned that direction by reading group_vars/vm.yml as real YAML,
this just finishes the job and gets Ansible's actual merge/precedence
logic instead of reimplementing a piece of it by hand). It also means
`ansible-inventory` itself now handles the case dangerzone's hosts.ini
hits (a repeated "[vm]" group header) correctly for free, instead of
this script needing its own merge-not-overwrite workaround for it.

Requires a vault password for the same reason proxmox-nodes.py does --
group_vars/all/vault.yml has to decrypt just to load group_vars/all at
all. Already satisfied in practice: this only ever runs as a Terraform
external data source via scripts/tofu-with-vault-secrets.sh, which
exports VAULT_PASS before exec'ing tofu, and external data source
programs inherit that environment.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<hostname -> {node, node_alias,
template, cores, memory, disk_resize, enabled, protected, vmid, app_ip,
management_ip} as a JSON string>"} -- `external` only allows string
values in its result, so the real payload is nested one level down and
locals.tf jsondecode()s it back out.
"""
import json
import os
import subprocess
import sys
from pathlib import Path


def load_inventory(ansible_playbooks_dir: Path, inventory_env: str) -> dict:
    inventory_dir = ansible_playbooks_dir / "inventory" / inventory_env
    if not (inventory_dir / "hosts.ini").is_file():
        print(
            f"hosts.ini not found under {inventory_dir} (set ANSIBLE_PLAYBOOKS_DIR "
            "if ansible-playbooks isn't a sibling checkout)",
            file=sys.stderr,
        )
        sys.exit(1)

    vault_pass_script = ansible_playbooks_dir / "scripts" / "vault_pass_from_env.sh"
    proc = subprocess.run(
        [
            "ansible-inventory",
            "-i", str(inventory_dir),
            "--list",
            "--vault-password-file", str(vault_pass_script),
        ],
        cwd=ansible_playbooks_dir,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        sys.exit(1)
    return json.loads(proc.stdout)


def main():
    sys.stdin.read()  # discard the query object on stdin

    script_dir = Path(__file__).resolve().parent
    repo_root = script_dir.parent
    ansible_playbooks_dir = Path(
        os.environ.get("ANSIBLE_PLAYBOOKS_DIR", repo_root.parent / "ansible-playbooks")
    )
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "dangerzone")

    inventory = load_inventory(ansible_playbooks_dir, inventory_env)
    hostvars = inventory.get("_meta", {}).get("hostvars", {})
    vm_hosts = inventory.get("vm", {}).get("hosts", [])
    proxmox_hosts = inventory.get("proxmox", {}).get("hosts", [])

    node_aliases = {name: f"node{i + 1}" for i, name in enumerate(proxmox_hosts)}

    result = {}
    for name in vm_hosts:
        hv = hostvars.get(name, {})
        vm = hv.get("proxmox_vm", {})

        host = {
            "node": vm["node"],
            "node_alias": node_aliases[vm["node"]],
            "template": vm["template"],
            "cores": vm.get("cores", 2),
            "memory": vm.get("memory", 2048),
            "enabled": hv.get("proxmox_enabled", True),
            "protected": hv.get("proxmox_protected", True),
            "vmid": int(hv["proxmox_vmid"]),
            "app_ip": hv["app_ip"],
            "management_ip": hv["management_ip"],
        }
        if vm.get("disk_resize") is not None:
            host["disk_resize"] = vm["disk_resize"]

        result[name] = host

    print(json.dumps({"json": json.dumps(result)}))


if __name__ == "__main__":
    main()
