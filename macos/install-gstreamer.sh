#!/bin/bash
# Downloads GStreamer's official macOS runtime and unpacks the parts
# TorrServer uses into <root>/<version>, for the Moviestracker app.
#
#   install-gstreamer.sh <root>    install (replaces older versions)
#   install-gstreamer.sh --size    the download's size in bytes, for progress
#   install-gstreamer.sh --version the GStreamer version it installs
#
# The runtime's libraries find each other relative to themselves, so it runs
# from any folder: no /Library/Frameworks, no admin password. The download is
# kept in <root>/.download while it runs, so the app can show progress.
set -euo pipefail

# The GStreamer this Moviestracker is tested with. Update all lines together.
gst_version="1.28.7"
gst_url="https://gstreamer.freedesktop.org/data/pkg/osx/$gst_version/gstreamer-1.0-$gst_version-universal.pkg"
gst_sha256="529fdf4a4027d942e59b5b3564f6400adaa008f63ce5f3fed4ffe35d73911994"
gst_size=153594157

# The parts TorrServer's pipelines need: demuxers, parsers, x264, libav
# (AAC, AC3, DTS), Apple's hardware codecs, and the HTTP source from "net".
parts="base-system-1.0 base-crypto gstreamer-1.0-core gstreamer-1.0-playback
gstreamer-1.0-codecs gstreamer-1.0-codecs-restricted gstreamer-1.0-codecs-gpl-restricted
gstreamer-1.0-libav gstreamer-1.0-system"

case "${1:-}" in
  --size) echo "$gst_size"; exit 0 ;;
  --version) echo "$gst_version"; exit 0 ;;
esac
root="${1:?usage: install-gstreamer.sh <root> | --size | --version}"

# Tests use a local installer.
url="${MT_GST_URL:-$gst_url}"
sum="${MT_GST_SHA256:-$gst_sha256}"

fail() {
  echo "install-gstreamer: $*" >&2
  exit 1
}

mkdir -p "$root"
work="$root/.download"
rm -rf "$work"
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM
mkdir -p "$work"

# The unpacked installer and the result both exist for a moment.
free_kb="$(df -Pk "$root" | awk 'NR == 2 { print $4 }')"
if [ "$free_kb" -lt $((900 * 1024)) ]; then
  fail "not enough free disk space: GStreamer needs about 900 MB while it installs"
fi

# GStreamer's server sometimes drops connections: resume and retry.
pkg="$work/gstreamer.pkg"
tries=0
until curl -fsSL --http1.1 --retry 5 --retry-all-errors --connect-timeout 20 -C - -o "$pkg" "$url"; do
  tries=$((tries + 1))
  [ "$tries" -lt 20 ] || fail "could not download $url"
  sleep 3
done
got="$(shasum -a 256 "$pkg" | awk '{ print $1 }')"
[ "$got" = "$sum" ] || fail "the download's checksum is $got, not $sum"

pkgutil --expand-full "$pkg" "$work/x" >/dev/null
rm -f "$pkg"
out="$work/$gst_version"
mkdir -p "$out"
for part in $parts; do
  payload="$(find "$work/x" -maxdepth 2 -type d -path "*/$part-[0-9]*.pkg/Payload" | head -n 1)"
  [ -n "$payload" ] || fail "the installer has no $part"
  ditto "$payload" "$out"
done
# From "net" only its libraries and the HTTP source plugin.
net="$(find "$work/x" -maxdepth 2 -type d -path "*/gstreamer-1.0-net-[0-9]*.pkg/Payload" | head -n 1)"
[ -n "$net" ] || fail "the installer has no gstreamer-1.0-net"
ditto "$net" "$work/net"
mv "$work/net/lib/gstreamer-1.0" "$work/net-plugins"
mkdir "$work/net/lib/gstreamer-1.0"
mv "$work/net-plugins/libgstsoup.dylib" "$work/net/lib/gstreamer-1.0/"
ditto "$work/net" "$out"
[ -f "$out/lib/libgstreamer-1.0.0.dylib" ] || fail "the installer has no libgstreamer"

# Swap in the new version, then drop older ones.
rm -rf "${root:?}/$gst_version"
mv "$out" "$root/$gst_version"
for old in "$root"/*; do
  [ "$old" = "$root/$gst_version" ] || rm -rf "$old"
done
echo "$root/$gst_version"
