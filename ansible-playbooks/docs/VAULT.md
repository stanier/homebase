### Ansible Vault

Secrets live in each environment's `group_vars/all/vault.yml`, encrypted
with Ansible Vault. Pass `--ask-vault-pass` explicitly:

```
ansible-playbook --ask-vault-pass -i inventory/<env> plays/system/update.yml
```

`testrun.sh` is one `ansible-playbook` run over `plays/testrun.yml`, so
one prompt covers the whole sequence. Tofu's external data sources read
inventory files as plain YAML (not `ansible-inventory`, which would
render every vault reference), so provisioning needs no password of its
own. `scripts/vault_pass_from_env.sh` serves `$VAULT_PASS` as a vault
password file for separate processes such as
`proxmox-tofu/scripts/tofu-with-vault-secrets.sh`.

`become_ask_pass` is off: plays run as the `automation` account, which
has passwordless sudo (see the end of this page).

The `installer` (`../installer`) finds `vault_*` variables an
environment references but doesn't define, and generates or imports
them. `installer/internal/catalog/catalog.go` lists every secret with its
consumer.

#### First-time setup and editing

```
ansible-vault encrypt inventory/<env>/group_vars/all/vault.yml
ansible-vault edit inventory/<env>/group_vars/all/vault.yml
```

Keep the vault password in a password manager. Never hand-edit the
encrypted file.

#### Convention

Every secret is `vault_<name>` in `vault.yml` and exposed under its real
name in plaintext `group_vars/all/vars.yml`; roles only ever use the
plain name:

```yaml
# group_vars/all/vault.yml (encrypted)
vault_root_password_hash: "$6$..."

# group_vars/all/vars.yml (plaintext)
root_password_hash: "{{ vault_root_password_hash }}"
```

