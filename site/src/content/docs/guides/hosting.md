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

## Run as a user service on Linux

Create `~/.config/systemd/user/apptrail.service`:

```ini
[Unit]
Description=Apptrail self-hosted dashboard
After=network.target

[Service]
ExecStart=%h/.local/bin/apptrail -addr 0.0.0.0:8080 -data %h/.local/share/apptrail
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

Then run:

```sh
systemctl --user daemon-reload
systemctl --user enable --now apptrail
journalctl --user -u apptrail -f
```

The logs contain the first-run setup token. To run a user service after logging out, your host may need user lingering enabled with `loginctl enable-linger "$USER"`.

For Docker Compose, use its configured restart policy and named volume instead.

## HTTPS and reverse proxies

When a reverse proxy terminates HTTPS, set Apptrail's exact browser-facing origin:

```sh
APPTRAIL_ORIGIN=https://apptrail.example.com apptrail -addr 127.0.0.1:8080 -data ./data
```

This enables Secure session cookies and defines the accepted browser origin for mutations. Serve the frontend and API on the same origin. Forward the original Host header; Apptrail does not implicitly trust forwarded headers.

For systemd, add this under `[Service]`:

```ini
Environment=APPTRAIL_ORIGIN=https://apptrail.example.com
```

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

Stop the service and make a backup first. Run the installer again to replace the executable:

```sh
systemctl --user stop apptrail
curl -fsSL https://samishal1998.github.io/apptrail/install.sh | sh
~/.local/bin/apptrail --version
systemctl --user start apptrail
```

Keep your original data directory. Review release notes before upgrades; replacing a binary with an older version is not a database migration strategy. Restore a matching backup if a future schema change requires it.

## Owner account recovery

The owner can change their password in Settings. To recover a forgotten password, stop the instance and run the following in Bash against the same data directory:

```sh
read -rs -p 'New password: ' APPTRAIL_NEW_PASSWORD
export APPTRAIL_NEW_PASSWORD
~/.local/bin/apptrail -data "$HOME/.local/share/apptrail" -reset-password
unset APPTRAIL_NEW_PASSWORD
```

Restart normally afterward. All previous sessions are revoked; app and dashboard data are preserved.
