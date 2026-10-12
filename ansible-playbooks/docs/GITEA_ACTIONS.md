# Gitea Actions: gitea-runner, appdeploy, and private action repos

Known failure modes in CI, and their fixes. Check here first before
debugging a workflow or touching gitea-runner or appdeploy.

## gitea-runner

### DNS: rootless Podman can't reach the host's resolver

Symptom: `act_runner` logs (`podman logs`/`journalctl -u gitea-runner`)
show `Cannot ping the Gitea instance server ... lookup
gitea.dangerzone.internal on 1.1.1.1:53: no such host`, or the same
`no such host` against `1.1.1.1` from inside a workflow's job container
(e.g. `actions/checkout` failing to clone).

Cause: rootless Podman can't hand a container the host's own
`resolv.conf` nameserver when it's a `systemd-resolved` stub
(`127.0.0.53`) -- a container can't reach the host's loopback, so Podman
silently substitutes a public fallback (`1.1.1.1`) instead. Every other
service in `container-files` addresses fleet peers by raw `app_ip`
(see Caddy's Caddyfile), so this never mattered until gitea-runner
needed to resolve an actual hostname.

Fix, in two places (separate container-create calls), both templated
from `gitea-runner-1`'s `containerapps_secret_files` so the address
comes from `hostvars['dnsguard'].app_ip`:
- `gitea-runner.container`'s `DNS=` -- covers `act_runner` itself.
- `gitea-runner/data/config.yaml`'s `container.options: --dns=...`
  -- covers each *job* container `act_runner` spawns per workflow run,
  which are separate `docker create` calls via the podman socket and
  aren't covered by the quadlet's own `DNS=`.

If it breaks again, compare dnsguard's `app_ip` in inventory with what's
deployed in `/srv/containers/container-files/gitea-runner/` on
`gitea-runner-1`.

### SELinux: confined container can't connect to the podman socket

Symptom: `permission denied while trying to connect to the Docker
daemon socket at unix:///var/run/docker.sock`. Confirm with:

```
getenforce
sudo ausearch -m avc -ts recent -i | tail -40
```

Look for `denied { connectto } ... tclass=unix_stream_socket` with
`tcontext=...:container_runtime_t:...`.

Cause: `connectto` is checked against the *listening process's* context
(`container_runtime_t`), not the socket file's label, so `:z`/`:Z`
relabeling can't help. No stock boolean covers it
(`container_connect_any` is for TCP).

Fix: `roles/containerapps` loads a one-rule policy module, exactly what
`audit2allow` generates from the denial. Verify it loaded:

```
semodule -l | grep gitea_runner_podman_sock
```

### Re-registering as a brand-new runner on every deploy

Symptom: a new "homelab-runner" entry appears in Gitea's Site
Administration > Actions > Runners on every `podman.yml` run instead of
the same one persisting.

Cause: act_runner's identity file `gitea-runner/data/.runner` getting
deleted. The local-source sync excludes it from `--delete`; if a new
sync path is added, it needs the same exclude. Stale runner entries
have to be removed by hand in the Runners admin page.

## Fleet-wide: VM app-network DNS points at the wrong resolver

Symptom: a container using `Network=host` (e.g. Caddy) fails to
resolve `*.dangerzone.internal` at all -- e.g. issuing a cert for a new
`appdeploy` route fails with `dial tcp: lookup
acme.dangerzone.internal on 1.1.1.1:53: no such host` in
`podman logs systemd-caddy`.

Cause: the VM is still using DHCP-provided resolvers, which don't know
`base_domain`. `roles/common/tasks/internal_dns.yml` points every VM at
dnsguard only; re-apply it with:

```
ansible-playbook -i inventory/dangerzone plays/system/baseline_packages.yml --ask-vault-pass -l <host>
```

## appdeploy

### Setting it up

See `docs/VAULT.md`'s "appdeploy CI key" section for the full secret
list. Two easy-to-miss gotchas:

- **`vault_appdeploy_ci_private_key` and `appdeploy_ci_public_key` must
  be the same keypair.** Nothing checks it, and a mismatch only shows up
  as `Permission denied (publickey,...)` on app-host. Check without
  printing the key:

  ```bash
  cd ~/homebase/ansible-playbooks
  ansible localhost -m ansible.builtin.copy -a \
    "content={{ vault_appdeploy_ci_private_key }} dest=/tmp/appdeploy_ci_priv mode=0600" \
    -e @inventory/dangerzone/group_vars/all/vault.yml \
    -e ansible_connection=local \
    --ask-vault-pass
  derived_pub=$(ssh-keygen -y -f /tmp/appdeploy_ci_priv 2>&1)
  configured_pub=$(grep 'appdeploy_ci_public_key:' inventory/dangerzone/group_vars/all/vars.yml \
    | sed -E 's/^[^"]*"([^"]*)".*$/\1/')
  [ "$derived_pub" = "$configured_pub" ] && echo MATCH || echo MISMATCH
  shred -u /tmp/appdeploy_ci_priv 2>/dev/null || rm -f /tmp/appdeploy_ci_priv
  ```

- **`vault_gitea_ci_api_token` needs the `write:user` scope
  specifically** (not `write:repository`/`write:organization`/
  `write:admin`) -- every API call `roles/appdeploy` makes is under
  `/user/actions/...`, nothing repo- or org-scoped.

Rerun `plays/apps/appdeploy.yml` after changing either the keypair or
the token -- it pushes the (base64-encoded) private key and
`DEPLOY_HOST` into Gitea automatically; nothing to configure by hand in
Gitea's UI beyond generating that one PAT.

### Debugging "Permission denied (publickey,...)" on a deploy

In order:

1. Confirm the keypair actually matches (script above).
2. Confirm `app-host`'s `authorized_keys` entry is well-formed -- a
   single stray whitespace character in the `command="...",...` options
   list silently truncates it and kills pubkey auth with this exact
   generic error, no other symptom:
   ```
   ssh keyton@app-host "sudo cat ~containers/.ssh/authorized_keys" | cat -A
   ```
3. Confirm permissions sshd cares about -- it silently rejects keys if
   any of these are group/world-writable, again with no specific error:
   ```
   ssh keyton@app-host "sudo ls -ld ~containers ~containers/.ssh ~containers/.ssh/authorized_keys"
   ```
4. If still stuck, tail sshd's own log while re-triggering the workflow
   -- this is the only place that shows the *actual* reason, rather
   than the client's generic "Permission denied":
   ```
   ssh keyton@app-host "sudo journalctl -u sshd -n 0 -f"
   ```

## Referencing a private action repo (ci-actions) from a workflow

`ci-actions` (the shared `deploy-to-apphost` composite action every
app repo's workflow uses) is private. Two things have to be true for a
workflow to actually fetch it:

- **Reference it by full URL, not a bare `owner/repo@ref`:**
  ```yaml
  uses: https://gitea.dangerzone.internal/keyton/ci-actions/deploy-to-apphost@main
  ```
  A bare reference (`uses: keyton/ci-actions/deploy-to-apphost@main`)
  resolves against Gitea's `[actions] DEFAULT_ACTIONS_URL` setting,
  which defaults to `github` (so it 404s against github.com for a repo
  that only exists locally). **Do not** set `DEFAULT_ACTIONS_URL =
  self` to fix this -- it's a global, instance-wide setting, not scoped
  to one owner, and it also hijacks every stock `uses:
  actions/checkout@v4` / `actions/setup-python@v5` reference in every
  workflow fleet-wide, routing those at this Gitea instance too (where
  they don't exist). This was tried and reverted -- see the comment in
  `host_vars/gitea.yml` where it used to live.

- **`ci-actions` needs `keyton` added as a Collaborative Owner**
  (that repo's own Settings > Actions > General). Without this, even
  the full-URL form 404s with `repository not found` for anonymous
  cross-repo access -- Gitea doesn't implicitly grant this just because
  the same account owns both repos.

Every app repo's workflow should reference
`${{ secrets.SSH_PRIVATE_KEY }}` / `${{ vars.DEPLOY_HOST }}` --
`roles/appdeploy` provides both at user level (see `docs/VAULT.md`), so
a repo only needs Actions enabled.

## The two `ansible-inventory` clones gotcha

If a fix "isn't working": `~/git/sources/gitea/ansible-inventory` and
`~/homebase/ansible-playbooks/inventory` are two
**independent clones** of the same `ansible-inventory` repo -- nothing
keeps them in sync automatically. Editing one (or committing without
pushing) has zero effect on a play run from the other. Before
concluding a fix "isn't working," confirm:

```
git -C ~/git/sources/gitea/ansible-inventory status -sb
git -C ~/homebase/ansible-playbooks/inventory status -sb
```

Both should show clean and `main...origin/main` with no ahead/behind,
and whichever one you're about to run a play from should be the one you
just edited.
