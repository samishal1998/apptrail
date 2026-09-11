---
title: Apps & discovery
description: Connect Docker, discover Traefik routes, and understand Apptrail's stable application registry.
---

## Supported providers

The initial release supports Docker containers, Traefik routing labels on those containers, and manual application entries.

Docker discovery uses API **v1.44**, requiring **Docker Engine 25 or newer** or a compatible API. Caddy, Nginx, Apache, file providers, and Docker event watching are later milestones.

## Connect a provider

In **Providers**, create an endpoint and run **Scan now**. Supported endpoint forms:

```text
unix:///var/run/docker.sock
https://docker-api.example.com
```

A provider reports its last successful scan, observation count, and connection/parse diagnostics. Enabled providers are scanned on a five-minute ticker. Slow network work can extend a busy cycle.

### Docker access

The application needs read access to:

```text
GET /v1.44/containers/json?all=true
```

Use a restricted Docker API proxy where available. Direct Docker socket access is highly privileged: mounting the socket with `:ro` does **not** restrict which Docker API operations a process could request.

For direct socket use, the account running Apptrail must already have access to that socket. In a container, mount it explicitly and supply the socket's host group ID using `group_add`. Apptrail does not modify socket permissions. Remote HTTPS endpoints use the system CA roots; client-certificate and custom authentication configuration are not part of this release.

## Apptrail labels

```yaml
services:
  photos:
    image: your-photo-app
    labels:
      apptrail.enable: "true"
      apptrail.id: photos
      apptrail.name: Photo library
      apptrail.description: Our favorite memories, all together.
      apptrail.url: https://photos.example.com
      apptrail.category: Media
      apptrail.icon: https://photos.example.com/icon.png
```

Use `apptrail.enable: "false"` to exclude a container. Add `apptrail.url` when Apptrail cannot determine the public entry point. Published container ports alone are not assumed to be browser-facing applications.

Optional labels:

```yaml
labels:
  apptrail.manifest: /.well-known/apptrail.json
  apptrail.health.url: /health
  apptrail.health.method: GET
```

Never put passwords, tokens, or other secrets in labels.

## Traefik routes

Apptrail recognizes a `Host` rule with an optional `PathPrefix` conjunction:

```yaml
labels:
  traefik.http.routers.photos.rule: Host(`apps.example.com`) && PathPrefix(`/photos`)
  traefik.http.routers.photos.tls: "true"
```

This produces `https://apps.example.com/photos`. Router TLS labels determine HTTPS. Unsupported expressions are reported as unresolved instead of being guessed; an explicit `apptrail.url` is the simplest override.

## Stable identity

Observations are associated using explicit `apptrail.id` values, normalized launch URLs, and stable provider-native/Compose identities. Recreating a container should not create a new dashboard entry or erase its user preferences.

URL normalization preserves base paths: `/photos` and `/notes` are different applications. Conflicting explicit identities cause the scan to fail transactionally and retain the previous snapshot, so an ambiguous match does not silently combine apps.

Manual entries also participate in URL identity. Later infrastructure discovery can enrich the same application.

## Missing apps and failed scans

- **Failed scan:** previous observations remain present; inspect the provider's diagnostics.
- **Successful scan with an absent app:** the app becomes **missing**.
- **Absent for 24 hours:** it is displayed as **stale**.
- **Forget app:** removes it and its dashboard placements. A later scan can discover it again.

Nothing is automatically deleted. Pausing a provider stops future scans. Removing a provider retains its observations as missing.
