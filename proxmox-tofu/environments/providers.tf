# One provider instance per [proxmox] node, keyed by the node's real
# name. Connection details come from scripts/proxmox-nodes.py.
provider "proxmox" {
  alias    = "by_node"
  for_each = var.proxmox_nodes

  endpoint  = "https://${local.nodes[each.key].host}:8006/"
  api_token = "${local.nodes[each.key].api_user}!${local.nodes[each.key].api_token_id}=${var.proxmox_api_token_secrets[each.key]}"
  insecure  = true # self-signed cert, same as proxmox_api_validate_certs: false

  ssh {
    username    = local.nodes[each.key].ssh_user
    private_key = file(pathexpand(local.nodes[each.key].ssh_private_key_file))

    node {
      name    = each.key
      address = local.nodes[each.key].host
    }
  }
}
