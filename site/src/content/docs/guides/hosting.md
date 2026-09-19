---
title: Hosting & maintenance
description: Run Apptrail as a service, configure HTTPS, back up SQLite data, and upgrade safely.
---

## Listen address and storage

```sh
apptrail -addr 0.0.0.0:8080 -data "$HOME/.local/share/apptrail"
```

`-addr` selects the interface and port. `0.0.0.0` listens on all interfaces, including a Tailscale interface. Use `127.0.0.1:8080` when only a reverse proxy on the same host should connect.

`-data` selects the persistent directory. Use local storage and retain this directory across upgrades. It contains the SQLite database, WAL files when active, and first-run setup token.

## Run as a background service

```sh
apptrail service install
apptrail service status
```

Apptrail uses systemd user services on Linux, launchd LaunchAgents on macOS, and Windows Services on Windows. Windows setup requires an Administrator terminal; Linux/macOS setup runs as your normal user. See [background services](../services/) for data paths, logs, startup behavior, and lifecycle commands.

For Docker Compose, use its configured restart policy and named volume instead.

## HTTPS and reverse proxies

When a reverse proxy terminates HTTPS, set Apptrail's exact browser-facing origin:

```sh
APPTRAIL_ORIGIN=https://apptrail.example.com apptrail -addr 127.0.0.1:8080 -data ./data
```

This enables Secure session cookies and defines the accepted browser origin for mutations. Serve the frontend and API on the same origin. Forward the original Host header; Apptrail does not implicitly trust forwarded headers.

For CLI-managed services on Linux/macOS, reinstall the configuration with the exact origin and your existing data path:

```sh
apptrail service install -addr 127.0.0.1:8080 -data /absolute/path/to/data -origin https://apptrail.example.com
```

Windows service configuration changes require uninstall/reinstall. With Compose, set `APPTRAIL_ORIGIN=https://apptrail.example.com` in the `.env` file and recreate the container with `docker compose up -d`.

An example Caddy proxy:

```text title="Caddyfile"
apptrail.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Direct HTTP works over a trusted encrypted Tailscale connection. Use HTTPS for deployment outside that environment.

## Back up your workspace

For a simple, consistent backup:

1. Stop Apptrail.
2. Copy the complete configured data directory to your backup location.
3. Start Apptrail again.

Do not copy only `apptrail.db` while the server is running: committed transactions can still be in its WAL. Backups contain your password hash, active session records, and infrastructure details; keep them private.

To restore, stop Apptrail, restore that directory, and restart with the same `-data` path.

## Upgrade

On Linux/macOS, stop the service and make a backup first. Run the installer again to replace the executable at the same path:

```sh
apptrail service stop
curl -fsSL https://samishal1998.github.io/apptrail/install.sh | sh
~/.local/bin/apptrail --version
~/.local/bin/apptrail service start
```

Keep your original data directory. Review release notes before upgrades; replacing a binary with an older version is not a database migration strategy. Restore a matching backup if a future schema change requires it.

For Windows, follow the [service executable upgrade steps](../services/#windows--windows-services). For Docker Compose, run `docker compose pull` and `docker compose up -d`, preserving the existing project name and data volume.

## Owner account recovery

The owner can change their password in Settings. To recover a forgotten password, stop the instance and run the following in Bash against the same data directory:

```sh
read -rs -p 'New password: ' APPTRAIL_NEW_PASSWORD
export APPTRAIL_NEW_PASSWORD
~/.local/bin/apptrail -data /absolute/path/to/your/data -reset-password
unset APPTRAIL_NEW_PASSWORD
```

Restart normally afterward. All previous sessions are revoked; app and dashboard data are preserved.
