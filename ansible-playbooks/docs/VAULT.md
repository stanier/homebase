### Ansible Vault

Secrets live in `group_vars/all/vault.yml`, encrypted with Ansible Vault.
`ask_vault_pass` is left `False` in `ansible.cfg` deliberately (a bare
`vault_password_file` path there would need to exist on disk for every
invocation) — pass `--ask-vault-pass` explicitly instead:

```
ansible-playbook --ask-vault-pass plays/system/update.yml
```

#### testrun.sh: one prompt covers the whole run

`testrun.sh` runs a single `ansible-playbook` invocation
(`plays/testrun.yml`, which `import_playbook`s the whole
provision→onboard→deploy sequence), so one `--ask-vault-pass` prompt
already covers everything — no per-call password plumbing needed. This
used to chain several separate `ansible-playbook` calls and thread a
`VAULT_PASS` env var between them (`scripts/vault_pass_from_env.sh` still
exists and is used the same way by `proxmox-tofu/scripts/tofu-with-vault-secrets.sh`,
which is still a separate process from `testrun.sh` and still needs that
trick for its own standalone use), but with everything down to one
invocation there's nothing left to thread between calls.

Note this depends on `plays/vm/provision_vms.yml`'s Tofu run never
needing its own vault password: `proxmox-tofu/scripts/vm-hostvars.py`/
`proxmox-nodes.py`/`network-config.py` (its Terraform `external` data
sources) read `hosts.ini`/`group_vars`/`host_vars` as plain YAML/text
directly instead of shelling out to `ansible-inventory --list` — see
those scripts' own docstrings for why: `ansible-inventory --list`
eagerly renders every host's vars, including unrelated hosts' Jinja
references to `group_vars/all/vault.yml` secrets, so it would demand a
vault password even though nothing these scripts actually read is
secret.

`become_ask_pass=False` in `ansible.cfg`, so you're only ever prompted for
the vault password — never a separate become password. This works because
routine plays (`update`, `podman`, `root_password`) run as the `automation`
service account (see below), which has passwordless sudo, instead of
prompting for a personal sudo password.

#### First-time setup

`group_vars/all/vault.yml` is checked in as plaintext scaffolding. Encrypt it
before adding real secrets:

```
ansible-vault encrypt group_vars/all/vault.yml
```

You'll be asked to set a vault password — pick one and keep it somewhere
safe (e.g. a password manager). Anyone running these playbooks needs it.

#### Editing secrets

```
ansible-vault edit group_vars/all/vault.yml
```

This decrypts to `$EDITOR`, then re-encrypts on save. Never hand-edit the
encrypted file directly.

#### Convention

Every secret is named `vault_<name>` inside `vault.yml`, and referenced under
its real name from a plaintext vars file (e.g. `group_vars/all/vars.yml`):

```yaml
# group_vars/all/vault.yml (encrypted)
vault_root_password_hash: "$6$..."

# group_vars/all/vars.yml (plaintext)
root_password_hash: "{{ vault_root_password_hash }}"
```

This keeps `git diff` readable for non-secret changes and means roles/plays
only ever reference the plain variable name.

#### Example: root_password_hash

`roles/common` sets the root password on every host using
`root_password_hash`, sourced from `vault_root_password_hash`. No role in
this repo depends on `common` via `meta/main.yml` — consistency instead
comes from `plays/system/onboarding.yml` (see
`docs/Typical_Procedure.md`), which every real fleet host goes through
first and which explicitly imports `common`'s `provisioning.yml` tasks;
`plays/system/baseline_packages.yml` and `plays/system/update.yml` also
pull `common` in directly for the same reason. That's a procedural
guarantee (onboard first, always), not a dependency-graph one —
`roles/hypervisor` in particular is deliberately left out of it, since
two of its four consuming plays
(`plays/vm/legacy/create_vm.yml`/`delete_vm.yml`) run with
`connection: local` against the Ansible controller itself, where applying
`common`'s host provisioning would be wrong. To (re)apply just the root
password fleet-wide outside of onboarding, run
`plays/system/root_password.yml` (its `provisioning`/`never` tags mean it
only runs when tagged explicitly). Store a pre-hashed value in the vault,
never plaintext — generate one with `mkpasswd -m sha-512` or
`openssl passwd -6`.

