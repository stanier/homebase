terraform {
  required_version = ">= 1.7"

  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "~> 0.112"
    }
    
    external = {
      source  = "hashicorp/external"
      version = "~> 2.3"
    }
  }

  # path/workspace_dir deliberately left unset -- supplied via
  # -backend-config by scripts/tofu-with-vault-secrets.sh / scripts/import.sh
  # so state lives next to the Ansible inventory it's derived from, not
  # inside this repo. See README.md's "Prerequisites" for the exact
  # `tofu init` invocation.
  backend "local" {}
}
