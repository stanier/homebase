# homebase

Infrastructure-as-code for a self-hosted homelab: two Proxmox nodes,
Ansible for host lifecycle and configuration, OpenTofu for VM
provisioning, and Podman Quadlet units for every containerized service.

## Repos

- **[`ansible-playbooks/`](ansible-playbooks/README.md)** — onboarding,
  updates, and everything not containerized (native mail stack,
  firewall, backups, CI deploy wiring). Also drives deployment of
  `container-files` onto hosts. Two environments: `testzone`
  (dev/staging) and `dangerzone` (prod).
- **[`proxmox-tofu/`](proxmox-tofu/README.md)** — OpenTofu module +
  root config that clones VMs from golden templates, wires up
  networking/cloud-init, and boots them. The supported default for VM
  *creation/deletion* in both environments; everything after boot is
  still Ansible.
- **[`container-files/`](container-files/README.md)** — Podman Quadlet
  unit definitions and config for every containerized service (Caddy,
  DNS, Gitea, monitoring stack, mail webmail, etc.), synced onto hosts
  by `ansible-playbooks`' `containerapps` role.

## How they fit together

```
proxmox-tofu   -- clones/boots a VM from a golden template
     |
     v
ansible-playbooks -- onboards it, configures the OS, deploys native
     |                services (mail, firewall, backups)
     v
container-files -- (via ansible-playbooks' containerapps role) the
                    containerized services that actually run on it
```

A typical VM's lifecycle: `proxmox-tofu` creates it →
`ansible-playbooks` brings up the management network and runs
onboarding/baseline/update → if it's a `[containers]` host,
`ansible-playbooks` also syncs `container-files` onto it and starts the
relevant systemd Quadlet units.

See `ansible-playbooks/docs/Typical_Procedure.md` for the full,
step-by-step version of that flow, and `ansible-playbooks/docs/VAULT.md`
for how secrets move between all three repos.

## License

GPLv3 — see [`LICENSE`](LICENSE).
