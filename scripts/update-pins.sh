#!/usr/bin/env bash
# Moves the TorrServer and GStreamer pins to their latest stable upstream
# releases, once those are at least a week old (like Dependabot's cooldown):
#
#   scripts/torrserver.lock        TorrServer's version and the SHA-256 GitHub
#                                  publishes for each file, and its licence's
#   macos/install-gstreamer.sh     GStreamer's version, SHA-256 and size
#
# It prints one line per pin it moved ("TorrServer MatriX.145 → MatriX.146")
# and nothing when all are current. .github/workflows/pins.yml opens a pull
# request with the result; CI then builds and tests with the new versions.
#
# Needs curl and jq. GH_TOKEN, when set, is sent to GitHub's API.
# Tests replace upstream with PINS_GITHUB_API, PINS_GITHUB_RAW and
# PINS_GSTREAMER, the repository with PINS_ROOT, and the time with PINS_NOW.
set -euo pipefail

root="${PINS_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
github_api="${PINS_GITHUB_API:-https://api.github.com}"
github_raw="${PINS_GITHUB_RAW:-https://raw.githubusercontent.com}"
gstreamer="${PINS_GSTREAMER:-https://gstreamer.freedesktop.org/data/pkg/osx}"
now="${PINS_NOW:-$(date +%s)}"
min_age=$((7 * 24 * 3600))

lock="$root/scripts/torrserver.lock"
gst_script="$root/macos/install-gstreamer.sh"

fail() {
  echo "update-pins: $*" >&2
  exit 1
}

fetch() {
  curl -fsSL --retry 3 --connect-timeout 20 "$@"
}

sha256() {
  if command -v sha256sum >/dev/null; then
    sha256sum | awk '{print $1}'
  else
    shasum -a 256 | awk '{print $1}'
  fi
}

# old enough <unix time>: whether a release published then has waited a week.
old_enough() {
  [ $((now - $1)) -ge "$min_age" ]
}

update_torrserver() {
  local auth=() rel tag published pinned
  [ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GH_TOKEN")
  rel="$(fetch "${auth[@]}" -H "Accept: application/vnd.github+json" "$github_api/repos/YouROK/TorrServer/releases/latest")"
  tag="$(jq -r '.tag_name' <<<"$rel")"
  published="$(jq -r '.published_at | fromdateiso8601' <<<"$rel")"
  pinned="$(awk '$1 == "version" {print $2}' "$lock")"
  [ "$(jq -r '.prerelease' <<<"$rel")" = false ] || return 0
  [ "$tag" != "$pinned" ] || return 0
  old_enough "$published" || return 0

  # Every pinned file must be in the release, with GitHub's digest.
  local next="$lock.next" name digest
  cp "$lock" "$next"
  while read -r name; do
    digest="$(jq -r --arg n "$name" '.assets[] | select(.name == $n) | .digest // empty' <<<"$rel")"
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { rm -f "$next"; fail "TorrServer $tag has no SHA-256 for $name"; }
    set_value "$next" "$name" "${digest#sha256:}"
  done < <(awk '$1 ~ /^TorrServer-/ {print $1}' "$lock")
  set_value "$next" LICENSE "$(fetch "$github_raw/YouROK/TorrServer/$tag/LICENSE" | sha256)"
  set_value "$next" version "$tag"
  mv "$next" "$lock"
  echo "TorrServer $pinned → $tag"
}

# set_value <file> <key> <value>: the second word of the line starting with key.
set_value() {
  awk -v k="$2" -v v="$3" '$1 == k {$0 = k " " v} {print}' "$1" >"$1.tmp" && mv "$1.tmp" "$1"
}

# GStreamer's even minor versions are stable (1.28.x); odd ones (1.29.x) are
# development releases.
update_gstreamer() {
  local pinned latest pkg headers published sum size
  pinned="$(sed -n 's/^gst_version="\(.*\)"$/\1/p' "$gst_script")"
  latest="$(fetch "$gstreamer/" | grep -oE 'href="1\.[0-9]*[02468]\.[0-9]+/"' | sed -E 's/href="(.*)\/"/\1/' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)"
  [ -n "$latest" ] || fail "no GStreamer versions at $gstreamer/"
  [ "$latest" != "$pinned" ] || return 0
  [ "$(printf '%s\n%s\n' "$pinned" "$latest" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)" = "$latest" ] || return 0

  pkg="gstreamer-1.0-$latest-universal.pkg"
  headers="$(fetch -I "$gstreamer/$latest/$pkg" | tr -d '\r')"
  published="$(awk -F': ' 'tolower($1) == "last-modified" {print $2}' <<<"$headers" | jq -R 'strptime("%a, %d %b %Y %H:%M:%S GMT") | mktime')"
  size="$(awk -F': ' 'tolower($1) == "content-length" {print $2}' <<<"$headers")"
  [[ "$size" =~ ^[0-9]+$ ]] || fail "GStreamer $latest: no size for $pkg"
  old_enough "$published" || return 0
  sum="$(fetch "$gstreamer/$latest/$pkg.sha256sum" | awk -v p="$pkg" '$2 == p || $2 == "*" p {print $1}')"
  [[ "$sum" =~ ^[0-9a-f]{64}$ ]] || fail "GStreamer $latest: no SHA-256 for $pkg"

  sed -e "s/^gst_version=\".*\"$/gst_version=\"$latest\"/" \
    -e "s/^gst_sha256=\".*\"$/gst_sha256=\"$sum\"/" \
    -e "s/^gst_size=.*$/gst_size=$size/" "$gst_script" >"$gst_script.next"
  chmod +x "$gst_script.next"
  mv "$gst_script.next" "$gst_script"
  echo "GStreamer $pinned → $latest"
}

update_torrserver
update_gstreamer
