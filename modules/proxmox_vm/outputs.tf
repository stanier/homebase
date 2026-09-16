output "vmid" {
  value = proxmox_virtual_environment_vm.this.vm_id
}

output "id" {
  description = "Full resource address suffix (node/vmid), for cross-referencing in -target or downstream automation."
  value       = proxmox_virtual_environment_vm.this.id
}

output "management_mac" {
  value = local.mgmt_nic_pinned ? var.management_mac : null
}

output "app_ip" {
  value = var.app_ip
}

output "management_ip" {
  value = var.management_ip
}
