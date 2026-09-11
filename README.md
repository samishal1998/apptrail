# Apptrail

Discover, organize, and launch your self-hosted apps. Go + React/TypeScript + SQLite, with the built frontend embedded in one executable.

**[Website & app guide](https://samishal1998.github.io/apptrail/)** · **[Downloads](https://github.com/samishal1998/apptrail/releases/latest)**

## Install a release

Linux and macOS (AMD64 or ARM64):

```sh
curl -fsSL https://samishal1998.github.io/apptrail/install.sh | sh
~/.local/bin/apptrail -data "$HOME/.local/share/apptrail"
```

The installer verifies SHA256 and the executable version, then installs atomically to `~/.local/bin`. It does not require root, edit your shell, or start a service. Pin a release or choose a different destination with `sh -s -- --version v0.1.0 --dir "$HOME/bin"`. Windows AMD64 ZIPs and manual downloads are on GitHub Releases.

Run `apptrail --version` to inspect the installed version. See the [installation guide](https://samishal1998.github.io/apptrail/guides/installation/) and [hosting guide](https://samishal1998.github.io/apptrail/guides/hosting/) for setup, services, upgrades, and backups.

## Run locally

Requires Node.js 22.12+ (24 recommended) and Go 1.26.8+. Go's automatic toolchain management can download the required Go version.

```sh
npm ci --prefix web
npm run build --prefix web
go build -o apptrail .
./apptrail -addr 0.0.0.0:8080 -data ./data
```

Open `http://localhost:8080`, or use this machine's LAN/Tailscale address. The first start prints a **setup token**, also stored in `data/setup-token`. Paste it into the setup screen and create the single owner account. Setup is closed after that account exists.

The server uses port **8080** by default. `-addr` and `-data` change the listen address and persistent directory. Keep the data directory on local persistent storage.

### Container

```sh
docker compose up --build -d
docker compose logs apptrail
```

The named volume contains the registry, account, sessions, and dashboard state. The image runs as UID/GID 10001 and does not have Docker access until you explicitly configure an endpoint/mount.

## First steps

1. **Providers → Connect provider:** choose a Docker Unix socket or HTTP(S) API endpoint, then **Scan now**.
2. **Apps:** inspect discovered applications or add manual entries. Favorite, rename, hide, and override icons here.
3. **Overview → Choose apps:** choose exactly which apps appear on that dashboard page. New discovery does not rearrange your curated page.
4. **Layout:** drag cards, or use the keyboard-accessible earlier/later buttons. Ordering persists independently for each page.
5. **Page settings:** rename a page, set its URL, and choose private or public. **New page** creates another dashboard over the same registry.
6. **Settings:** switch themes, update your password, or enable private-network metadata probes for LAN/Tailscale services.

Public pages live at `/d/<slug>`. Anonymous visitors receive only that page's visible app display fields, launch links, icons, and health. The global registry, providers, diagnostics, settings, and mutations require owner login. Hiding an app also removes it from public pages. Making a page private blocks subsequent anonymous API/icon requests; open public pages recheck every 30 seconds and when focused.

## Docker discovery

The initial provider reads the Docker **v1.44 API (Engine 25+)** and supports:

- Apptrail labels: `enable`, `id`, `name`, `description`, `category`, `icon`, `url`, `manifest`, `health.url`, and `health.method` under the `apptrail.*` namespace.
- Traefik `Host(...)` with an optional `&& PathPrefix(...)`, explicit router TLS settings, and stable Compose project/service identity.
- Docker health and container running state.
- Scheduled snapshots on a five-minute ticker, plus manual scans.

Complex Traefik rules are reported as unresolved; use `apptrail.url` for an explicit launch URL. Published ports alone are not treated as proof of a browser-facing URL. Containers without a resolved URL remain inspectable in **Discover**.

```yaml
labels:
  apptrail.id: photos
  apptrail.name: Photo library
  apptrail.url: https://photos.example.com
  apptrail.category: Media
```

A restricted Docker API proxy is preferable to direct socket access. The provider needs `GET /v1.44/containers/json?all=true`. Mounting `docker.sock` read-only does **not** make the Docker API read-only. For direct socket access, mount it explicitly and give the runtime user the socket's existing group; Apptrail does not alter host permissions.

Snapshots are reconciled transactionally. Failed scans and identity conflicts retain the previous observations. Successful scans mark absent apps missing; after 24 hours they are shown as stale. They are never automatically deleted. Forgetting an app removes its dashboard placements; discovery can recreate it on a later scan.

## Enrichment and persistence

Metadata refreshes extract HTML title/description, favicon links, web app manifest icons, and version-1 `/.well-known/apptrail.json` metadata. Supported manifest health requests are GET/HEAD. Relative Apptrail resource URLs resolve against the application's public URL; automatically followed manifest/icon declarations must remain on the same origin. Explicit owner icon overrides can point to another origin.

HTTP requests have timeouts, a 1 MiB body limit, a redirect limit, and DNS/IP validation at connection time, including after redirects. Private/LAN/loopback/Tailscale probes are opt-in. Link-local/cloud metadata and reserved address ranges remain blocked. Outbound requests do not inherit proxy credentials or execute JavaScript. Background enrichment uses four workers; total probe/icon concurrency is capped at eight.

Data lives in `apptrail.db` with SQLite WAL enabled. Observations, identity aliases, durable user overrides, dashboard ordering, owner credentials, and hashed session tokens are persisted separately. Field priority and source are inspectable from each app card. User overrides take precedence over future discovery.

For a simple consistent backup, stop Apptrail and copy its data directory. Do not copy only the database file while the server is running; committed changes may still be in the WAL.

## Authentication and reverse proxies

Passwords are bcrypt-hashed; passwords must contain 12–72 UTF-8 bytes. Login/setup attempts are rate-limited. Sessions expire after seven days and use HttpOnly, SameSite cookies. Mutation endpoints reject cross-origin browser requests.

For an HTTPS reverse proxy, configure the exact public origin:

```sh
APPTRAIL_ORIGIN=https://apptrail.example.com ./apptrail
```

This also enables Secure session cookies. Serve the UI and API on the same origin and preserve the original Host header. Forwarded headers are not implicitly trusted. Direct HTTP works for the Tailscale preview; use HTTPS when serving the application outside a trusted encrypted network.

### Recover the owner password

Stop the running instance, then run against the same data directory:

```sh
read -rs -p 'New password: ' APPTRAIL_NEW_PASSWORD
export APPTRAIL_NEW_PASSWORD
./apptrail -data ./data -reset-password
unset APPTRAIL_NEW_PASSWORD
```

Restart normally afterward. This changes the password and revokes all existing sessions; it preserves dashboards and applications.

## Development and checks

```sh
go test -race ./...
go vet ./...
npm run build --prefix web
```

For frontend hot reload, run the Go server on port 8080 and `npm run dev --prefix web`. Vite proxies `/api` to Go. Build the frontend again before creating a release executable.

The backend integration checks cover owner setup/login, password/session revocation, publication boundaries, hidden apps, stable Docker identity, snapshot rollback, HTTP probe/redirect limits, manifest enrichment, and SQLite restart persistence.

The visual implementation reuses the working pack's dark/light tokens and SVG icons. The canonical logo/background exports remain pending in the pack; the UI uses a text wordmark and decorative CSS scenery.

Caddy/Nginx/Apache/file providers, widgets, Docker event watching, and multiple user accounts remain later milestones from the PRD.

## Website and release maintenance

The public website and app guide use Astro Starlight in `site/`:

```sh
npm ci --prefix site
npm run dev --prefix site
npm run build --prefix site
```

The default local URL is `http://localhost:4321/apptrail/`. GitHub Pages is deployed by `.github/workflows/pages.yml` on relevant changes to `main`. Set repository **Pages → Source → GitHub Actions**. The Pages `install.sh` is generated from the root script, keeping one source of truth.

Tagged releases are built by `.github/workflows/release.yml`:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Use a new semantic version for each release. Tags with a prerelease suffix are marked as prereleases. The workflow verifies the project, builds five platform archives, uploads `SHA256SUMS` and the installer, and only then publishes the draft release. An already published release is never overwritten.

Local packaging and installer checks:

```sh
npm run build --prefix web
bash scripts/release.sh v0.1.0
node --test scripts/install.test.cjs
```

Packaging requires Linux with Go, `tar`, `zip`, and `sha256sum`, and writes to a new `release-dist/` directory. Generated binaries, local instance data, and build outputs are excluded from version control.
