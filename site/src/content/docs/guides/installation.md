---
title: Installation
description: Install Apptrail on Linux, macOS, or Windows, or run it in a container.
---

Apptrail runs on your own machine. This documentation site is hosted on GitHub Pages; your application registry and owner account stay on your server.

## Linux and macOS

The installer supports **x86_64/AMD64** and **ARM64**, including Apple Silicon and 64-bit Raspberry Pi operating systems.

```sh
curl -fsSL https://samishal1998.github.io/apptrail/install.sh | sh
```

It downloads the latest stable release, checks its SHA256 checksum, verifies the executable's version, and atomically installs it at `~/.local/bin/apptrail`. It does not require `sudo`, start a background service, or modify your shell configuration.

Start Apptrail:

```sh
~/.local/bin/apptrail -addr 0.0.0.0:8080 -data "$HOME/.local/share/apptrail"
```

Open `http://localhost:8080`. For a remote server, use its LAN or Tailscale address, followed by `:8080`.

### Choose a version or installation directory

```sh
curl -fsSL https://samishal1998.github.io/apptrail/install.sh |
  sh -s -- --version v0.1.0 --dir "$HOME/bin"
```

`APPTRAIL_VERSION` and `APPTRAIL_INSTALL_DIR` are also supported. A custom directory must be an absolute path writable by your user. Prereleases require an explicit version; the default follows GitHub's latest stable release.

### Inspect before installing

```sh
curl -fsSL https://samishal1998.github.io/apptrail/install.sh -o install.sh
less install.sh
sh install.sh
```

The script requires `curl`, `tar`, `awk`, and either `sha256sum` or `shasum`, along with standard Unix tools. A checksum or download failure leaves an existing installation intact. Checksums protect download integrity; their trust comes from the same GitHub release and HTTPS connection.

## Windows

Download the **windows_amd64.zip** archive and `SHA256SUMS` from [GitHub Releases](https://github.com/samishal1998/apptrail/releases/latest). Compare the archive's hash with its entry in `SHA256SUMS`:

```powershell
Get-FileHash .\apptrail_v0.1.0_windows_amd64.zip -Algorithm SHA256
Expand-Archive .\apptrail_v0.1.0_windows_amd64.zip .\apptrail
cd apptrail
.\apptrail.exe -addr 0.0.0.0:8080 -data .\data
```

Substitute the version you downloaded. On Windows, configure Docker discovery through an HTTP(S) Docker API endpoint; Unix sockets and Windows named pipes are not supported there by the initial provider.

## Docker Compose

Clone the project and build its container:

```sh
git clone https://github.com/samishal1998/apptrail.git
cd apptrail
docker compose up --build -d
docker compose logs apptrail
```

Compose publishes port `8080` and stores data in the `apptrail-data` named volume. The image runs as UID/GID `10001`. Docker discovery access must be configured separately; the default container does not mount the host's Docker socket.

## Build from source

Requires **Node.js 22.12+** (24 recommended) and **Go 1.26.8+**. Go's automatic toolchain management can download the required compiler.

```sh
git clone https://github.com/samishal1998/apptrail.git
cd apptrail
npm ci --prefix web
npm run build --prefix web
go build -o apptrail .
./apptrail -data ./data
```

The built executable includes the frontend; Node.js is only needed at build time.

## Create your owner account

On its first start, Apptrail prints a setup token. It is also stored in the configured data directory as `setup-token`.

1. Open Apptrail in your browser.
2. Paste the setup token.
3. Choose your username and a password of at least 12 characters, up to 72 UTF-8 bytes.
4. Create your account.

The first account is the instance's single owner. Account setup closes once it exists. Next, [build your first dashboard](../quick-start/).
