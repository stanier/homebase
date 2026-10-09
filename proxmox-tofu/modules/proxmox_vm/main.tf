terraform {
  required_providers {
    proxmox = {
      source = "bpg/proxmox"
    }
  }
}

locals {
  mgmt_nic_pinned = var.management_network && var.management_mac != ""
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

    # Full clone, not linked. A linked clone's disk stays a delta against
    # the template's, so deleting/rebuilding the template (see
    # build_template.yml) would orphan or break every VM cloned from it.
    # Full clones cost more storage up front but keep each VM's disk
    # independent -- worth it for a golden-template workflow like this one.
    full = true
  }

  cpu {
    cores = var.cores
    type  = var.cpu_type
  }

  memory {
    dedicated = var.memory

    # bpg's floating is Proxmox's balloon value. Leaving it at the
    # provider's 0 would remove the balloon device outright, so even
    # memory_min == memory is worth passing: the device stays for
    # in-guest stats, it just never reclaims anything.
    floating = coalesce(var.memory_min, var.memory)
  }

  disk {
    datastore_id = var.storage
    interface    = "scsi0"
    size         = var.disk_size != null ? trimsuffix(var.disk_size, "G") : null

    # storage is a dir-backed datastore (qcow2 files), which does support
    # discard -- it punches holes in the qcow2 file so guest-side fstrim
    # actually reclaims host disk space instead of the file only ever
    # growing. ssd presents the disk as non-rotational so guests that
    # gate TRIM on that (Windows, some udev rules) still pass it through.
    discard = "on"
    ssd     = true
  }

  network_device {
    bridge = var.app_bridge
  }

  dynamic "network_device" {
    for_each = var.management_network ? [1] : []
    content {
      bridge      = var.mgmt_bridge
      mac_address = local.mgmt_nic_pinned ? var.management_mac : null
    }
  }

  vga {
    type = var.vga_type
  }

  initialization {
    datastore_id = var.storage

    # Proxmox defaults this to true, running a full package upgrade via
    # cloud-init on every first boot before Ansible ever touches the
    # host -- redundant with the update plays, and slower/flakier since
    # it runs unattended with no retry (see the rocky-10 template's
    # cloud-final.service failing outright on a transient AppStream
    # mirror skew). Package upgrades belong to Ansible's update plays,
    # not first boot.
    upgrade = false

    ip_config {
      ipv4 {
        address = "${var.app_ip}/${var.app_network_prefix}"
        gateway = var.app_gateway
      }
    }

    dynamic "ip_config" {
      for_each = var.management_network ? [1] : []
      content {
        ipv4 {
          address = "${var.management_ip}/${var.management_network_prefix}"
        }
      }
    }

    user_account {
      username = var.ciuser
      keys     = [var.ssh_public_key]
    }

    # Only when there's something to set -- the provider doesn't store an
    # empty dns block, so a static `servers = []` diffs on every plan.
    dynamic "dns" {
      for_each = length(var.nameservers) > 0 ? [1] : []
      content {
        servers = var.nameservers
      }
    }

    vendor_data_file_id = local.mgmt_nic_pinned ? proxmox_virtual_environment_file.mgmt_rename[0].id : null
  }

  lifecycle {
    ignore_changes = [
      clone,

      disk,
    ]

    #prevent_destroy = terraform.workspace != "testzone"
  }
}
