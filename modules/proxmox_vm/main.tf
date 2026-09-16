terraform {
  required_providers {
    proxmox = {
      source = "bpg/proxmox"
    }
  }
}

locals {
  mgmt_nic_pinned = var.management_mac != ""
}

resource "proxmox_virtual_environment_file" "mgmt_rename" {
  count = local.mgmt_nic_pinned ? 1 : 0

  content_type = "snippets"
  datastore_id = var.snippet_storage
  node_name    = var.node

  source_raw {
    file_name = "${var.name}-mgmt-rename.yaml"
    data = templatefile("${path.module}/templates/mgmt-nic-rename.yaml.tftpl", {
      management_mac       = var.management_mac
      management_interface = var.management_interface
    })
  }
}

resource "proxmox_virtual_environment_vm" "this" {
  name            = var.name
  node_name       = var.node
  vm_id           = var.vmid
  pool_id         = var.pool
  started         = var.started
  stop_on_destroy = var.stop_on_destroy

  clone {
    vm_id = var.template_vmid
    full  = true
  }

  cpu {
    cores = var.cores
    type  = var.cpu_type
  }

  memory {
    dedicated = var.memory
  }

  disk {
    datastore_id = var.storage
    interface    = "scsi0"
    size         = var.disk_size != null ? trimsuffix(var.disk_size, "G") : null
  }

  network_device {
    bridge = var.app_bridge
  }

  network_device {
    bridge      = var.mgmt_bridge
    mac_address = local.mgmt_nic_pinned ? var.management_mac : null
  }

  vga {
    type = var.vga_type
  }

  initialization {
    datastore_id = var.storage

    ip_config {
      ipv4 {
        address = "${var.app_ip}/${var.network_prefix}"
        gateway = var.app_gateway
      }
    }

    ip_config {
      ipv4 {
        address = "${var.management_ip}/${var.network_prefix}"
      }
    }

    user_account {
      username = var.ciuser
      keys     = [var.ssh_public_key]
    }

    dns {
      servers = var.nameservers
    }

    vendor_data_file_id = local.mgmt_nic_pinned ? proxmox_virtual_environment_file.mgmt_rename[0].id : null
  }

  lifecycle {
    ignore_changes = [
      clone,

      disk,
    ]

    prevent_destroy = var.protected
  }
}
