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

GitHub runs these checks for pushes to `main` and pull requests. Installer tests use local download fixtures and cover architecture selection, pinned/latest versions, checksum rejection, and preserving an existing installation on failure. Additional macOS/Windows jobs build the native executable and exercise service lifecycle operations (the macOS runtime check requires a logged-in GUI domain).

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
4. Packages `.tar.gz` or `.zip` archives, the installer, Dockerfile, Compose file, and `SHA256SUMS`.
5. Uploads everything to a draft release, then publishes it after all uploads succeed.
6. Builds Alpine-based Linux AMD64/ARM64 images using that release's installer, pushes the versioned GHCR image, verifies container startup, and promotes stable images to `latest`.

Prerelease tags such as `v0.3.0-rc.1` are marked as prereleases and do not replace the latest stable download or image. An existing public release is not overwritten by a rerun; a failed draft can be completed by rerunning the workflow. If only the container job fails after release publication, rerun **failed jobs** rather than the entire workflow.

Container publication uses the repository's `GITHUB_TOKEN` with `packages: write`. The published GHCR images are an additional download option; the default Compose file builds locally from the standalone Dockerfile. GitHub may make a newly created package private; set the **apptrail** container package to **Public** in its package settings once to allow anonymous image pulls.

To inspect the packages locally before tagging:

```sh
npm run build --prefix web
bash scripts/release.sh v0.1.0
```

This writes to a new `release-dist/` directory. The packaging script runs on Linux with Go, `tar`, `zip`, and `sha256sum`. It refuses to overwrite an existing output directory.

## Deploy GitHub Pages

The **Documentation** workflow builds `site/` and deploys it with GitHub Pages whenever relevant files change on `main`. It can also be run manually from Actions.

Repository settings must use **Pages → Source → GitHub Actions**. The workflow uses GitHub's Pages URL/base-path outputs, so the generated links and assets stay aligned with the deployment.

`install.sh`, `compose.yaml`, and `dockerfile.txt` are generated into the site from their root source files during the Astro build. Public downloads and release attachments share those same sources.

For a custom domain, configure it in GitHub Pages and its DNS provider. For a local custom-base build, set `SITE_URL` and `SITE_BASE` when running `npm run build --prefix site`.

## Scope

The initial release focuses on the registry, Docker/Traefik discovery, metadata, one owner account, and private/public dashboard pages. Provider expansion and richer integrations are tracked separately from the working MVP. See the [PRD](https://github.com/samishal1998/apptrail/blob/main/Apptrail_PRD.md) for the longer-term design.
