---
title: Troubleshooting
description: Resolve installation, Docker discovery, metadata, sign-in, and sharing issues.
---

## The installer cannot find a release

Check [GitHub Releases](https://github.com/samishal1998/apptrail/releases). The default installer follows the latest stable release; use `--version vX.Y.Z` for a specific version or prerelease.

Only Linux/macOS AMD64 and ARM64 are supported by the shell installer. Windows AMD64 uses a ZIP download. A 32-bit ARM operating system needs to be replaced with a supported 64-bit OS or a separately supported source build.

## `apptrail: command not found`

Run it using its full path:

```sh
~/.local/bin/apptrail --version
```

Or add `~/.local/bin` to your shell's `PATH`. The installer intentionally leaves shell configuration to you.

## Docker says permission denied

The default Compose file builds locally. If building fails before the installer runs, check access to the Docker daemon and the Alpine base image. If you explicitly use the published GHCR image instead, a pull-access error concerns registry authentication rather than application discovery.

The Apptrail process cannot access the configured socket. Check the actual account/group running the service and the socket's permissions. For a container, verify the socket mount and `group_add` configuration, or use a restricted Docker API proxy.

Avoid solving this by making the socket world-writable. A `:ro` mount alone does not restrict Docker API operations.

## A provider scan fails

Read its diagnostics in **Providers**. Common causes are an inaccessible socket, unreachable API endpoint, Docker versions older than Engine 25, invalid responses, or conflicting Apptrail IDs.

A failed scan retains previous observations. Fix the configuration and choose **Scan now**.

## A discovered app has no launch URL

Find it in **Discover**. Add an explicit `apptrail.url` label or edit its launch URL in Apptrail.

The initial Traefik parser supports `Host` and an optional `PathPrefix` conjunction. More complex expressions are not guessed. Container port mappings alone are not treated as public browser URLs.

## I mounted a Caddyfile or Traefik config, but nothing was discovered

Apptrail v0.2 does not yet implement those file providers or direct Caddy/Traefik API discovery. The current Traefik integration reads labels through the Docker API. Add an explicit `apptrail.url` label to the app's Docker container, or create a manual app entry.

Both proxies expose APIs that can avoid config mounts in future integrations. See [Caddy and Traefik APIs](../discovery/#caddy-and-traefik-apis) for endpoints and container networking requirements. `localhost` inside Apptrail is not the host or another container; sharing a network also does not make another container's loopback-only listener reachable.

## Metadata or icons are missing

1. Verify the launch/icon URL from the Apptrail host, not just your browser.
2. For LAN, localhost, or Tailscale targets, enable **Settings → Allow private-network probes**.
3. Choose **Refresh metadata** and inspect its diagnostics.

Responses larger than 1 MiB, excessive redirects, invalid TLS certificates, cross-origin automatic resource declarations, and blocked IP ranges can prevent enrichment. Pages requiring login may not expose useful public metadata. Explicit owner overrides are useful for these apps.

## My app did not appear on Overview

New discoveries appear in **Apps**. Add them to a page with **Choose apps**. This separation prevents new deployments from rearranging your dashboard.

If an app seems missing, check **Apps → Hidden** and its lifecycle/source details.

## Visitors cannot open a shared page

- Set the page's visibility to **Anyone**.
- Check the slug and use `/d/<slug>`.
- Add visible apps through **Choose apps**.
- Ensure visitors can reach your Apptrail host.

Publishing a page does not make its linked services public or grant visitors access to your LAN/Tailscale network. A private page is viewed through the signed-in owner workspace.

## Requests are rejected behind a proxy

Set `APPTRAIL_ORIGIN` to the exact external origin, including scheme and non-default port when applicable. Preserve the Host header and keep the UI/API on one origin.

If the origin is HTTPS, the browser must use HTTPS for its Secure session cookie. See [hosting and maintenance](../hosting/).

## Too many login attempts

Login and setup attempts are limited by source IP over a 15-minute window. Wait for that window to expire. Password recovery is available through the local CLI when you have access to the server.

## Still stuck?

For native service problems, run `apptrail service status`. Check journald on Linux or `apptrail.log` in the configured data directory on macOS/Windows. Linux needs an active systemd user manager; macOS installation needs a logged-in desktop session; Windows management requires an Administrator terminal. Reuse your original data directory when moving a foreground installation into a service.

[Open a GitHub issue](https://github.com/samishal1998/apptrail/issues) with your Apptrail version, operating system, relevant redacted diagnostics, and steps to reproduce. Leave setup tokens, cookies, passwords, and private infrastructure credentials out of the report.
