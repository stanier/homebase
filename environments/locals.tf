locals {
  # Selects which inventory/<env> vms/nodes below resolve against too --
  # ANSIBLE_INVENTORY_ENV (set by scripts/tofu-with-vault-secrets.sh /
  # scripts/import.sh from this same workspace name) must always match
  # whichever workspace is currently selected.
  pool            = terraform.workspace
  storage         = "local"
  snippet_storage = "local"
  network_prefix  = 24

  app_bridge  = "vmbr2"
  app_gateway = "192.168.104.1"
  mgmt_bridge = "vmbr42"

  management_mac_prefix = "02:42:FF"
  management_interface  = "eth1"

  ciuser         = "automation"
  ssh_public_key = trimspace(file(pathexpand("~/.ssh/automation_ed25519.pub")))

  default_disk_size = "10G"

  stop_on_destroy = true

  # Golden template vmids -- inventory/<env>/group_vars/proxmox/templates.yml.
  templates = {
    debian-13 = 9001
    rocky-10  = 9003
  }

  vms = jsondecode(data.external.vm_hostvars.result.json)

  nodes = jsondecode(data.external.proxmox_nodes.result.json)

  management_mac = { for name, vm in local.vms :
    name => "${local.management_mac_prefix}:${substr(format("%06x", vm.vmid), 0, 2)}:${substr(format("%06x", vm.vmid), 2, 2)}:${substr(format("%06x", vm.vmid), 4, 2)}"
  }

  enabled_vms = { for name, vm in local.vms : name => vm if try(vm.enabled, true) }

  vm_config = { for name, vm in local.enabled_vms : name => merge(vm, {
    template_vmid  = local.templates[vm.template]
    disk_size      = try(vm.disk_resize, local.default_disk_size)
    management_mac = local.management_mac[name]
  }) }
}
