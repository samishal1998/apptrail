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

## Container discovery access

The [two-file Compose installation](../installation/#docker-compose) builds Apptrail's image locally from its Dockerfile and the released CLI installer. **Build context is not runtime discovery access:** putting proxy configuration next to the Dockerfile does not make it available inside the running container. Configure the relevant API connection or an explicit runtime mount.

| Source | Access needed | Config-file mount needed? | Apptrail v0.2 support |
| --- | --- | --- | --- |
| Docker containers and Traefik Docker labels | Docker API through a mounted socket or restricted HTTP(S) proxy | No Caddyfile, Traefik file, or application Compose file | Available |
| Traefik's active HTTP routers/services | Traefik's enabled, protected HTTP API | No | Planned |
| Caddy's active configuration | Caddy admin API, preferably its native JSON configuration | No | Planned |
| Caddyfile or Traefik file-provider configuration | Read-only bind mounts of the configuration and included files | Yes | Planned |

### Use a Docker API proxy

Apptrail can use an existing restricted Docker API proxy without mounting `docker.sock` into Apptrail itself. Attach Apptrail to a network that can reach the proxy, then configure its **Docker** provider with an endpoint such as:

```text
http://docker-api:2375
```

Here, `docker-api` is an example proxy service name on a shared Docker network. The proxy must permit `GET /v1.44/containers/json?all=true`; it should not expose unrestricted daemon access. A proxy container that uses the local Docker socket still needs its own socket access.

### Mount the Docker socket directly

For a conventional Linux Docker host, add this to `compose.override.yaml` beside the downloaded `compose.yaml` and `Dockerfile`. Merge it into an existing override rather than replacing your other settings:

```yaml
services:
  apptrail:
    group_add:
      - "${DOCKER_GID:?Set DOCKER_GID to the Docker socket group ID}"
    volumes:
      - type: bind
        source: /var/run/docker.sock
        target: /var/run/docker.sock
        read_only: true
        bind:
          create_host_path: false
```

Set the socket's numeric group ID and recreate the container:

```sh
export DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
docker compose up --build -d
```

Compose automatically merges `compose.override.yaml`. The original `/data` volume is retained, and Apptrail still runs as UID `10001` with the socket group added. Then configure **Providers → Connect provider** with `unix:///var/run/docker.sock` and choose **Scan now**.

This socket path and `stat` command are Linux-specific. Rootless Docker and Docker Desktop can use different socket paths and permissions; adapt the bind source and group to your installation or use a reachable API proxy. A read-only socket mount does not make the Docker API read-only.

### Container networking

- `localhost` inside Apptrail refers to the Apptrail container, not the host or a Caddy/Traefik container.
- Containers in different Compose projects need an explicitly shared network before their service names can resolve to one another.
- Sharing a network does not expose an API that listens only on loopback inside another container.
- Publishing a port on the host and joining a private Docker network are different choices. Discovery should use a protected internal endpoint where possible.

## Caddy and Traefik APIs

:::note[Planned integrations]
Caddy and direct Traefik API discovery are not implemented in Apptrail v0.2. The current provider endpoint field expects a **Docker API**, not a Caddy or Traefik API. Mounting their config files or enabling private-network metadata probes does not add those providers.
:::

### Caddy: active JSON configuration

Caddy has an administration API. Its default listener is **`localhost:2019`**, unless changed or disabled by configuration. A future Apptrail provider can read:

```http
GET /config/
GET /config/apps/http/servers
```

These return Caddy's active native JSON configuration, including routing and reverse-proxy handlers. Reading the active configuration avoids having to reconstruct it from Caddyfile imports, environment substitutions, or subsequent API changes. A Caddyfile mount is unnecessary for this approach.

In separate containers, the default loopback listener is not reachable from Apptrail. Access would need an explicitly reachable private listener or a permissioned shared Unix socket. Caddy's admin API can also **change configuration and stop the server**; an Apptrail integration should use a restricted read-only access path rather than publicly publishing port `2019`.

See the [Caddy admin API reference](https://caddyserver.com/docs/api).

### Traefik: inspect active routers and services

Traefik has a **read-only inspection API**, also used by its dashboard. It is not a configuration-writing control plane. Useful discovery endpoints include:

```http
GET /api/http/routers
GET /api/http/services
GET /api/entrypoints
GET /api/rawdata
```

These can expose active routing information from enabled providers, including file-backed configuration. A future API integration can inspect those routes without mounting Traefik's configuration files. A configured API base path changes the endpoint prefixes.

The API must be enabled and exposed deliberately, typically through a protected router using `api@internal`. Keep it on a restricted network with appropriate access controls; do not enable an unprotected public API just for discovery. **The existing Docker-label integration does not require enabling this API at all.**

See the [Traefik API and dashboard reference](https://doc.traefik.io/traefik/reference/install-configuration/api-dashboard/).

### File mounts as a fallback

If API access is unavailable, a future file-based provider would need explicit read-only mounts. This is an **illustrative future mount layout**, not a working provider configuration in v0.2:

```yaml
services:
  apptrail:
    volumes:
      - type: bind
        source: ./caddy-config
        target: /config/caddy
        read_only: true
        bind:
          create_host_path: false
      - type: bind
        source: ./traefik-config
        target: /config/traefik
        read_only: true
        bind:
          create_host_path: false
```

The host directories must exist and be readable by Apptrail's container user. A file provider would read container paths such as `/config/caddy/Caddyfile` or `/config/traefik/dynamic/apps.yaml`. Mount required imports/includes and preserve their path relationships; a top-level file alone may be insufficient. Traefik's static `traefik.yml` often only selects providers and entry points—the actual routes can live in separate dynamic files or Docker labels.

Mount only the configuration required for discovery, not certificate stores, private keys, or unrelated secrets. File snapshots can differ from the running proxy's active state, so the planned preference is API discovery when a suitable protected endpoint is available.

For now, add Caddy-routed or Traefik-file-routed apps manually, or give their Docker containers an explicit `apptrail.url` label so the existing Docker provider can discover them.

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
