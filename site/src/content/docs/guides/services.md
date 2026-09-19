---
title: Background services
description: Install and manage Apptrail with systemd, launchd, or Windows Services.
---

Apptrail **0.2 and newer** can install itself into the operating system's service manager. Install the executable in a permanent location first. Stop an existing foreground instance before adopting it as a service.

## Quick start

```sh
apptrail service install
apptrail service status
```

Installation enables automatic startup and starts the service immediately. The CLI prints the persistent data directory and setup-token location.

To preserve an existing account and registry, explicitly pass that instance's data directory:

```sh
apptrail service install -data /absolute/path/to/existing/data
```

Other install options:

```sh
apptrail service install \
  -addr 0.0.0.0:8080 \
  -data /absolute/path/to/data \
  -origin https://apptrail.example.com
```

`-origin` is optional. It defaults to `APPTRAIL_ORIGIN` at installation time and is saved in the service arguments. The listen address and data directory are also saved; changing your shell environment later does not reconfigure an installed service.

## Lifecycle commands

```sh
apptrail service start
apptrail service stop
apptrail service restart
apptrail service status
apptrail service uninstall
```

Uninstall stops the service and removes its registration. **The executable, account, database, and logs are retained.** Start/stop/restart/status do not accept configuration flags; use install to configure a service.

## Linux — systemd

Run the commands as your normal user, **without sudo**. Apptrail creates:

```text
~/.config/systemd/user/apptrail.service
```

`XDG_CONFIG_HOME` is respected. The default data directory is `$XDG_DATA_HOME/apptrail`, falling back to `~/.local/share/apptrail`.

The service starts with your user manager, restarts after failures, and shuts down gracefully on stop. For startup before login and continued operation after logout:

```sh
loginctl enable-linger "$USER"
```

View logs with:

```sh
journalctl --user -u apptrail -f
```

Running `service install` again updates an Apptrail-managed unit and restarts it. A user-written unit without Apptrail's management marker is not overwritten. Back up and remove an existing custom unit before adopting CLI management, or continue managing it with `systemctl`.

On Linux distributions without systemd, run Apptrail in the foreground under your existing supervisor, or use Docker Compose.

## macOS — launchd

Run installation as your normal user from a logged-in desktop session. Apptrail creates this LaunchAgent:

```text
~/Library/LaunchAgents/io.github.samishal1998.apptrail.plist
```

It starts when you log in and restarts after failures. The default data directory is:

```text
~/Library/Application Support/Apptrail
```

View `apptrail.log` in the configured data directory. Re-running install updates an Apptrail-managed definition and restarts the agent. This is a per-user LaunchAgent, not a system-wide LaunchDaemon that starts before login.

If bootstrap fails from an SSH-only session, sign in to the macOS desktop and retry. User-written definitions are preserved rather than overwritten.

## Windows — Windows Services

Open an **Administrator PowerShell** terminal and run:

```powershell
.\apptrail.exe service install
.\apptrail.exe service status
```

Apptrail installs an automatic Windows Service named **Apptrail**, running under the built-in **LocalService** account. It copies the executable to `%ProgramFiles%\Apptrail\apptrail.exe`, defaults data to `%ProgramData%\Apptrail`, and grants LocalService access to the executable and data directory.

You can also manage it in the Windows **Services** application. Logs go to `apptrail.log` inside the data directory. When stopped through Windows Services, Apptrail shuts down its HTTP server and database gracefully.

To change arguments or upgrade the service executable, use an Administrator terminal:

```powershell
& "$env:ProgramFiles\Apptrail\apptrail.exe" service uninstall
# From the newly downloaded release directory:
.\apptrail.exe service install -data "$env:ProgramData\Apptrail"
```

Reuse your custom `-data`, `-addr`, and `-origin` values if you configured them. The data directory is not deleted by uninstall.

## Upgrades and logs

On Linux and macOS, stop the service, replace the binary with the installer, then start it again. The executable must remain at the path stored during service installation.

Windows uses its copied executable, so uninstall/reinstall as described above. Make a consistent backup before any upgrade.

macOS and Windows file logs are append-only. For high-volume deployments, configure external log rotation. Linux uses journald's retention policy.

## Containers

Do not install systemd/launchd/Windows Services inside the Apptrail container. Its process runs in the foreground; Docker Compose's restart policy handles service lifecycle. See [container installation](../installation/#docker-compose).
