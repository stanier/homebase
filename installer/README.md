# installer

Secret-bootstrap helper for `ansible-playbooks` inventory environments.
Automates the manual process documented in
[`ansible-playbooks/docs/VAULT.md`](../ansible-playbooks/docs/VAULT.md):
figuring out which `vault_*` variables an environment references but
hasn't defined yet, how each one should be generated (or where it has to
be imported from), and writing it into `group_vars/all/vault.yml`.

## Status

Feature-complete for a first pass: `scan` (read-only gap report), `set`
(add/update vault_ keys -- explicit value or catalog-driven generation,
dry-run or real), and `wizard` (guided quickstart/update walkthrough
over that same scan+set+generate foundation -- not a separate code
path). All three share `internal/catalog`, `internal/scan`,
`internal/vaultfile`, `internal/generate`, and `internal/secretops`.

`wizard` (no `-i`) now starts with a mode fork: fill secrets for an
existing environment (unchanged), or start a new one -- a multi-select
picker over `internal/svccatalog`'s six manifested services, ending in
a preview of what the selection implies (which secrets it'll need).
Picking services doesn't generate `hosts.ini`/`host_vars` yet -- that's
the next piece this preview is staged in front of.

## Usage

```
go run . wizard [-i ../ansible-playbooks/inventory/testzone]

go run . scan -i ../ansible-playbooks/inventory/testzone
go run . set  -i ../ansible-playbooks/inventory/testzone -dry-run vault_trivy_token=abc123
go run . set  -i ../ansible-playbooks/inventory/testzone vault_trivy_token=abc123

# or let the catalog generate it -- bare KEY instead of KEY=VALUE
go run . set  -i ../ansible-playbooks/inventory/testzone vault_trivy_token
go run . set  -i ../ansible-playbooks/inventory/testzone vault_wazuh_indexer_admin_password vault_wazuh_indexer_admin_password_hash
```

### wizard

Without `-i`, starts with a fork: **fill in secrets for an existing
environment**, or **start a new environment**. `-i` skips the fork
(passing a path already says which one you mean) straight to the
existing-environment flow below.

**Start a new environment** opens a multi-select picker over
`internal/svccatalog.Load()`'s services (`space` toggles, `a`/`n`
select all/none, arrow keys move) -- core services are pre-checked.
`enter` goes to a review screen listing the chosen services and every
`vault_*` name their `SecretRefs()` imply, deduplicated and sorted.
This is a **preview only**: nothing is written, and generating a real
`hosts.ini`/`host_vars` from the selection isn't built yet.

**Fill in secrets for an existing environment**: pick (or pass via
`-i`) an environment, decrypt its `vault.yml` if it has one, then step
through every `vault_*` secret it references but hasn't defined -- one
at a time, generate/paste/skip -- review, and commit. "Quickstart" and
"update" are the same code path (`internal/scan.DiffAgainst`'s gap
list), just framed differently depending on whether `vault.yml` existed
at all when the wizard started.

Entries with a `DependsOn` (the Wazuh password hashes) are automatically
ordered after their dependency, so by the time the wizard reaches the
hash its password has already been resolved.

A `StrategySha512Crypt` generation shows its companion plaintext once
("SAVE THIS NOW") before moving on -- same rule as `set`: only the hash
is ever written to `vault.yml`.

Any subprocess that needs an interactive vault password prompt
(`ansible-vault view`/`edit`/`encrypt`) runs via `tea.ExecProcess`, which
pauses the Bubble Tea program and hands the real terminal to that
subprocess -- the wizard's own code never touches the password. Review
screen's `[c] cancel` writes nothing at all, not even a backup.

### scan

Reads every `.yml`/`.yaml` file under the given environment directory
(except `vault.yml` itself) for `vault_*` references, decrypts that
environment's `vault.yml` (via `ansible-vault view`, so it prompts for
the vault password the normal way — never handled by this tool), and
reports:

- **MISSING** — referenced somewhere but not yet in `vault.yml`, with a
  description, consumer, and generation strategy pulled from
  `internal/catalog` when the name is recognized
