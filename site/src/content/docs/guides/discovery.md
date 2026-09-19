---
title: Apps & discovery
description: Connect Docker, Caddy, or Traefik and understand Apptrail's stable application registry.
---

## Supported providers

Apptrail v0.3 supports **Docker**, **Caddy**, and **Traefik** API providers, plus manual application entries. Docker discovery also reads Traefik labels without needing a separate Traefik API connection.

Docker discovery uses API **v1.44**, requiring **Docker Engine 25 or newer** or a compatible API. Nginx, Apache, direct configuration-file providers, and Docker event watching are later milestones.

## Connect a provider

In **Providers → Connect provider**, select **Docker**, **Caddy**, or **Traefik**, enter that API's base endpoint, and run **Scan now**. Endpoints are configured explicitly; Apptrail does not scan your network or autodetect administration ports. Supported endpoint forms include:

```text
unix:///var/run/docker.sock
https://docker-api.example.com
http://127.0.0.1:2019
https://traefik.example.com
```

A provider reports its last successful scan, observation count, and connection/parse diagnostics. Enabled providers are scanned on a five-minute ticker. Slow network work can extend a busy cycle.

The type is fixed once a provider is created. Its name, endpoint, authorization-file path, and enabled state can be edited. Existing installations upgrade their stored providers as Docker providers, preserving IDs, observations, accounts, overrides, and dashboards. Back up your data before upgrading; the new provider fields require a schema migration.

### Docker access

The application needs read access to:

```text
GET /v1.44/containers/json?all=true
```

Use a restricted Docker API proxy where available. Direct Docker socket access is highly privileged: mounting the socket with `:ro` does **not** restrict which Docker API operations a process could request.

For direct socket use, the account running Apptrail must already have access to that socket. In a container, mount it explicitly and supply the socket's host group ID using `group_add`. Apptrail does not modify socket permissions. HTTPS uses the system CA roots. Authorization headers are supported through a file; custom CA configuration, client certificates, and other authentication header names are not currently provider settings.

## Container discovery access

