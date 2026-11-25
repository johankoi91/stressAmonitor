#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
archive="$root/.local/go-sdk/agora-sdk-2.6.6-linux.zip"
mkdir -p "$(dirname "$archive")"
command -v unzip >/dev/null || { echo 'Install unzip to extract the SDK.' >&2; exit 1; }
if [[ ! -f "$archive" ]]; then
  curl --fail --location --retry 3 'https://download.agora.io/sdk/release/shengwang_rtc_sdk.v4.6.31.135_28737_SERVER_20260930_1604_1321736_external-20260824_1848-3a.zip' -o "$archive.tmp"
  mv "$archive.tmp" "$archive"
fi
expected=902126d5518add1594ef480700c0503a0d11b3964dd22f0a5ee3ddc2b5204259
if command -v sha256sum >/dev/null; then
  actual="$(sha256sum "$archive" | cut -d ' ' -f 1)"
else
  actual="$(shasum -a 256 "$archive" | cut -d ' ' -f 1)"
fi
[[ "$actual" == "$expected" ]] || { echo 'SDK archive checksum mismatch' >&2; exit 1; }
destination=${1:-"$root/backend/third_party/agora-go/agora_sdk"}
temporary=$(mktemp -d "${TMPDIR:-/tmp}/rtc-sdk.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
unzip -q "$archive" 'agora_sdk/*' -d "$temporary"
mkdir -p "$destination"
cp -R "$temporary/agora_sdk/." "$destination/"
echo 'Verified and extracted Agora SDK 2.6.6'
