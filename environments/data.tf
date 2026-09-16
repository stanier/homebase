data "external" "vm_hostvars" {
  program = ["${path.module}/../scripts/vm-hostvars.py"]
}

data "external" "proxmox_nodes" {
  program = ["${path.module}/../scripts/proxmox-nodes.py"]
}
