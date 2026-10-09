# Both set by scripts/tofu-with-vault-secrets.sh / scripts/import.sh from
# hosts.ini's [proxmox] group and the vault. proxmox_nodes has to be a
# variable rather than data.external.proxmox_nodes' keys: a provider's
# for_each must be known before any data source is read.
variable "proxmox_nodes" {
  type = set(string)
}

variable "proxmox_api_token_secrets" {
  type      = map(string)
  sensitive = true
}