- **DEFINED BUT UNREFERENCED** — in `vault.yml` but nothing points at it
  anymore (candidate for cleanup, but check the other environment first —
  some secrets are deliberately identical across `testzone`/`dangerzone`)
- **PRESENT** (with `-v`) — already resolved

Exit code is `1` if anything is missing, `0` otherwise — usable in a
pre-flight check before a play run.

### set

Adds or updates one or more vault_ keys in an environment's `vault.yml`,
preserving every other key's existing value, order, and (for multi-line
values) block-scalar formatting. Setting a key to the value it already
has is a no-op — safe to re-run. Each argument is either:

- `KEY=VALUE` — set an explicit value
- `KEY` (bare) — generate the value using that key's catalog entry:
  - `StrategyRandomBase64` → `generate.RandomBase64`
  - `StrategySha512Crypt` → generates a random companion plaintext
    password, hashes it with `generate.Sha512Crypt`, stores only the
    hash, and **prints the plaintext once** ("SAVE THESE NOW") since
    there's no way to recover it afterward — it's the actual login
    credential (e.g. `vault_root_password_hash`)
  - `StrategyExternalCmd` → runs the catalog's `ExternalCmdArgv`
    (e.g. Wazuh's own indexer `hash.sh`), substituting in its
    `DependsOn` key's plaintext value — which must already be resolved
    (existing in `vault.yml`, or set/generated earlier in the *same*
    command)
  - `StrategyImportOnly` and unrecognized keys fail with a message
    pointing at the catalog's import hint — these need an explicit
    `KEY=VALUE`

- `-dry-run` prints the merged YAML and exits; nothing on disk changes.
  Generation (including any external command) still runs to produce a
  value to preview — a real run afterward generates a fresh, different
  random value, it doesn't reuse the dry-run's.
- A real run backs up the existing file to `vault.yml.bak` first, then
  writes:
  - a **brand-new** `vault.yml` is freshly encrypted via
    `ansible-vault encrypt` (prompts for a new vault password);
  - an **existing encrypted** `vault.yml` is updated via
    `ansible-vault edit` with a non-interactive `EDITOR` shim, so it's
    re-encrypted with its *existing* password — this tool's own code
    never holds that password;
  - an **existing plaintext scaffold** (pre-first-encrypt, per
    VAULT.md's first-time-setup step) is overwritten in place, with a
    reminder printed to encrypt it.

Both commands honor `ANSIBLE_VAULT_PASSWORD_FILE`/`VAULT_PASS` the same
way `ansible-vault` itself does, for non-interactive/scripted use (see
the tests).

### Finding ansible-vault (`internal/ansiblebin`)

`ansible-playbooks` installs Ansible into a project-local venv, not
globally -- so a plain PATH lookup fails unless that venv happens to be
activated in the current shell, which a wizard invoked fresh usually
isn't. `ansiblebin.Resolve(anchor)` (anchor is normally the `vault.yml`
path being operated on) tries, in order: `$PATH`, `$VIRTUAL_ENV/bin/`,
then a `venv/bin/` or `.venv/bin/` found by walking up from anchor's
directory, then the same walking up from the current working directory,
then finally just the bare `ansible-vault` name (surfacing the same
"not found in $PATH" error as before if truly nothing exists anywhere).

## Layout

```
internal/catalog/    port of VAULT.md's per-secret knowledge (generation
                      strategy, consumer, shared/per-host handling)
internal/scan/        reference scanning + vault.yml diffing
internal/vaultfile/   order-preserving vault.yml load/merge/commit, with
                      a two-phase Prepare/Commit split so a caller that
                      owns the terminal (the TUI) can run the underlying
                      ansible-vault subprocess itself via tea.ExecProcess
internal/generate/    generation strategies: RandomBase64, Sha512Crypt,
                      External (argv-based, for tools like Wazuh's own
                      indexer hash.sh -- see catalog's ExternalCmdArgv)
internal/secretops/   the "given this SecretSpec, produce a value"
                      operation `set` and `wizard` share identically
internal/tui/         the Bubble Tea wizard (model/update/view/cmds)
internal/svccatalog/  declarative container-files service manifests --
                      see its own section below
internal/ansiblebin/  locates ansible-vault (PATH, $VIRTUAL_ENV, or a
                      venv/.venv found by walking up from the target
                      vault.yml/cwd) so an unactivated project venv
                      doesn't break every subprocess call
testdata/             fixture environment for go test (real inventory/ is
                      gitignored, so tests can't depend on it existing)
```

## Generation strategies (`internal/generate`)

Pure functions, no filesystem/vault dependency of their own:

- `RandomBase64(n)` — `crypto/rand` bytes, standard-base64-encoded
  (matches `openssl rand -base64 n`); `n<=0` falls back to 32 bytes.
- `Sha512Crypt(password)` — shells out to `openssl passwd -6 -stdin`
  (matches VAULT.md's `mkpasswd -m sha-512`/`openssl passwd -6`
  convention). The password is piped via stdin, never passed as an
  argv element, so it never shows up in a process listing.
- `External(argv, password)` — runs an arbitrary external tool
  (argv form, not a shell string, so a password with special
  characters can't break argument parsing), substituting
  `generate.PasswordPlaceholder` for the real password wherever it
  appears in argv. Paired with `ExtractBcryptHash` for tools like
  Wazuh's `hash.sh` that print a banner around the actual hash.
  `catalog.SecretSpec.ExternalCmdArgv` holds the ready-to-run argv for
  the two Wazuh password-hash entries.

## Service manifests (`internal/svccatalog`)

Declares each `container-files/<service>/` service as a YAML manifest
under `internal/svccatalog/manifests/` instead of a Go struct literal --
adding or correcting a service means editing a manifest file, not this
package's code (it's embedded via `go:embed`, so a rebuild is still
needed to pick it up, but nothing else is).

Each manifest mirrors the two real mechanisms `roles/containerapps`
uses to wire secrets into a service (see VAULT.md): `env` (becomes a
host's `containerapps_env.<service>` block) and `secret_files` (becomes
`containerapps_secret_files` -- a templated file dropped into the
service's own `data/` dir, e.g. Caddy's CA key). Both use this repo's
existing plain-name convention (`{{ grafana_admin_password }}`, not
`{{ vault_grafana_admin_password }}`).

Manifests don't separately declare which of their values are secrets --
`Service.SecretRefs()` derives that by scanning `env`/`secret_files` for
`{{ x }}` references and checking `catalog.LookupByVarsName(x)`, which
is just `catalog.Lookup("vault_"+x)` (mechanical, since every existing
catalog entry's vars.yml plain name is its `vault_` name with the
prefix stripped — see VAULT.md's own convention section). This is what
keeps the two catalogs from drifting apart: a manifest referencing
`{{ grafana_admin_password }}` automatically gets `vault_grafana_admin_password`
in its `SecretRefs()` with no separate mapping to maintain, and a
typo'd reference that resolves to neither a known non-secret
(`base_domain`) nor a catalog entry fails `TestNoDanglingVarRefs` loudly
instead of silently producing an unresolvable `vault_` name later.

Six core services are manifested so far: `caddy` (host networking, no
`env`, one `secret_files` entry), `gitea` (no secrets modeled yet --
its real ones go through an absolute, host-specific templated `app.ini`
this doesn't generate), `grafana`, `victoriametrics` (no secrets at
all), `authentik` (its own Quadlet network, four container units
sharing one `env` block), and `code-server` -- chosen to cover each
real pattern (host vs. default vs. named network; `env`-only vs.
`secret_files`-only vs. no secrets; single- vs. multi-unit) rather than
being an arbitrary subset. `Load()` embeds and parses all of them;
`Core()` filters to the starter-picker set (currently everything).

Not built yet: the picker/generator that actually turns a chosen set of
services into a new environment's `hosts.ini`/`host_vars` (host
allocation, IP/vmid numbering, VLAN/Proxmox-node prompts) -- this is
just the data layer that step will consume.

## Testing

```
go test ./...
```

`testdata/fixture-env` is a synthetic environment with a handful of dummy
`vault_*` values, encrypted with `testdata/fixture-vault-pass` (a fixed,
throwaway password — not a real secret, never reused anywhere else).
