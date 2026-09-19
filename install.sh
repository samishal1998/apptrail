#!/bin/sh
# Install a verified GitHub release. Keep execution inside main so a truncated
# curl response cannot execute a partially downloaded installer.
set -eu

main() {
  repo=samishal1998/apptrail
  version=${APPTRAIL_VERSION:-latest}
  install_dir=${APPTRAIL_INSTALL_DIR:-"${HOME:?HOME is not set}/.local/bin"}
  work=
  staged=
  trap 'if [ -n "$work" ]; then rm -rf "$work"; fi; if [ -n "$staged" ]; then rm -f "$staged"; fi' 0
  trap 'exit 130' INT
  trap 'exit 143' TERM

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version|--dir)
        [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '%s\n' "Missing value for $1" >&2; exit 2; }
        if [ "$1" = --version ]; then version=$2; else install_dir=$2; fi
        shift 2 ;;
      --help|-h)
        printf '%s\n' 'Usage: sh install.sh [--version vX.Y.Z] [--dir DIRECTORY]' \
          'Defaults: latest stable release, installed in $HOME/.local/bin.'
        return ;;
      *) printf 'Unknown argument: %s\n' "$1" >&2; exit 2 ;;
    esac
  done

  for cmd in curl tar uname mktemp awk chmod mkdir cp mv; do
    command -v "$cmd" >/dev/null 2>&1 || { printf 'Required command not found: %s\n' "$cmd" >&2; exit 1; }
  done
  if command -v sha256sum >/dev/null 2>&1; then checksum=sha256sum
  elif command -v shasum >/dev/null 2>&1; then checksum=shasum
  else printf '%s\n' 'Install sha256sum or shasum to verify the download.' >&2; exit 1
  fi

  case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) printf '%s\n' 'Supported systems: Linux and macOS. Windows ZIPs are available on GitHub Releases.' >&2; exit 1 ;; esac
  case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) printf '%s\n' 'Supported architectures: x86_64 and ARM64.' >&2; exit 1 ;; esac

  if [ "$version" = latest ]; then
    resolved=$(curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 120 --proto '=https' --proto-redir '=https' --output /dev/null --write-out '%{url_effective}' "https://github.com/$repo/releases/latest") || { printf '%s\n' 'Could not resolve the latest release. Check GitHub Releases or choose --version.' >&2; exit 1; }
    case "$resolved" in "https://github.com/$repo/releases/tag/"*) version=${resolved##*/} ;; *) printf '%s\n' 'Unexpected release redirect.' >&2; exit 1 ;; esac
  fi
  case "$version" in v*) ;; *) version=v$version ;; esac
  printf '%s\n' "$version" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$/ {valid=1} END {exit !valid}' || { printf '%s\n' 'Invalid version; use vX.Y.Z or vX.Y.Z-prerelease.' >&2; exit 2; }

  case "$install_dir" in /*) ;; *) printf '%s\n' 'The install directory must be an absolute path.' >&2; exit 2 ;; esac
  [ ! -d "$install_dir/apptrail" ] && [ ! -L "$install_dir/apptrail" ] || { printf '%s\n' 'The destination apptrail is a directory or symlink; choose another --dir.' >&2; exit 1; }
  umask 077
  work=$(mktemp -d "${TMPDIR:-/tmp}/apptrail-install.XXXXXX")
  archive="apptrail_${version}_${os}_${arch}.tar.gz"
  base="https://github.com/$repo/releases/download/$version"
  printf 'Downloading Apptrail %s for %s/%s…\n' "$version" "$os" "$arch"
  for file in "$archive" SHA256SUMS; do
    curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 300 --proto '=https' --proto-redir '=https' --output "$work/$file" "$base/$file"
  done
  expected=$(awk -v file="$archive" '$2==file {print $1; n++} END {if(n!=1) exit 1}' "$work/SHA256SUMS") || { printf '%s\n' 'Release checksum is missing or ambiguous.' >&2; exit 1; }
  [ "${#expected}" -eq 64 ] || { printf '%s\n' 'Invalid SHA256 checksum.' >&2; exit 1; }
  case "$expected" in *[!0-9a-fA-F]*) printf '%s\n' 'Invalid SHA256 checksum.' >&2; exit 1 ;; esac
  if [ "$checksum" = sha256sum ]; then actual=$(sha256sum "$work/$archive"); else actual=$(shasum -a 256 "$work/$archive"); fi
  actual=${actual%% *}
  [ "$actual" = "$expected" ] || { printf '%s\n' 'Checksum mismatch. The existing installation was not changed.' >&2; exit 1; }

  tar -xzf "$work/$archive" -C "$work" apptrail
  [ -f "$work/apptrail" ] && [ ! -L "$work/apptrail" ] || { printf '%s\n' 'Release does not contain a regular apptrail executable.' >&2; exit 1; }
  chmod 755 "$work/apptrail"
  reported=$("$work/apptrail" --version)
  [ "$reported" = "Apptrail $version" ] || { printf '%s\n' 'The downloaded executable reported an unexpected version.' >&2; exit 1; }
  mkdir -p "$install_dir"
  staged=$(mktemp "$install_dir/.apptrail.XXXXXX")
  cp "$work/apptrail" "$staged"
  chmod 755 "$staged"
  mv -f "$staged" "$install_dir/apptrail"
  staged=
  printf '\nInstalled %s to %s/apptrail\n' "$reported" "$install_dir"
  case ":${PATH:-}:" in *":$install_dir:"*) ;; *) printf 'Add %s to your PATH, or use the full path below.\n' "$install_dir" ;; esac
  printf '\nStart your server:\n  "%s/apptrail" -addr 0.0.0.0:8080 -data "$HOME/.local/share/apptrail"\n' "$install_dir"
  printf '\nBackground services (Apptrail 0.2+):\n  "%s/apptrail" service install\nGuide: https://samishal1998.github.io/apptrail/guides/services/\n' "$install_dir"
}

main "$@"
