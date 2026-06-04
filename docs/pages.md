# GitHub Pages

This repository publishes a static Storybook build to GitHub Pages from
the `.github/workflows/storybook.yml` workflow. The workflow:

1. Runs on every push to `main` (and via `workflow_dispatch`).
2. Installs frontend deps, typechecks, runs the vitest suite.
3. Builds Storybook to `frontend/storybook-static/`.
4. Uploads that directory as a Pages artifact and deploys it.

## Enabling Pages on this repo

One-time setup (do this once in the GitHub UI):

1. Open **Settings → Pages**.
2. Under **Source**, choose **GitHub Actions** (not "Deploy from a
   branch"). The workflow already requests the `pages: write`
   permission.
3. Save. Subsequent pushes to `main` will publish to
   `https://<org>.github.io/<repo>/`.

If Pages is set to "Deploy from a branch" the workflow will fail with
a permissions error — switch it to "GitHub Actions" to fix.

## URL

After the first successful deploy the URL is shown on the workflow run
summary and in **Settings → Pages**. Subsequent runs overwrite the
previous deployment (single `environment: github-pages` slot).
