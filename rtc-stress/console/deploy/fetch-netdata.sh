#!/bin/sh
set -eu
version=2.13.0
checksum=bb0be18228b49c9c816b87193a070ed93defdd8e366368899e39cb7d005ca6f7
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
package="$root/.local/netdata/netdata-x86_64-v$version.gz.run"
mkdir -p "$(dirname "$package")"
if [ ! -f "$package" ]; then
  curl -fL --retry 3 -o "$package.tmp" "https://github.com/netdata/netdata/releases/download/v$version/netdata-x86_64-v$version.gz.run"
  mv "$package.tmp" "$package"
fi
if command -v sha256sum >/dev/null; then
  actual=$(sha256sum "$package" | cut -d ' ' -f 1)
else
  actual=$(shasum -a 256 "$package" | cut -d ' ' -f 1)
fi
[ "$actual" = "$checksum" ] || { echo 'Netdata SHA256 mismatch' >&2; exit 1; }
echo "Verified Netdata $version"
