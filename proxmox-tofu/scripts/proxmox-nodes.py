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

This shells out to `ansible-inventory --list` rather than hand-parsing
hosts.ini/group_vars/host_vars (this script's own previous approach,
and still vm-hostvars.py's -- see that script's docstring for why it
avoided this originally): a hand-rolled parser silently falls behind
the moment inventory conventions change -- e.g. ansible_user moving
from hosts.ini's inline [proxmox:vars] into group_vars/proxmox/ broke
this exact script once already. `ansible-inventory` is the same
resolver every real Ansible run uses, so this can't drift from it.

That does mean this now needs a vault password -- group_vars/all/vault.yml
is a Vault-encrypted file, and Ansible has to decrypt it just to *load*
group_vars/all at all, even for hosts/vars that have nothing to do with
secrets. Not a new constraint in practice: this script only ever runs as
a Terraform external data source invoked via
scripts/tofu-with-vault-secrets.sh, which already exports VAULT_PASS
into its own environment before exec'ing tofu -- external data source
programs inherit that, so ansible-playbooks/scripts/vault_pass_from_env.sh
(the same VAULT_PASS-reading vault password script used elsewhere) just
works here unchanged.

stdin: the `external` data source's query object (ignored, no inputs
needed here). stdout: {"json": "<alias -> {name, host, ssh_user,
ssh_private_key_file} as a JSON string>"} -- same nested-string
convention as vm-hostvars.py, for the same reason (`external` only
allows string values in its result).
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
    inventory_env = os.environ.get("ANSIBLE_INVENTORY_ENV", "testzone")

    inventory = load_inventory(ansible_playbooks_dir, inventory_env)
    hostvars = inventory.get("_meta", {}).get("hostvars", {})
    proxmox_hosts = inventory.get("proxmox", {}).get("hosts", [])

    if not proxmox_hosts:
        print(
            f"No hosts in the [proxmox] group for inventory/{inventory_env}",
            file=sys.stderr,
        )
        sys.exit(1)

    result = {}
    for i, name in enumerate(proxmox_hosts):
        hv = hostvars.get(name, {})
        if "ansible_user" not in hv:
            print(
                f"{name} (in [proxmox]) has no effective ansible_user -- "
                "providers.tf's ssh block needs one to know who to connect as. "
                "Set it in group_vars/proxmox/, group_vars/all, or host_vars.",
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
