# One instance of this module = one cloned VM, equivalent to what
# ansible-playbooks' roles/hypervisor/tasks/create_vm.yml does per host in
# [vm]. Golden templates themselves are still built by Ansible
# (plays/build_proxmox_templates.yml) -- this module only ever clones an
# already-existing template vmid, it never builds one.

variable "name" {
  description = "inventory_hostname equivalent -- also the Proxmox VM name."
  type        = string
}

variable "node" {
  description = "Real Proxmox node name this VM lives on -- see the calling environment's `nodes` local."
  type        = string
}

variable "vmid" {
  description = "Real Proxmox vmid for this VM (hosts.ini's proxmox_vmid)."
  type        = number

  validation {
    condition     = var.vmid >= 100 && var.vmid <= 999999999
    error_message = "vmid must be a valid Proxmox VM ID (100-999999999)."
  }
}

variable "template_vmid" {
  description = "vmid of the golden template to clone (proxmox_templates.*.vmid in inventory)."
  type        = number

  validation {
    condition     = var.template_vmid >= 100 && var.template_vmid <= 999999999
    error_message = "template_vmid must be a valid Proxmox VM ID (100-999999999)."
  }
}

variable "pool" {
  description = "Proxmox resource pool (proxmox_pool), e.g. testzone/dangerzone."
  type        = string
}

variable "storage" {
  description = "Storage id the cloned disk lands on (proxmox_storage_pool)."
  type        = string
}

variable "disk_size" {
  description = "Disk size after clone, e.g. \"20G\" (proxmox_vm.disk_resize). Null keeps the template's own size."
  type        = string
  default     = null
}

variable "cores" {
  type    = number
  default = 2
}

variable "memory" {
  type    = number
  default = 2048
}

variable "memory_min" {
  description = "Balloon floor in MiB (Proxmox's \"Minimum memory\", proxmox_vm.memory_min) -- the host may reclaim down to this when it's above ~80% RAM. Null or equal to memory keeps the balloon device for stats but never shrinks the guest."
  type        = number
  default     = null
}

variable "cpu_type" {
  description = "Emulated CPU type -- \"host\" by default, same reasoning as proxmox_vm_cpu in the Ansible role: the Proxmox nodes here aren't clustered, so there's no cross-node migration compatibility to give up."
  type        = string
  default     = "host"
}

variable "vga_type" {
  type    = string
  default = "std"
}

variable "app_bridge" {
  description = "net0 -- the app-serving VLAN bridge on this VM's node."
  type        = string
}

variable "app_ip" {
  type = string

  validation {
    condition     = can(regex("^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}$", var.app_ip))
    error_message = "app_ip must be a bare IPv4 address (e.g. 192.168.1.10) -- app_network_prefix is supplied separately, no /CIDR suffix here."
  }
}

variable "app_gateway" {
  type = string

  validation {
    condition     = can(regex("^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}$", var.app_gateway))
    error_message = "app_gateway must be a bare IPv4 address -- check proxmox_app_gateway for this VM's node in inventory (group_vars/proxmox/ or host_vars/<node>.yml)."
  }
}

variable "management_network" {
  description = "Whether the VM gets net1 on mgmt_bridge (management_network_enabled). False skips the NIC, its ip_config and the mgmt-NIC rename snippet; mgmt_bridge/management_ip are then ignored."
  type        = bool
  default     = true
}

variable "mgmt_bridge" {
  description = "net1 -- the management VLAN bridge on this VM's node."
  type        = string
}

variable "management_ip" {
  type = string

  validation {
    condition     = can(regex("^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}$", var.management_ip))
    error_message = "management_ip must be a bare IPv4 address (e.g. 192.168.42.10) -- management_network_prefix is supplied separately, no /CIDR suffix here."
  }
}

variable "app_network_prefix" {
  description = "CIDR prefix length for app_ip on net0, e.g. 24 for a 255.255.255.0 mask."
  type        = number
  default     = 24

  validation {
    condition     = var.app_network_prefix >= 1 && var.app_network_prefix <= 32 && floor(var.app_network_prefix) == var.app_network_prefix
    error_message = "app_network_prefix must be a whole number from 1 to 32 (the /N of a CIDR, not a dotted netmask)."
  }
}

variable "management_network_prefix" {
  description = "CIDR prefix length for management_ip on net1, e.g. 21 for a 255.255.248.0 mask."
  type        = number
  default     = 24

  validation {
    condition     = var.management_network_prefix >= 1 && var.management_network_prefix <= 32 && floor(var.management_network_prefix) == var.management_network_prefix
    error_message = "management_network_prefix must be a whole number from 1 to 32 (the /N of a CIDR, not a dotted netmask)."
  }
}

variable "management_mac" {
  description = <<-EOT
    Pinned MAC for net1, e.g. "02:42:FF:00:01:02" -- computed by the caller
    from management_mac_prefix + vmid, same as create_vm.yml's
    management_mac fact. Empty string skips MAC pinning and the mgmt-NIC
    rename snippet entirely (net1 keeps whatever MAC/name Proxmox and the
    guest assign it), matching the Ansible role's behavior when
    management_mac_prefix is unset.
  EOT
  type        = string
  default     = ""

  validation {
    condition     = var.management_mac == "" || can(regex("^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$", var.management_mac))
    error_message = "management_mac must be empty (skip MAC pinning) or a MAC address like 02:42:FF:00:01:02."
  }
}

variable "management_interface" {
  description = "Guest interface name net1 should be renamed to via udev, e.g. mgmt1/eth1 (management_interface). Only used when management_mac is set."
  type        = string
  default     = ""
}

variable "snippet_storage" {
  description = "Directory-backed storage with the Snippets content type enabled (proxmox_snippet_storage), for the mgmt-NIC-rename vendor-data snippet."
  type        = string
  default     = "local"
}

variable "nameservers" {
  type    = list(string)
  default = []
}

variable "ciuser" {
  description = "Cloud-init user provisioned on the VM -- always passed explicitly by environments/locals.tf (local.ciuser), so no default here doubles as a stale placeholder."
  type        = string
}

variable "ssh_public_key" {
  type = string
}

variable "started" {
  description = "Power state -- true keeps this VM running (state: started in create_vm.yml)."
  type        = bool
  default     = true
}

variable "stop_on_destroy" {
  description = <<-EOT
    Forces a Proxmox stop (hard power-off) instead of a graceful ACPI
    shutdown when this VM is destroyed. Defaults to false (the
    provider's own default) -- a graceful shutdown is the right choice
    for anything that might actually be holding state worth flushing.
    Set true for VMs that are routinely destroyed/recreated (e.g.
    testzone's testrun.sh --clean cycle), where waiting out a guest
    that's slow -- or has stopped responding -- to shut down gracefully
    just stalls the rebuild for no benefit.
  EOT
  type        = bool
  default     = false
}
