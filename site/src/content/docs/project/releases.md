---
title: Releases & contributing
description: Build Apptrail, run its checks, publish a tagged release, and deploy the documentation site.
---

Apptrail's source, installer, release workflow, and documentation live in [one GitHub repository](https://github.com/samishal1998/apptrail).

## Local development

```sh
npm ci --prefix web
npm run build --prefix web
go run .
```

For frontend hot reload, also run `npm run dev --prefix web`. Vite forwards `/api` to the Go server on port 8080.

The public website is a separate Astro Starlight project:

```sh
npm ci --prefix site
npm run dev --prefix site
```

Its default base path is `/apptrail/`. The website is static; it does not include a running Apptrail backend or any instance data.

## Checks

```sh
go test -race ./...
go vet ./...
npm run build --prefix web
node --test scripts/install.test.cjs
npm run build --prefix site
```

GitHub runs these checks for pushes to `main` and pull requests. Installer tests use local download fixtures and cover architecture selection, pinned/latest versions, checksum rejection, and preserving an existing installation on failure.

## Publish a release

Maintainers publish by pushing a semantic version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Use a new version for each release. The release workflow:

1. Builds the React frontend.
2. Runs backend, installer, and validation checks.
3. Cross-compiles the embedded Go executable for Linux AMD64/ARM64, macOS AMD64/ARM64, and Windows AMD64.
4. Packages `.tar.gz` or `.zip` archives, the installer, and `SHA256SUMS`.
5. Uploads everything to a draft release, then publishes it after all uploads succeed.

Prerelease tags such as `v0.2.0-rc.1` are marked as prereleases and do not replace the latest stable download. An existing public release is not overwritten by a rerun; a failed draft can be completed by rerunning the workflow.

To inspect the packages locally before tagging:

```sh
npm run build --prefix web
bash scripts/release.sh v0.1.0
```

This writes to a new `release-dist/` directory. The packaging script runs on Linux with Go, `tar`, `zip`, and `sha256sum`. It refuses to overwrite an existing output directory.

## Deploy GitHub Pages

The **Documentation** workflow builds `site/` and deploys it with GitHub Pages whenever relevant files change on `main`. It can also be run manually from Actions.

Repository settings must use **Pages → Source → GitHub Actions**. The workflow uses GitHub's Pages URL/base-path outputs, so the generated links and assets stay aligned with the deployment.

`install.sh` is generated into the site from the root installer during the Astro build. The Pages installer and release installer share one source file.

For a custom domain, configure it in GitHub Pages and its DNS provider. For a local custom-base build, set `SITE_URL` and `SITE_BASE` when running `npm run build --prefix site`.

## Scope

The initial release focuses on the registry, Docker/Traefik discovery, metadata, one owner account, and private/public dashboard pages. Provider expansion and richer integrations are tracked separately from the working MVP. See the [PRD](https://github.com/samishal1998/apptrail/blob/main/Apptrail_PRD.md) for the longer-term design.
