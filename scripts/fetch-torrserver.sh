#!/usr/bin/env bash
# Downloads a pinned TorrServer file and refuses it unless its SHA-256
# matches torrserver.lock.
#
#   fetch-torrserver.sh [dest]            the GStreamer build for this machine
#   TS_TARGET=linux/arm64 fetch-torrserver.sh dest   … for another platform
#   fetch-torrserver.sh --license dest    TorrServer's licence at the pinned tag
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
lock="$here/torrserver.lock"
version="$(awk '$1 == "version" {print $2}' "$lock")"

if [ "${1:-}" = "--license" ]; then
  asset="LICENSE"
  dest="${2:?destination}"
  url="https://raw.githubusercontent.com/YouROK/TorrServer/$version/LICENSE"
else
  dest="${1:-$here/../bin/torrserver}"
  target="${TS_TARGET:-}"
  if [ -z "$target" ]; then
    case "$(uname -s)" in
      Darwin) os=darwin ;;
      Linux) os=linux ;;
      *) echo "unsupported OS: $(uname -s) (macOS and Linux only)" >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
      x86_64 | amd64) arch=amd64 ;;
      arm64 | aarch64) arch=arm64 ;;
      *) echo "unsupported CPU: $(uname -m)" >&2; exit 1 ;;
    esac
    target="$os/$arch"
  fi
  asset="TorrServer-gst-${target%/*}-${target#*/}"
  url="https://github.com/YouROK/TorrServer/releases/download/$version/$asset"
fi

want="$(awk -v a="$asset" '$1 == a {print $2}' "$lock")"
[ -n "$want" ] || { echo "no pinned checksum for $asset" >&2; exit 1; }

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
echo "Downloading TorrServer ${version} ${asset}…"
curl -fsSL --retry 3 -o "$tmp" "$url"

if command -v sha256sum >/dev/null; then
  got="$(sha256sum "$tmp" | awk '{print $1}')"
else
  got="$(shasum -a 256 "$tmp" | awk '{print $1}')"
fi
if [ "$got" != "$want" ]; then
  echo "checksum mismatch for $asset: got $got, want $want" >&2
  exit 1
fi

mkdir -p "$(dirname "$dest")"
if [ "$asset" = "LICENSE" ]; then
  install -m 0644 "$tmp" "$dest"
else
  install -m 0755 "$tmp" "$dest"
fi
echo "Installed $dest ($version)"