Store password *hashes* where the consumer accepts one (root, mailboxes,
AdGuardHome, Wazuh's internal users), never the plaintext. Opaque
service secrets: `openssl rand -base64 24`.

#### root_password_hash

`roles/common` sets root's password from `root_password_hash` during
onboarding and every baseline run. `plays/system/root_password.yml`
re-applies just that. Generate with `mkpasswd -m sha-512` or
`openssl passwd -6`.

#### container-files secrets

Services deployed by `roles/containerapps` get secrets through two
host_vars keys:

```yaml
# host_vars/<host>.yml
containerapps_env:
  <service-subdir>:
    SOME_VAR: "{{ some_plain_var_name }}"

containerapps_secret_files:
  - dest: <service-subdir>/path/relative/to/service/dir
    content: |
      config referencing "{{ some_plain_var_name }}"
```

`containerapps_env` writes `<service>/.env`, which the service's
`.container` units load with `EnvironmentFile=`.
`containerapps_secret_files` is for secrets that don't fit an env var
(config files, key files); a `dest` starting with `/` is written outside
the synced tree (e.g. gitea's `/srv/gitea/config/app.ini`). Neither ends
up in the `container-files` repo, both tasks are `no_log`, and a
changed file restarts its service.

One vault entry can feed several consumers (e.g.
`vault_dns_acme_tsig_secret` goes to both pages-server and BIND;
`vault_keycloak_pg_password` to Keycloak and its Postgres). Some values
are pinned to what a live service already uses because rotating them
breaks things (Gitea's `internal_token`, LFS and OAuth2 JWT secrets).

#### The internal CA chain

Caddy's internal PKI uses an intermediate CA signed by an offline root.
The root's key is in no vault, repo or host: it lives only in
`~/.homelab-ca/` on an operator's machine, managed by
`scripts/generate_ca_chain.sh` (`root` once, `intermediate <env>` per
environment). After issuing an intermediate, the script prints what to
do:

- vault its key as `vault_caddy_intermediate_key` in that environment;
- copy its cert to `inventory/<env>/group_vars/files/caddy-intermediate.crt`.

The root cert goes to `inventory/<env>/group_vars/files/root-ca.crt`
(same file in both environments). Both certs are loaded into
`caddy_intermediate_cert` / `root_ca_cert` with `lookup('file', ...)`,
and `roles/common` installs the root into every host's trust store. The
files sit under `group_vars/` because Ansible tries to parse anything
else under `inventory/<env>/` as an inventory source. The root cert is
also committed in `container-files` (caddy, caddy-l4, gitea-runner).

`roles/mail_server` signs Dovecot's certificate with the same
intermediate, so mail needs no extra secret.

#### appdeploy's CI key and Gitea token

One keypair serves every app repo in both environments. Generate it by
hand:

```
ssh-keygen -t ed25519 -N '' -f /tmp/appdeploy_ci_ed25519 -C appdeploy-ci
```

Vault the private half as `vault_appdeploy_ci_private_key` in both
environments, put the public half in `appdeploy_ci_public_key` in both
`group_vars/all/vars.yml`, then delete the temp files.

`roles/appdeploy` pushes the private key (base64) into Gitea as the
user-level Actions secret `SSH_PRIVATE_KEY`, and app-host's address as
the variable `DEPLOY_HOST`. Gitea inherits user-level secrets into every
repo the user owns, so a new app repo only needs Actions enabled. That
push needs `vault_gitea_ci_api_token`: a personal access token for the
repo owner with write access to Actions secrets and variables (Settings
→ Applications; the scope label varies by Gitea version). The API is
called on Gitea's published port directly, not through Caddy.

#### Generated, not vaulted: the Gitea runner token

The runner registration token isn't in the vault. `roles/containerapps`
runs `gitea actions generate-runner-token` in the gitea container and
`host_vars/gitea-runner-1.yml` reads it from
`hostvars['gitea'].gitea_runner_registration_token`. Once act_runner has
registered (its `data/.runner` file exists) no new token is generated,
so its `.env` stays stable and it isn't restarted mid-job. On a fresh
environment the token needs gitea already running, which is why
`plays/apps/podman.yml` deploys gitea on its own first.

#### Proxmox API tokens

`roles/hypervisor` and proxmox-tofu authenticate with an API token.
`proxmox_api_user` / `proxmox_api_token_id` are plain identifiers in
`group_vars/proxmox`. The secret is per node, because the nodes aren't
clustered and each has its own token database:

```yaml
vault_turkey_proxmox_api_token_secret: "..."
vault_homelab_proxmox_api_token_secret: "..."
```

On each node: Datacenter → Permissions → API Tokens, add a token for
that user/ID, untick "Privilege Separation", and vault the secret under
that node's name.

#### Mail

- `vault_roundcube_des_key` (24 characters, `openssl rand -base64 18`):
  Roundcube's key for the IMAP password it stores.
- `vault_mail_<name>_password_hash` (`mkpasswd -m sha-512`), one per
  entry in `host_vars/mail1.yml`'s `mail_server_virtual_mailboxes`:

  ```yaml
  mail_server_virtual_mailboxes:
    - name: keyton
      password_hash: "{{ vault_mail_keyton_password_hash }}"
  ```

`postfix_root_alias_recipient` expects one of these mailboxes to exist.

#### FreeIPA

- `vault_freeipa_ds_password` (Directory Manager) and
  `vault_freeipa_admin_password` (the `admin` principal, also used for
  enrollment and API calls). `roles/freeipa` refuses to install without
  both.
- `vault_freeipa_{keycloak,authentik,grafana,gitea}_bind_password`: the
  `svc-*-bind` LDAP accounts those services search FreeIPA with. All four
  are required.
- `vault_freeipa_<uid>_password` per `freeipa_admin_users` entry
  (`plays/apps/freeipa_ssh_access.yml`): a one-time bootstrap password;
  FreeIPA forces a change at first login.

FreeIPA doesn't run DNS (`--setup-dns` is never passed); its A and SRV
records are maintained by hand in the `dns` zone.

#### Keycloak OIDC clients

`vault_keycloak_bootstrap_password` is the `admin` account
`plays/apps/keycloak.yml` logs in as. Each OIDC client's secret
(`vault_keycloak_vaultwarden_client_secret`,
`vault_keycloak_synapse_client_secret`,
`vault_keycloak_meshcore_webui_client_secret`) is vaulted rather than generated
by Keycloak, so the consuming service's config can template the same
value. The play skips any client whose secret isn't defined.

#### Matrix alert bot

`vault_matrix_alertbot_as_token` and `vault_matrix_alertbot_hs_token`
(`openssl rand -hex 32` each) are the appservice tokens in chat1's
`matrix/data/alertbot-registration.yaml`. `as_token` doubles as the
access token metrics1's `matrix-alertmanager-receiver` posts alerts
with, as `@alertbot`. `hs_token` is never used (the registration has no
`url`, so Synapse never calls out), but the format requires it. Rotating
`as_token` means redeploying both chat1 and metrics1. See the homebase
repo's `docs/observability.md` for the rest of the bot's setup.

#### MeshCore MQTT logins

`vault_meshcore_mqtt_gateway_password` and
`vault_meshcore_mqtt_matrix_bot_password` (`openssl rand -hex 24` each)
are meshcore1's Mosquitto logins for `meshcore-gateway` and the planned
Matrix bridge bot (`matrix-bot`). Both are written in plaintext to
`meshcore/data/mosquitto-passwd`, and `mosquitto.container` hashes a
copy at start. The gateway's also goes into `meshcore/.env` as
`MQTT_PASSWORD`. What each login may do is set by
`container-files/meshcore/mosquitto/acl`. To add an MQTT user, add a
vaulted password, a line in `host_vars/meshcore1.yml`'s
`mosquitto-passwd` entry, and its ACL stanza.

`vault_meshcore_mqtt_webui_password` (`openssl rand -hex 24`) is the
read-only `webui` login for the traffic web UI, `meshcore-webui`. It
goes into `mosquitto-passwd` too, and into `meshcore/data/webui.env`
rather than the shared `.env`, which already has the gateway's
`MQTT_PASSWORD`. The UI has no login of its own. meshcore1's
`oauth2-proxy` puts Keycloak in front of it at `mesh.<base_domain>`,
using `vault_keycloak_meshcore_webui_client_secret` (the
`meshcore-webui` client in `plays/apps/keycloak.yml`; see Keycloak OIDC
clients above) and `vault_meshcore_webui_cookie_secret` (its
session-cookie key, which must be 16, 24 or 32 bytes:
`openssl rand -hex 16`). Both go into `meshcore/data/oauth2-proxy.env`.
Rotating the cookie secret logs everyone out of the UI.

#### Wazuh

Wazuh needs mutual TLS between its three services plus hashed internal
users.

**TLS, once, by hand:** run Wazuh's `wazuh-certs-tool.sh` with nodes
named `wazuh-indexer`, `wazuh-manager` and `wazuh-dashboard` (matching
the `NetworkAlias`es in `container-files/wazuh/`), and give each node's
`ip:` in the tool's `config.yml` that same name, not an address: it
becomes the cert's only SAN, and the manager's Filebeat verifies
`https://wazuh-indexer:9200` by hostname (`x509: certificate is not
valid for any names` means the SAN is wrong). Vault all ten outputs:
`vault_wazuh_{root_ca,indexer,manager,dashboard,admin}_{cert,key}`.

**Passwords:** `vault_wazuh_indexer_admin_password`,
`vault_wazuh_dashboard_password`, `vault_wazuh_api_password`,
`vault_wazuh_registration_password`. The first two also need bcrypt
hashes for the indexer's `internal_users.yml`
(`vault_wazuh_indexer_admin_password_hash`,
`vault_wazuh_dashboard_password_hash`):

```
podman run --rm --entrypoint /usr/share/wazuh-indexer/plugins/opensearch-security/tools/hash.sh \
  wazuh/wazuh-indexer:4.14.7 -p '<password>'
```

They must be bcrypt (`$2y$...`, what `hash.sh` prints); a `$6$`
SHA-512-crypt hash from `mkpasswd` or `password_hash('sha512')` is
silently never matched, and the dashboard hangs at "server is not ready
yet" with `Authentication finally failed for kibanaserver` in the
indexer log.

The indexer seeds its security index from `internal_users.yml` only
when the index doesn't exist yet. After changing a password or hash on
a running deployment, push it in by hand:

```
podman exec systemd-wazuh-indexer bash -c 'export JAVA_HOME=/usr/share/wazuh-indexer/jdk C=/usr/share/wazuh-indexer/config;
  bash /usr/share/wazuh-indexer/plugins/opensearch-security/tools/securityadmin.sh \
  -cd $C/opensearch-security/ -nhnv -cacert $C/certs/root-ca.pem \
  -cert $C/certs/admin.pem -key $C/certs/admin-key.pem -p 9200 -icl'
```

(as the `containers` user on the wazuh host). Never ship the image's
stock `internal_users.yml`; its demo hashes are public. The wazuh host's `host_vars` wires everything in through
`containerapps_env` (usernames and passwords) and
`containerapps_secret_files` (certs, `internal_users.yml`, the manager's
`authd.pass`).

`roles/wazuh_agent` uses `wazuh_manager_host` (plain) and
`wazuh_registration_password` (the same secret as `authd.pass`).

#### Backups (not wired in yet)

`roles/backup` would need `vault_backup_password` (the restic repo
password — losing it makes every backup unrecoverable, so keep a copy
outside the vault) plus `vault_backup_aws_access_key_id` /
`vault_backup_aws_secret_access_key`, scoped to the backup bucket.
These are shared fleet-wide; only `backup_repository` is per host.

#### The automation account

`roles/common` provisions an `automation` account on every host: key-only
(`roles/common/files/automation_ed25519.pub`; the private key stays at
`~/.ssh/automation_ed25519` on the operator's machine), password-locked,
with passwordless sudo. It's the fleet-wide `ansible_user`.

proxmox-tofu's cloud-init creates the account on first boot, so
onboarding connects as `automation` straight away. For a host that
didn't come from Tofu, bootstrap once as another user:

```
ansible-playbook -i inventory/<env> plays/system/onboarding.yml \
  --ask-become-pass -e ansible_user=<bootstrap-user> -l <new-host>
```
