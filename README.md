# docs-site

The homebase documentation site, built with [Zensical](https://zensical.org)
and served by Caddy. Content lives in `docs/` as Markdown; `zensical.toml`
holds the site config (nav, theme, palette).

## Layout

- `zensical.toml` — Zensical project config (site name/URL, nav, theme).
- `docs/` — Markdown source for the site.
- `Containerfile` — builds the static site output (`site/`) into a Caddy
  image for serving.
- `.gitea/workflows/deploy.yml` — on push to `main`, builds the site with
  `zensical build` and ships it to the app host via the external
  `keyton/ci-actions` `deploy-to-apphost` action (needs the
  `APPDEPLOY_HOST` repo variable and `CI_ACTIONS_TOKEN`/`APPDEPLOY_SSH_KEY`
  secrets configured in Gitea).

## Local preview

```
pip install zensical
cd docs-site
zensical serve
```

`docs/` covers the system as a whole — architecture, environments, and
how `proxmox-tofu`, `ansible-playbooks`, and `container-files` hand off
to each other. Detail specific to one of those repos lives in that
repo's own Zensical docs site instead (each carries the same
`zensical.toml`/`Containerfile`/`.gitea/workflows/deploy.yml` pattern
as this one).