#### container-files secrets

Services deployed by `roles/containerapps` (see `plays/apps/podman.yml`) get
their secrets the same way: a `vault_<name>` entry in `vault.yml`, exposed
under its real name in `group_vars/all/vars.yml`, then wired to the host(s)
that actually run the service via two host_vars keys:

```yaml
# host_vars/<host>.yml
containerapps_env:
  <service-subdir>:
    SOME_VAR: "{{ some_plain_var_name }}"

containerapps_secret_files:
  - dest: <service-subdir>/path/relative/to/service/dir
    content: |
      arbitrary file contents, e.g. a config snippet
      referencing "{{ some_plain_var_name }}"
```

`containerapps_env` writes a `.env` file into each named service's
directory before `podman-compose up`, so compose files reference secrets as
`${SOME_VAR}` instead of hardcoding them -- podman-compose loads `.env`
from its working directory automatically, and the role already `chdir`s
into each service's directory. `containerapps_secret_files` handles
secrets that don't fit the env-var shape, e.g. `dns/data/named.conf`
`include`s a TSIG key file instead of embedding it, since bind-mounted
config isn't run through compose's variable substitution.

Neither of these is committed to the `container-files` repo -- `.env` and
`dns/data/keys/` are gitignored there -- so the repo itself stays free of
live secrets; only the Ansible Vault holds real values, and both tasks use
`no_log: true` so secret values never land in playbook output.

