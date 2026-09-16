locals {
  # Selects which inventory/<env> vms/nodes below resolve against too --
  # ANSIBLE_INVENTORY_ENV (set by scripts/tofu-with-vault-secrets.sh /
  # scripts/import.sh from this same workspace name) must always match
  # whichever workspace is currently selected.
  pool            = terraform.workspace
  storage         = "local"
  snippet_storage = "local"
  network_prefix  = 24

  # Real bridge names/gateway/MAC prefix for turkey/homelab -- pulled
  # from inventory (group_vars/proxmox/main.yml, group_vars/vm.yml) via
  # scripts/network-config.py rather than duplicated as literals here,
  # same reasoning as local.nodes/local.vms below.
  network_config = jsondecode(data.external.network_config.result.json)

  app_bridge  = local.network_config.app_bridge
  app_gateway = local.network_config.app_gateway
  mgmt_bridge = local.network_config.mgmt_bridge

  management_mac_prefix = local.network_config.management_mac_prefix
  management_interface  = local.network_config.management_interface

  ciuser         = "automation"
  ssh_public_key = trimspace(file(pathexpand("~/.ssh/automation_ed25519.pub")))

  default_disk_size = "10G"

  # Hard power-off on destroy is fine for testzone's routinely
  # destroyed/recreated VMs (see testrun.sh), but dangerzone VMs hold
  # state worth flushing -- see modules/proxmox_vm's stop_on_destroy
  # docs -- so they get a graceful ACPI shutdown instead.
  stop_on_destroy = terraform.workspace == "testzone"

  # Golden template vmids -- inventory/<env>/group_vars/proxmox/templates.yml.
  templates = {
    debian-13 = 9001
    rocky-10  = 9003
  }

  vms = jsondecode(data.external.vm_hostvars.result.json)

  nodes = jsondecode(data.external.proxmox_nodes.result.json)

  # 3 octets (6 hex digits) only cover vmid values up to 16,777,215, but
  # modules/proxmox_vm's vmid validation allows up to 999,999,999. Taking
  # the low 6 hex digits (vmid % 0x1000000) instead of the raw, wider hex
  # string's leading 6 digits means two vmids only collide if they're
  # exactly a multiple of 16,777,216 apart -- vs. the previous
  # format("%06x", vmid) substr, which silently dropped the trailing
  # digits and collided for every vmid sharing the same leading 6 (i.e.
  # any two vmids within the same 16M-wide block).
  management_mac = { for name, vm in local.vms :
    name => "${local.management_mac_prefix}:${substr(format("%06x", vm.vmid % 16777216), 0, 2)}:${substr(format("%06x", vm.vmid % 16777216), 2, 2)}:${substr(format("%06x", vm.vmid % 16777216), 4, 2)}"
  }

  enabled_vms = { for name, vm in local.vms : name => vm if try(vm.enabled, true) }

  # vms.tf only ever defines vm_node1/vm_node2 -- a host whose node_alias
  # (from scripts/vm-hostvars.py, derived from hosts.ini's [proxmox] group
  # order) doesn't match either would silently match neither module's
  # for_each and end up unmanaged with no error. vms.tf's
  # check_known_node_aliases precondition fails the plan instead.
  unmodeled_vms = [for name, vm in local.enabled_vms : name if !contains(["node1", "node2"], vm.node_alias)]

  vm_config = { for name, vm in local.enabled_vms : name => merge(vm, {
    template_vmid  = local.templates[vm.template]
    disk_size      = try(vm.disk_resize, local.default_disk_size)
    management_mac = local.management_mac[name]
  }) }
}
