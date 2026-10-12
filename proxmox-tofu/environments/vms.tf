module "vm" {
  source = "../modules/proxmox_vm"

  for_each = local.vm_config

  providers = {
    proxmox = proxmox.by_node[each.value.node]
  }

  name          = each.key
  node          = each.value.node
  vmid          = each.value.vmid
  template_vmid = each.value.template_vmid
  pool          = local.pool
  storage       = local.nodes[each.value.node].storage

  cores      = each.value.cores
  memory     = each.value.memory
  memory_min = each.value.memory_min

  disk_size = each.value.disk_size

  usb_mappings = each.value.usb_mappings

  app_bridge  = local.nodes[each.value.node].app_bridge
  app_ip      = each.value.app_ip
  app_gateway = local.nodes[each.value.node].app_gateway

  management_network = each.value.management_network
  mgmt_bridge        = local.nodes[each.value.node].mgmt_bridge
  management_ip      = each.value.management_ip

  app_network_prefix        = each.value.app_network_prefix
  management_network_prefix = each.value.management_network_prefix

  management_mac       = each.value.management_mac
  management_interface = local.management_interface
  snippet_storage      = local.nodes[each.value.node].snippet_storage

  nameservers     = []
  ciuser          = local.ciuser
  ssh_public_key  = local.ssh_public_key
  stop_on_destroy = local.stop_on_destroy
}

# One-time migration from the old per-node vm_node1 (turkey) / vm_node2
# (homelab) modules. Covers both workspaces; a `from` that isn't in the
# selected workspace's state is a no-op. Delete once testzone and
# dangerzone have each been applied with this config.
moved {
  from = module.vm_node1["app-host"]
  to   = module.vm["app-host"]
}
moved {
  from = module.vm_node1["app-proxy"]
  to   = module.vm["app-proxy"]
}
moved {
  from = module.vm_node1["container-sandbox"]
  to   = module.vm["container-sandbox"]
}
moved {
  from = module.vm_node1["gitea-runner-1"]
  to   = module.vm["gitea-runner-1"]
}
moved {
  from = module.vm_node1["glassroom"]
  to   = module.vm["glassroom"]
}
moved {
  from = module.vm_node1["stoa"]
  to   = module.vm["stoa"]
}
moved {
  from = module.vm_node1["trivy"]
  to   = module.vm["trivy"]
}
moved {
  from = module.vm_node2["dns"]
  to   = module.vm["dns"]
}
moved {
  from = module.vm_node2["dnsguard"]
  to   = module.vm["dnsguard"]
}
moved {
  from = module.vm_node2["freeipa"]
  to   = module.vm["freeipa"]
}
moved {
  from = module.vm_node2["gitea"]
  to   = module.vm["gitea"]
}
moved {
  from = module.vm_node2["keycloak"]
  to   = module.vm["keycloak"]
}
moved {
  from = module.vm_node2["mail1"]
  to   = module.vm["mail1"]
}
moved {
  from = module.vm_node2["metrics1"]
  to   = module.vm["metrics1"]
}
moved {
  from = module.vm_node2["wazuh"]
  to   = module.vm["wazuh"]
}
moved {
  from = module.vm_node2["wireguard"]
  to   = module.vm["wireguard"]
}