Current secrets using this pattern: `vault_pages_forge_api_token`
(pages-server's Gitea API token),
`vault_dns_acme_tsig_secret` (the RFC2136 TSIG key shared between
pages-server's ACME DNS-01 solver and BIND's `dns/` zone -- same secret,
two consumers, so it's one vault entry referenced from both
`containerapps_env.pages` and `containerapps_secret_files`),
`vault_influxdb_admin_password` and `vault_influxdb_admin_token` (InfluxDB
init credentials, consumed by `influxdb/.env` -- the token is also reused
in the Grafana datasource provisioning file below since Grafana needs it
to query InfluxDB), `vault_grafana_admin_password` (Grafana's admin
login, consumed by `grafana/.env`), `vault_keycloak_pg_password`
(Keycloak's Postgres password, consumed by both `keycloak/.env` and
`postgresql/.env` via `containerapps_env.keycloak` -- same one value,
two consumers, same convention as `vault_dns_acme_tsig_secret` above)
and `vault_keycloak_bootstrap_password` (the `admin` realm-admin
account's initial password, `KC_BOOTSTRAP_ADMIN_PASSWORD`), and `vault_adguardhome_admin_password_hash`
(bcrypt hash, same convention as `root_password_hash` above -- never store
the plaintext password itself -- consumed by `adguardhome/data/conf/AdGuardHome.yaml`
via `containerapps_secret_files` so AdGuardHome skips its first-run setup
wizard and comes up with DNS already working; see
`host_vars/container-sandbox.yml`), and `vault_caddy_intermediate_key`
(EC private key for Caddy's intermediate CA, pinned so it survives a
container-sandbox rebuild instead of Caddy generating a new one into its
`/data` volume each time -- consumed by `caddy/data/intermediate.key`).
This intermediate is signed by an offline root CA that is deliberately
**not** in this vault (or anywhere in this repo, or on any host): its
private key lives only at `~/.homelab-ca/root.key` on an operator's own
machine, generated and rotated by hand via `scripts/generate_ca_chain.sh`.
That script also (re)issues each environment's intermediate; after running
it, vault-encrypt the printed intermediate key as
`vault_caddy_intermediate_key` in that environment's `vault.yml` and copy
the (non-secret) intermediate cert to `inventory/<env>/group_vars/files/caddy-intermediate.crt`
-- under `group_vars/` specifically, not a sibling `files/` dir, since
Ansible's directory inventory loader recurses into every file under
`inventory/<env>` except `group_vars/`, `host_vars/`, and `vars_plugins/`
looking for inventory sources, and chokes trying to parse a `.crt` as one
otherwise -- which `group_vars/all/vars.yml` loads into
`caddy_intermediate_cert` via a `lookup('file', ...)`. The root cert itself
is not a secret either (only its key needs to stay offline), but since this
repo is published, it isn't committed here -- it's the same file duplicated
into both `inventory/dangerzone/group_vars/files/root-ca.crt` and
`inventory/testzone/group_vars/files/root-ca.crt` (identical bytes in both,
since the root is shared across environments unlike the intermediate), and
loaded the same way into `root_ca_cert`, which `roles/common/tasks/trust_root_ca.yml`
installs into every host's trust store via `copy: content:` instead of a
static `files/root-ca.crt` in the role. It's also mirrored into
`container-files/caddy/data/root-ca.crt`,
`container-files/caddy-l4/data/root-ca.crt`, and
`container-files/gitea-runner/data/root-ca.crt` (those stay as plain
committed files in `container-files`, which isn't published).

Also using this pattern: `vault_gitea_internal_token` / `vault_gitea_lfs_jwt_secret` /
`vault_gitea_oauth2_jwt_secret` (Gitea's own internal API auth, LFS
transfer auth, and OAuth2 signing secrets -- pinned to their existing
live values rather than regenerated, since rotating any of them
invalidates existing LFS/OAuth2 tokens; consumed by the templated
`app.ini` in `host_vars/gitea.yml`, an absolute `containerapps_secret_files`
dest since it lives outside the synced container-files tree -- see that
file's own comment).

#### Example: the appdeploy CI key and its Gitea API push

`roles/appdeploy` restricts a forced-command SSH key to running
`deploy.sh` on `app-host` (see `docs/VAULT.md`'s sibling comments in that
role) -- one shared keypair across every app repo and both environments,
not per-app or per-environment, since every app repo lives under the
same `keyton` namespace and hits the same `app-host`.

Generate it once, by hand (no service to generate it on your behalf, same
situation as the Proxmox API tokens and the offline CA key below):

```
ssh-keygen -t ed25519 -N '' -f /tmp/appdeploy_ci_ed25519 -C keyton@galahad
```

Vault-encrypt the private half as `vault_appdeploy_ci_private_key` in
**both** `testzone` and `dangerzone` `vault.yml` (identical value in
both, same convention as `root_ca_cert`/`caddy_intermediate_key`'s shared
entries), then delete `/tmp/appdeploy_ci_ed25519`. The public half isn't
a secret -- copy `/tmp/appdeploy_ci_ed25519.pub`'s contents into
`appdeploy_ci_public_key` in both environments'
`group_vars/all/vars.yml` (plaintext, identical in both).

Unlike every other secret in this doc, this one is also vaulted purely
as an **offline backup**, not as the only copy Ansible ever reads: once
set, `roles/appdeploy` reads `appdeploy_ci_private_key` and pushes it
into Gitea itself, base64-encoded, as a *user*-level Actions secret
named `SSH_PRIVATE_KEY` (see `roles/appdeploy/tasks/gitea_ci_integration.yml`)
-- Gitea inherits user-level Actions secrets/variables into every repo
that user owns, so this happens once for the whole fleet, not once per
app repo. Base64, not the raw key, because `ci-actions`'
`deploy-to-apphost` composite action expects that and does its own
`base64 -d` (see that action's own comment on why: a raw multiline key
is prone to newline mangling round-tripping through a secrets field).
The same task also pushes `app-host`'s address as a `DEPLOY_HOST`
user-level Actions variable, so an app repo's workflow can reference
`${{ secrets.SSH_PRIVATE_KEY }}` / `${{ vars.DEPLOY_HOST }}` without
ever having been configured by hand in Gitea's UI. The only
manual step left per app repo is enabling Actions on it at all -- a
plain repo-settings toggle, not a secret.

Pushing to Gitea needs `vault_gitea_ci_api_token` -- a personal access
token for user `keyton` (Gitea's own UI has no API for creating tokens,
same as `vault_pages_forge_api_token`), scoped to write user-level
Actions secrets and variables. Create it once under `keyton`'s own
Settings > Applications > Generate New Token, picking whichever scope
Gitea's token-scope picker shows for Actions secrets/variables (the exact
label has moved around across Gitea versions -- check the picker itself
rather than trusting a hardcoded name here), then vault-encrypt it the
same way.

Both of these are read from `group_vars/all/vars.yml`
(`appdeploy_ci_private_key`, `gitea_ci_api_token`) and consumed by
`roles/appdeploy`, which calls Gitea's API directly against its
container's own published port (`http://<gitea-host>:3000`, see
`appdeploy_gitea_api_base` in `roles/appdeploy/defaults/main.yml`)
rather than through Caddy's public HTTPS vhost -- same reasoning as the
runner-token generation below: it avoids needing whatever host makes the
API call to trust the internal root CA just for this one purpose.

#### Generated, not vaulted: the Gitea Actions runner token

Not every value `containerapps_env` needs has to live in the vault at
all. The Gitea Actions runner registration token
(`gitea-runner/.env`'s `GITEA_RUNNER_REGISTRATION_TOKEN`) used to be a
`vault_gitea_runner_token` entry, generated once by hand in Gitea's admin
UI and pasted in -- but `act_runner` only ever reads that token before it
has registered; its real identity afterwards lives in its own persisted
`/data/.runner` file. That makes it possible to generate one on demand
instead of pinning it, so there's nothing to keep in sync in the vault
at all.

`roles/containerapps` does this itself: on the `gitea` host, it runs
`podman exec systemd-gitea gitea actions generate-runner-token` (as the
`containers` user, same as every other podman command in that role) and
`set_fact`s the result (`no_log: true`, so it never lands in playbook
output). `host_vars/gitea-runner-1.yml` then reads it via
`hostvars['gitea'].gitea_runner_registration_token` instead of a vault
variable -- the same cross-host "set_fact on one host, read via
`hostvars[...]` on another" trick `roles/appdeploy`/`appdeploy_caddy`
already use to wire app-host's generated pubkey into app-proxy's Caddy
config.

Two things keep this from being either wasteful or disruptive:

- **Only generated once.** A preceding task (`stat`, `delegate_to:
  gitea-runner-1`) checks for gitea-runner's own `/data/.runner` file and
  skips asking Gitea for a token at all once it exists -- `act_runner`
  would never look at it past that point, so generating (and writing) a
  new one every run would just be churn.
- **A changed `.env` now actually restarts the service.** `roles/containerapps`
  folds `containerapps_env` write results into the same
  `containerapps_quadlet_data_changed` fact that
  `containerapps_secret_files` changes already fed into -- without this,
  the freshly-written token would sit in `.env` but never reach the
  already-running (and still unregistered) `gitea-runner` container,
  since `systemd: state=started` is a no-op against a unit that's already
  up.

One rough edge remains: this needs gitea's own container already
running, which it won't be on a truly fresh host's very first
`podman.yml` run (the token-generation task runs before quadlet units
are started). That first run leaves the registration token empty and
`act_runner` failing to register -- which is why `testrun.sh` runs
`podman.yml` twice back to back: the second pass finds gitea already up
(started at the end of the first) and generates a real token. Every run
after those two is a no-op for this token, per the "only generated once"
guard above. Running `podman.yml` a single time against an
already-established environment (the normal case -- adding a host,
picking up a `container-files` change, etc.) never needs the second
pass; it's only a from-scratch `gitea` that requires it, which in
practice only happens via `testrun.sh`.

This pattern -- generate via the running service's own CLI/API instead
of a human clicking through a UI once -- only applies to secrets the
service actually exposes a way to (re)create programmatically. Something
like `vault_pages_forge_api_token` (a personal access token created by
hand in Gitea's UI) has no such hook, so it stays a vaulted, manually
created secret.

#### Example: the metrics stack's InfluxDB token

Grafana's InfluxDB datasource needs a live admin token embedded in its
provisioning YAML, not just an env var substitution, so the whole file is
generated via `containerapps_secret_files` (dest
`grafana/provisioning/datasources/datasources.yaml`) referencing the same
`influxdb_admin_token` used in `containerapps_env.influxdb`. See
`host_vars/container-sandbox.yml`.

#### Example: the Proxmox API tokens

`roles/hypervisor` (VM template building and creation, see
`docs/Typical_Procedure.md`) authenticates to the Proxmox API with a token
instead of a password. `proxmox_api_user` and `proxmox_api_token_id` are
just identifiers, plaintext defaults in
`roles/hypervisor/defaults/main.yml`. `proxmox_api_token_secret` is the
actual secret -- but unlike every other secret in this doc, it's set
**per Proxmox node**, in `host_vars/turkey.yml` /
`host_vars/homelab.yml`, not once in `group_vars/all/vars.yml`: turkey and
homelab aren't clustered, so each has its own independent user/token
database, and a token created on one is meaningless on the other.

One-time setup, repeated on **each** Proxmox node separately (turkey,
then homelab): in that node's Proxmox web UI, go to Datacenter >
Permissions > API Tokens, add a token for the user/ID named in
`roles/hypervisor/defaults/main.yml` (`ansible@pve` / `ansible-automation`
by default -- create that PVE user first under Datacenter > Permissions >
Users if it doesn't exist, with permissions to manage VMs), uncheck
"Privilege Separation" so the token inherits the user's own permissions,
then paste the generated secret into the vault under that node's own
name:

```
ansible-vault edit group_vars/all/vault.yml
```

```yaml
vault_turkey_proxmox_api_token_secret: "..."
vault_homelab_proxmox_api_token_secret: "..."
```

#### Example: restic backup credentials

`roles/backup` needs `vault_backup_password` (the restic repository
encryption password) and, since the fleet's repos are S3-backed,
`vault_backup_aws_access_key_id` / `vault_backup_aws_secret_access_key`.
All three are set once in `group_vars/all/vars.yml`/`vault.yml` and
shared across every `[containers]` host -- it's the repository *path*
that varies per host (`backup_repository` in `group_vars/containers.yml`,
built from `{{ inventory_hostname }}`), not these credentials, same
reasoning as `vault_appdeploy_ci_private_key` being one shared value
rather than per-host.

There's no service to generate these on your behalf: pick a restic
repository password yourself (treat it like the vault password itself --
losing it makes every existing backup unrecoverable, so keep it in a
password manager, not only in the vault), and create an S3 access
key/secret pair scoped to just the `homelab-backups` bucket from
whichever provider/endpoint hosts it. Vault-encrypt all three the same
way as everything else in this doc:

```
ansible-vault edit group_vars/all/vault.yml
```

```yaml
vault_backup_password: "..."
vault_backup_aws_access_key_id: "..."
vault_backup_aws_secret_access_key: "..."
```

#### Example: mail1 (roles/mail_server, container-files/dovecot, container-files/roundcube)

Two kinds of secrets here, both currently unset (`host_vars/mail1.yml`'s
`mail_server_virtual_mailboxes` is empty and `roundcube_des_key` has no
`vault_roundcube_des_key` yet, so nothing will actually work until these
are added):

`vault_roundcube_des_key` -- a 24-character key Roundcube uses to encrypt
the IMAP password it stores in its own sqlite DB, consumed by
`roundcube/.env` via `containerapps_env.roundcube` in
`host_vars/mail1.yml`. Generate one with:

```
openssl rand -base64 18
```

`vault_mail_<name>_password_hash` -- one per virtual mailbox, a
SHA512-CRYPT hash (same convention as `root_password_hash` above -- never
store the plaintext password itself), consumed by both Postfix's
recipient map and Dovecot's passdb via `mail_server_virtual_mailboxes` in
`host_vars/mail1.yml`. Generate one with:

```
mkpasswd -m sha-512
```

Then, for each mailbox (e.g. `keyton`):

```
ansible-vault edit group_vars/all/vault.yml
```

```yaml
vault_roundcube_des_key: "..."
vault_mail_keyton_password_hash: "$6$..."
```

...and add the mailbox to `host_vars/mail1.yml`'s
`mail_server_virtual_mailboxes`:

```yaml
mail_server_virtual_mailboxes:
  - name: keyton
    password_hash: "{{ vault_mail_keyton_password_hash }}"
```

The mailbox this creates is `keyton@{{ base_domain }}` -- note that
`postfix_root_alias_recipient` in `group_vars/all/vars.yml` already
assumes this exact address for root/system mail, so it needs at least one
matching mailbox to actually land anywhere.

Dovecot's own TLS certificate isn't vaulted at all -- `roles/mail_server`
mints it directly from the same offline intermediate CA
(`caddy_intermediate_cert`/`caddy_intermediate_key`, see the Caddy
intermediate example above) via `community.crypto`, so it needs no
separate secret or manual step.

#### Example: freeipa (roles/freeipa)

Two bootstrap passwords, both required before `freeipa_enabled: true`
will actually install anything -- `roles/freeipa`'s own `assert` task
fails loudly if either is empty rather than letting
`ipa-server-install` do something undefined with a blank password:

`vault_freeipa_ds_password` -- the Directory Manager (389 Directory
Server root) password. `vault_freeipa_admin_password` -- the `admin`
Kerberos/IPA principal's password, used for both initial login and any
later `ipa` CLI/API calls (e.g. the bind-account setup Plan 3's
Keycloak/Authentik LDAP federation needs).

```
ansible-vault edit group_vars/all/vault.yml
```

```yaml
vault_freeipa_ds_password: "..."
vault_freeipa_admin_password: "..."
```

Both are exposed as `freeipa_ds_password`/`freeipa_admin_password` in
`group_vars/all/vars.yml`, same convention as everything here.
`freeipa_realm`/`freeipa_basedn` aren't secrets -- both are derived from
`base_domain` by `group_vars/all/vars.yml` directly, no vault entry
needed.

Two more, for Plan 3's LDAP bind accounts (`roles/freeipa`'s own
`ipa_user` task creates `svc-keycloak-bind`/`svc-authentik-bind` from
these): `vault_freeipa_keycloak_bind_password` and
`vault_freeipa_authentik_bind_password`, exposed the same way as
`freeipa_keycloak_bind_password`/`freeipa_authentik_bind_password`.
These are the passwords Keycloak's LDAP User Federation and Authentik's
LDAP Source authenticate to FreeIPA with -- not personal accounts, so
generate them the same way as any other opaque service secret
(`openssl rand -base64 24`, no need to remember them).

Unlike `mail1`/`authentik`, FreeIPA deliberately doesn't take over this
zone's DNS (`--setup-dns` is never passed to `ipa-server-install`) --
AdGuardHome/bind on `container-sandbox` stays the one authoritative
server, and `freeipa`'s A record plus the `_kerberos`/`_ldap` SRV
records IPA clients need live by hand in that host's `dns/` zone file
instead. See `roles/freeipa/tasks/main.yml`'s own comment for why.

One more, for Plan 4's personal SSH/sudo access
(`plays/apps/freeipa_ssh_access.yml`): a `vault_freeipa_<uid>_password`
per entry in `host_vars/freeipa.yml`'s `freeipa_admin_users` list (e.g.
`vault_freeipa_keyton_password`), referenced from that entry's own
`password` field. Unlike the bind-account passwords above, this one
*is* meant to be a one-time bootstrap/reset value, not a long-lived
secret -- FreeIPA forces a Kerberos password change at next login for
any account an admin sets a password on this way, so the real,
day-to-day password never actually lives in the vault. See
`docs/Typical_Procedure.md`'s "FreeIPA-backed SSH access" section.

#### Example: Wazuh (container-files/wazuh, roles/wazuh_agent)

More moving parts than anything else in this doc, since Wazuh's own
security model requires mutual TLS between its three services plus a
password-hash-based internal user database -- there's no way to trim
this down the way `vault_roundcube_des_key` or similar single-value
examples above do.

**One-time TLS bootstrap**, done by hand, outside Ansible entirely --
same shape as the offline root CA described in the Caddy intermediate
example above, just for a CA that's purely internal to the Wazuh stack
(not shared with anything else, so no reason to keep it as offline as
that one): run Wazuh's own `wazuh-certs-tool.sh` (or the
`generate-indexer-certs.yml` compose helper from the `wazuh-docker`
repo) once, naming nodes `wazuh-indexer`, `wazuh-manager`,
`wazuh-dashboard` to match the `NetworkAlias`es in
`container-files/wazuh/*.container`. That produces a root CA plus one
cert/key pair per node plus an `admin` client cert/key pair. Vault-encrypt
all ten as `vault_wazuh_root_ca_cert` / `vault_wazuh_root_ca_key`,
`vault_wazuh_indexer_cert` / `vault_wazuh_indexer_key`,
`vault_wazuh_manager_cert` / `vault_wazuh_manager_key`,
`vault_wazuh_dashboard_cert` / `vault_wazuh_dashboard_key`, and
`vault_wazuh_admin_cert` / `vault_wazuh_admin_key` -- the certs aren't
secret the way the keys are, but keeping the whole bundle together as
vault entries (rather than splitting certs out to plaintext `vars.yml`
the way `caddy_intermediate_cert` does) keeps this one bootstrap output
in one place instead of two.

**Passwords**, generated by hand (`openssl rand -base64 24`, same as
any other opaque service secret in this doc) and vault-encrypted:

```yaml
vault_wazuh_indexer_admin_password: "..."
vault_wazuh_dashboard_password: "..."
vault_wazuh_api_password: "..."
vault_wazuh_registration_password: "..."
```

`vault_wazuh_indexer_admin_password` and `vault_wazuh_dashboard_password`
each also need a bcrypt hash for the indexer's `internal_users.yml`
(generate with the indexer image's own tool: `podman run --rm
--entrypoint /usr/share/wazuh-indexer/plugins/opensearch-security/tools/hash.sh
wazuh/wazuh-indexer:4.14.7 -p '<password>'`) -- store those as
`vault_wazuh_indexer_admin_password_hash` /
`vault_wazuh_dashboard_password_hash`. **Do not** ship the image's
built-in `internal_users.yml` as-is: its demo hashes (`admin`,
`kibanaserver`, etc.) are public and well-known, used in every Wazuh
tutorial -- see `container-files/wazuh/wazuh-indexer.container`'s own
comment on this.

All exposed under their real names in `group_vars/all/vars.yml`, then
wired to whichever host runs `container-files/wazuh` (recommended:
`metrics1`, colocated with the rest of the observability stack -- see
the plan for why) via `containerapps_env`/`containerapps_secret_files`:

```yaml
# host_vars/<wazuh-host>.yml
containerapps_env:
  wazuh:
    INDEXER_USERNAME: admin
    INDEXER_PASSWORD: "{{ wazuh_indexer_admin_password }}"
    DASHBOARD_USERNAME: kibanaserver
    DASHBOARD_PASSWORD: "{{ wazuh_dashboard_password }}"
    API_USERNAME: wazuh-wui
    API_PASSWORD: "{{ wazuh_api_password }}"

containerapps_secret_files:
  - dest: wazuh/data/wazuh_indexer/certs/root-ca.pem
    content: "{{ wazuh_root_ca_cert }}"
  - dest: wazuh/data/wazuh_indexer/certs/indexer.pem
    content: "{{ wazuh_indexer_cert }}"
  - dest: wazuh/data/wazuh_indexer/certs/indexer-key.pem
    content: "{{ wazuh_indexer_key }}"
  - dest: wazuh/data/wazuh_indexer/certs/admin.pem
    content: "{{ wazuh_admin_cert }}"
  - dest: wazuh/data/wazuh_indexer/certs/admin-key.pem
    content: "{{ wazuh_admin_key }}"
  - dest: wazuh/data/wazuh_indexer/internal_users.yml
    content: |
      ---
      _meta:
        type: "internalusers"
        config_version: 2
      admin:
        hash: "{{ wazuh_indexer_admin_password_hash }}"
        reserved: true
        backend_roles: ["admin"]
      kibanaserver:
        hash: "{{ wazuh_dashboard_password_hash }}"
        reserved: true
  - dest: wazuh/data/wazuh_manager/certs/root-ca-manager.pem
    content: "{{ wazuh_root_ca_cert }}"
  - dest: wazuh/data/wazuh_manager/certs/wazuh.manager.pem
    content: "{{ wazuh_manager_cert }}"
  - dest: wazuh/data/wazuh_manager/certs/wazuh.manager-key.pem
    content: "{{ wazuh_manager_key }}"
  - dest: wazuh/data/wazuh_manager/authd.pass
    content: "{{ wazuh_registration_password }}"
  - dest: wazuh/data/wazuh_dashboard/certs/root-ca.pem
    content: "{{ wazuh_root_ca_cert }}"
  - dest: wazuh/data/wazuh_dashboard/certs/dashboard.pem
    content: "{{ wazuh_dashboard_cert }}"
  - dest: wazuh/data/wazuh_dashboard/certs/dashboard-key.pem
    content: "{{ wazuh_dashboard_key }}"
```

`roles/wazuh_agent` needs two more, exposed the same way and set once
in `group_vars/all/vars.yml` (fleet-wide, not per-host, like
`backup_password`): `wazuh_manager_host` (not a secret -- just the
address of whichever host runs `container-files/wazuh`) and
`wazuh_registration_password` (the same
`vault_wazuh_registration_password` used for `authd.pass` above -- one
shared secret, two consumers, same convention as
`vault_dns_acme_tsig_secret`).

#### The automation account

`roles/common` also provisions a dedicated `automation` service account on
every host (`provision_automation_user.yml`), authorized with a keypair
generated just for this purpose (private key kept locally at
`~/.ssh/automation_ed25519`, never committed; the public half lives at
`roles/common/files/automation_ed25519.pub`). It has no login password
(`password_lock: true`) and passwordless sudo via
`/etc/sudoers.d/automation`, so access is gated purely by possession of the
private key, and no become password is ever needed for it.

`plays/system/update.yml`, `plays/apps/podman.yml`, `plays/system/root_password.yml`, and now
`plays/system/onboarding.yml` too all run as this account. For a testzone host,
`environments/testzone/locals.tf`'s `ciuser`/`ssh_public_key` have
cloud-init create the `automation` account directly on first boot, so
onboarding connects as `automation` from the very start — no bootstrap
`keyton` hop, no `--tags`, no `--ask-become-pass` needed:

```
ansible-playbook plays/system/onboarding.yml -l <testzone-host>
```

`provision_automation_user.yml` (part of `common`'s provisioning tasks,
which onboarding also runs) still creates/repairs the account
idempotently regardless, which matters for any host that *doesn't* get
`automation` from cloud-init — a manually-onboarded or `dangerzone` host
still needs the old bootstrap path:

```
ansible-playbook plays/system/onboarding.yml --ask-become-pass -e ansible_user=<bootstrap-user> -l <new-host>
```
