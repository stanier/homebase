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
    error_message = "app_ip must be a bare IPv4 address (e.g. 192.168.1.10) -- network_prefix is supplied separately, no /CIDR suffix here."
  }
}

variable "app_gateway" {
  type = string
}

variable "mgmt_bridge" {
  description = "net1 -- the management VLAN bridge on this VM's node."
  type        = string
}

variable "management_ip" {
  type = string

  validation {
    condition     = can(regex("^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}$", var.management_ip))
    error_message = "management_ip must be a bare IPv4 address (e.g. 192.168.42.10) -- network_prefix is supplied separately, no /CIDR suffix here."
  }
}

variable "network_prefix" {
  type    = number
  default = 24
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
  type    = string
  default = "keyton"
}

variable "ssh_public_key" {
  type = string
}

variable "started" {
  description = "Power state -- true keeps this VM running (state: started in create_vm.yml)."
  type        = bool
  default     = true
}

variable "protected" {
  description = <<-EOT
    Sets prevent_destroy on this VM (main.tf's lifecycle block). Defaults
    to true -- a bare `tofu destroy` (no -target) refuses to touch this
    VM unless the caller explicitly opts it out. Set false only for VMs
    that are genuinely meant to be routinely destroyed/recreated (e.g.
    testzone's testrun.sh cycle) -- an environment's locals.tf, not a
    per-host default, is the right place to make that call.
  EOT
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
