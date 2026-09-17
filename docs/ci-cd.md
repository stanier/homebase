# CI/CD

Every repo in this family — including each docs site — ships itself
through the same Gitea Actions pattern: push to `main`, a workflow
builds the artifact, and a shared composite action deploys it to
`app-host` behind Caddy.

## The pieces

- **Gitea** (`container-files/gitea`) hosts every repo.
- **`gitea-runner`** (`container-files/gitea-runner`, `act_runner`)
  executes workflows. It resolves DNS through AdGuardHome, not the
  Podman default fallback — both the runner container itself and the
  per-job containers it spawns need this set separately (they're
  independent `docker create` calls through the Podman socket). See
  `ansible-playbooks/docs/GITEA_ACTIONS.md` if a job fails to resolve
  `gitea.dangerzone.internal` or clone a step.
- **`ci-actions`** — a private shared repo holding the
  `deploy-to-apphost` composite action every app workflow calls.
  Private repos must be referenced by **full URL**, not `owner/repo@ref`
  (Gitea's `DEFAULT_ACTIONS_URL` resolution otherwise sends bare
  references to github.com), and `keyton` needs to be added as a
  Collaborative Owner on `ci-actions` or cross-repo fetches 404.
- **`roles/appdeploy`** (`ansible-playbooks`) provisions the deploy
  keypair and Gitea API token, then pushes `SSH_PRIVATE_KEY` /
  `DEPLOY_HOST` into every repo `keyton` owns automatically — nothing
  to configure by hand in a repo beyond enabling Actions.
- **`roles/appdeploy_caddy`** generates the Caddy vhost for a deployed
  app.

## A typical workflow (this repo's included)

```yaml
name: Deploy Zensical site
on:
  push:
    branches: [main]
jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with: { python-version: "3.x" }
      - run: pip install zensical
      - run: zensical build --clean
      - uses: https://gitea.dangerzone.internal/keyton/ci-actions/deploy-to-apphost@main
        with:
          app: ${{ gitea.event.repository.name }}
          container_port: 80
          deploy_host: ${{ vars.DEPLOY_HOST }}
          ssh_key: ${{ secrets.SSH_PRIVATE_KEY }}
```

`deploy-to-apphost` ships whatever the build step produced (here,
Zensical's `site/` output wrapped in a Caddy image via this repo's
`Containerfile`) to `app-host` over the `appdeploy` forced-command SSH
key, and `roles/appdeploy_caddy` wires up the public route.

Each of `proxmox-tofu`, `ansible-playbooks`, and `container-files`
carries the same `zensical.toml` + `Containerfile` +
`.gitea/workflows/deploy.yml` pattern for its own docs site — copy this
repo's as the template when adding a new one.

## Debugging a failed deploy

Start with `ansible-playbooks/docs/GITEA_ACTIONS.md` — it covers, in
order of how they were actually hit:

1. `act_runner`/job-container DNS resolution failures
2. SELinux blocking the runner from the Podman socket
3. The runner re-registering as a new identity on every deploy (a sync
   task wiping its persisted `.runner` file)
4. Fleet-wide VM DNS pointing at the wrong resolver
5. `appdeploy` keypair/token mismatches and `Permission denied
   (publickey,...)` triage steps
6. Private action repo 404s

It also flags a sharp edge worth knowing before you start debugging at
all: `ansible-inventory` exists as **two independent clones**
(`~/git/sources/gitea/ansible-inventory` and
`homebase/ansible-playbooks/inventory`) that don't sync automatically
— confirm which one you actually edited before concluding a fix "isn't
working."
