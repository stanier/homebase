# ansible-playbooks

Ansible plays and roles for the homelab fleet: onboarding hosts, keeping
them updated, and deploying everything that isn't containerized (plus the
Ansible side of everything that is — see `../container-files`). VM
*creation* itself goes through `../proxmox-tofu`; this repo covers
everything before and after that.

Read `README.md` for layout and the role table, and `docs/` (especially
`docs/Typical_Procedure.md` and `docs/VAULT.md`) before touching roles
that deal with VM lifecycle or secrets.

## Conventions

- Two environments, `testzone` (dev/staging) and `dangerzone` (prod),
  live under `inventory/` — **gitignored, not committed**. Don't assume
  it exists in a fresh checkout, and never add real IPs, vmids, or
  vault contents to a tracked file.
- Every play needs `-i inventory/<env>` explicitly; there is no default
  environment. Double-check which one a command targets before running
  anything against `dangerzone`.
- Secrets go through Ansible Vault — see `docs/VAULT.md` for what's
  encrypted, where each secret is consumed, and the `automation` service
  account. Never write a plaintext secret into a role, play, or
  `group_vars`/`host_vars` file.
- `roles/containerapps` is what syncs `../container-files` onto
  `[containers]` hosts — changes to Quadlet units there take effect via
  this role's next run, not by editing anything in this repo.
- VM creation/deletion is `../proxmox-tofu`'s job now
  (`roles/hypervisor`'s `legacy_create_vm.yml`/`legacy_delete_vm.yml`
  are a fallback only — see that repo's README before reaching for
  them).
- `testrun.sh` runs the full provision → onboard → deploy sequence
  against `dangerzone` with a fixed host list and
  `containerapps_source=local`; it's the fast path for testing a change
  end-to-end from a local checkout, not a generic entry point.

## Before making changes

- Read `docs/Typical_Procedure.md` before any VM lifecycle or
  host-restore work.
- Read `docs/VAULT.md` before adding, moving, or consuming a secret.
- Check `docs/PACKAGE_MANAGEMENT.md` before touching the update plays'
  package-manager handling.
- There's no CI/test harness here beyond running the plays themselves —
  validate with `--check`/`-i inventory/testzone` against testzone
  before touching `dangerzone`, and don't run destructive plays
  (deletes, template rebuilds) without confirming the target
  environment first.
