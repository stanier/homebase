# Fails the plan instead of silently leaving a VM unmanaged when its
# node_alias doesn't match any module below -- see local.unmodeled_vms.
resource "terraform_data" "check_known_node_aliases" {
  lifecycle {
    precondition {
      condition     = length(local.unmodeled_vms) == 0
      error_message = "These enabled VMs have a node_alias outside {node1, node2}, so vm_node1/vm_node2's for_each below wouldn't manage them at all: ${join(", ", local.unmodeled_vms)}. Check hosts.ini's [proxmox] group for a typo, or add a vm_node3 module here if a third node was actually added."
    }
  }
}

module "vm_node1" {
  source = "../modules/proxmox_vm"

  for_each = { for name, vm in local.enabled_vms : name => vm if vm.node_alias == "node1" }

  providers = {
    proxmox = proxmox.node1
  }

  name          = each.key
  node          = local.vm_config[each.key].node
  vmid          = local.vm_config[each.key].vmid
  template_vmid = local.vm_config[each.key].template_vmid
  pool          = local.pool
  storage       = local.storage

  cores  = local.vm_config[each.key].cores
  memory = local.vm_config[each.key].memory

  disk_size = local.vm_config[each.key].disk_size

  app_bridge  = local.app_bridge
  app_ip      = local.vm_config[each.key].app_ip
  app_gateway = local.app_gateway

  mgmt_bridge   = local.mgmt_bridge
  management_ip = local.vm_config[each.key].management_ip

  network_prefix = local.network_prefix

  management_mac       = local.vm_config[each.key].management_mac
  management_interface = local.management_interface
  snippet_storage      = local.snippet_storage

  nameservers     = []
  ciuser          = local.ciuser
  ssh_public_key  = local.ssh_public_key
  protected       = local.vm_config[each.key].protected
  stop_on_destroy = local.stop_on_destroy
}

module "vm_node2" {
  source = "../modules/proxmox_vm"

  for_each = { for name, vm in local.enabled_vms : name => vm if vm.node_alias == "node2" }

  providers = {
    proxmox = proxmox.node2
  }

  name          = each.key
  node          = local.vm_config[each.key].node
  vmid          = local.vm_config[each.key].vmid
  template_vmid = local.vm_config[each.key].template_vmid
  pool          = local.pool
  storage       = local.storage

  cores  = local.vm_config[each.key].cores
  memory = local.vm_config[each.key].memory

  disk_size = local.vm_config[each.key].disk_size

  app_bridge  = local.app_bridge
  app_ip      = local.vm_config[each.key].app_ip
  app_gateway = local.app_gateway

  mgmt_bridge   = local.mgmt_bridge
  management_ip = local.vm_config[each.key].management_ip

  network_prefix = local.network_prefix

  management_mac       = local.vm_config[each.key].management_mac
  management_interface = local.management_interface
  snippet_storage      = local.snippet_storage

  nameservers     = []
  ciuser          = local.ciuser
  ssh_public_key  = local.ssh_public_key
  protected       = local.vm_config[each.key].protected
  stop_on_destroy = local.stop_on_destroy
}
