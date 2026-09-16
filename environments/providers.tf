provider "proxmox" {
  alias     = "node1"
  endpoint  = "https://${local.nodes.node1.host}:8006/"
  api_token = "root@pam!ansible=${var.node1_api_token_secret}"
  insecure  = true # self-signed cert, same as proxmox_api_validate_certs: false

  ssh {
    username    = "keyton"
    private_key = file(pathexpand("~/.ssh/id_ed25519"))

    node {
      name    = local.nodes.node1.name
      address = local.nodes.node1.host
    }
  }
}

provider "proxmox" {
  alias     = "node2"
  endpoint  = "https://${local.nodes.node2.host}:8006/"
  api_token = "root@pam!ansible=${var.node2_api_token_secret}"
  insecure  = true

  ssh {
    username    = "keyton"
    private_key = file(pathexpand("~/.ssh/id_ed25519"))

    node {
      name    = local.nodes.node2.name
      address = local.nodes.node2.host
    }
  }
}
