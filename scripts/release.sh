#!/usr/bin/env bash
set -euo pipefail

version=${1:?Usage: bash scripts/release.sh vX.Y.Z [output-directory]}
out=${2:-release-dist}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { printf 'Invalid release version: %s\n' "$version" >&2; exit 2; }
[[ -f web/dist/index.html ]] || { printf '%s\n' 'Build the frontend first: npm run build --prefix web' >&2; exit 1; }
[[ ! -e "$out" ]] || { printf 'Output directory already exists: %s\n' "$out" >&2; exit 1; }
mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  binary=apptrail
  [[ "$os" != windows ]] || binary=apptrail.exe
  printf 'Building %s %s\n' "$version" "$target"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w -X main.version=$version" -o "$work/$binary" .
  if [[ "$os" == windows ]]; then
    zip -q -j "$out/apptrail_${version}_${os}_${arch}.zip" "$work/$binary" README.md
  else
    tar -czf "$out/apptrail_${version}_${os}_${arch}.tar.gz" -C "$work" "$binary" -C "$PWD" README.md
  fi
done
cp install.sh "$out/install.sh"
cp Dockerfile compose.yaml "$out/"
# Hash names relative to the release directory so the same manifest works for downloaded assets.
for artifact in "$out"/*.tar.gz "$out"/*.zip "$out/install.sh" "$out/Dockerfile" "$out/compose.yaml"; do
  sum=$(sha256sum "$artifact")
  printf '%s  %s\n' "${sum%% *}" "${artifact##*/}"
done > "$out/SHA256SUMS"