The [two-file Compose installation](../installation/#docker-compose) builds Apptrail's image locally from its Dockerfile and the released CLI installer. **Build context is not runtime discovery access:** putting proxy configuration next to the Dockerfile does not make it available inside the running container. Configure the relevant API connection or an explicit runtime mount.

| Source | Access needed | Config-file mount needed? | Apptrail v0.3 support |
| --- | --- | --- | --- |
| Docker containers and Traefik Docker labels | Docker API through a mounted socket or restricted HTTP(S) proxy | No Caddyfile, Traefik file, or application Compose file | Available |
| Traefik's active HTTP routers/services | Traefik's enabled, protected HTTP API | No | Available |
| Caddy's active configuration | Caddy admin API, using its native JSON configuration | No | Available |
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

:::note[Explicit API connections]
Choose the correct provider type. Apptrail reads the configured API; it does not enable the proxy's API, change proxy configuration, or scan for open ports. The private-network probe setting applies to app metadata fetching, not these owner-configured provider connections.
:::

### Caddy: active JSON configuration

Select **Caddy**, then enter an admin base endpoint such as `http://127.0.0.1:2019` on the same host, `http://caddy:2019` on a suitably configured private container network, or `unix:///run/caddy/admin.sock` for a readable admin socket. Do not append `/config/` yourself. Caddy's default listener is **`localhost:2019`**, unless changed or disabled by configuration. Apptrail reads:

```http
GET /config/
```

This returns Caddy's active native JSON configuration. Apptrail walks nested `subroute` handlers and discovers `reverse_proxy` and `file_server` routes with concrete hostnames and supported paths. Caddyfile `handle_path` routes work through their adapted JSON. TLS policies and supported automatic-HTTPS settings determine the scheme; listener addresses provide ports. A Caddyfile mount is unnecessary.

Literal host/path matchers and a terminal path wildcard such as `/photos/*` are supported. Host wildcards, dynamic placeholders, other request matcher types, and non-TCP or port-range listeners are reported as unresolved. Redirect-only and arbitrary response handlers are not treated as applications. Missing hostnames are never guessed from the admin endpoint's address.

In separate containers, the default loopback listener is not reachable from Apptrail. Configure an explicitly reachable private listener or a permissioned shared Unix socket. Caddy's admin API can also **change configuration and stop the server**; give Apptrail a restricted read-only access path rather than publicly publishing port `2019`. Apptrail itself only sends GET requests. An authorization file is useful when a protected access proxy fronts the admin API; it does not enable authentication in Caddy by itself.

See the [Caddy admin API reference](https://caddyserver.com/docs/api).

### Traefik: inspect active routers and services

Select **Traefik**, then enter the base URL serving its protected API, for example `https://traefik.example.com`. Include a configured API base-path prefix, but not `/dashboard/` or a complete `/api/...` path. Traefik has a **read-only inspection API**, also used by its dashboard. Apptrail reads:

```http
GET /api/http/routers
GET /api/http/services
GET /api/entrypoints
```

These expose active routing information from enabled providers, including file-backed configuration. Apptrail derives launch URLs from enabled HTTP routers, their entrypoints, and TLS settings, and reads available upstream health from services. Docker and API observations with the same normalized URL reconcile into the same application.

Rules can combine literal `Host`, `Path`, and `PathPrefix` matchers with `&&`, `||`, and parentheses. Unsupported matchers such as `HostRegexp`, negation, and request-header constraints produce unresolved entries instead of guessed URLs. Internal API/no-op routers are ignored, and disabled routers are excluded with diagnostics.

Traefik paginates its API. Apptrail requests one bounded page with `per_page=10001`, accepts at most 10,000 records per endpoint, and rejects a response that indicates more pages. A truncated or failed scan never marks previously observed apps missing. Access proxies must preserve the query parameters and pagination headers.

The API must be enabled and exposed deliberately, typically through a protected router using `api@internal`. Keep it on a restricted network with appropriate access controls; do not enable an unprotected public API just for discovery. **The existing Docker-label integration does not require enabling this API at all.**

See the [Traefik API and dashboard reference](https://doc.traefik.io/traefik/reference/install-configuration/api-dashboard/).

### Protected APIs

Expand **Authentication (optional)** in the provider form and enter an absolute **Authorization file** path on the Apptrail server. The file contains one complete header value, for example:

```text
Bearer YOUR_READ_ONLY_TOKEN
```

For Basic authentication, use `Basic ` followed by the Base64 encoding of `username:password`. Only the file path is stored in provider settings. Credentials are read again on every scan, so rotating the file takes effect without recreating the provider. The file must be a readable regular file of at most 8 KiB.

For a container, mount your file read-only and enter the **container** path in the form. For example, add to your Compose override:

```yaml
services:
  apptrail:
    volumes:
      - type: bind
        source: ./proxy-authorization
        target: /run/secrets/proxy-authorization
        read_only: true
        bind:
          create_host_path: false
```

Create the host file first and grant the Apptrail process read access. In this example, enter `/run/secrets/proxy-authorization`. Do not put credentials in the endpoint URL or labels. Provider requests reject redirects, avoiding forwarding the authorization value to another endpoint. API response bodies and authorization values are not copied into connection-error diagnostics.

### Port inference and limits

The **management endpoint** is always explicitly configured. Application hostnames, paths, TLS, and listener ports are inferred from the API data. A Docker host port mapping, external load balancer, or NAT can make the real public port different from that listener port; use the app's launch-URL override in that case. The management API's port is never reused as an application port.

API responses are capped at 8 MiB, individual requests at 12 seconds, and a provider scan at a 45-second network deadline. Route expansion is bounded to 10,000 entries and nesting to 32 levels. Caddy health initially remains unknown until app metadata/health probing supplies it; reading configuration is not itself an availability check.

### File mounts as a fallback

If API access is unavailable, a future file-based provider would need explicit read-only mounts. This is an **illustrative future mount layout**, not a working provider configuration in v0.3:

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

Mount only the configuration required for discovery, not certificate stores, private keys, or unrelated secrets. File snapshots can differ from the running proxy's active state; the implemented Caddy/Traefik providers use their APIs instead.

When an API is unavailable or a route cannot be resolved, add the app manually, override its launch URL, or give its Docker container an explicit `apptrail.url` label.

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

The Docker-label integration uses the same literal `Host`, `Path`, and `PathPrefix` rule support as the Traefik API provider. For example:

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

If another provider still observes an app, missing sources cannot override its current facts. If all infrastructure sources are missing, last-known fields remain available. Owner overrides always take precedence.
