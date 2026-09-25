#!/usr/bin/env bash
# Builds a release: for each Linux target an archive with Moviestracker, the
# pinned and verified TorrServer, the licences and install.sh; on a Mac also
# the Moviestracker DMG (scripts/macapp.sh); then checksums.txt over them all.
#
#   scripts/release.sh v0.1.0
#
# Environment:
#   TARGETS          space-separated Linux os/arch list (default: linux/amd64 linux/arm64)
#   MACAPP           "no" skips the DMG (default: built when running on macOS)
#   DIST             output folder (default: dist)
#   TORRSERVER_FROM  folder with TorrServer-gst-<os>-<arch> and LICENSE already
#                    fetched (skips the download; used by tests and offline builds)
#   MT_SHARED_TMDB_KEY  the shared TMDB key built in, used when a user has no key
#                    of their own (default: the git-ignored .tmdb-shared-key file)
set -euo pipefail

version="${1:?usage: release.sh <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
dist="${DIST:-$root/dist}"
targets="${TARGETS:-linux/amd64 linux/arm64}"
ts_version="$(awk '$1 == "version" {print $2}' "$root/scripts/torrserver.lock")"
# shellcheck source=scripts/shared-tmdb-key.sh
. "$root/scripts/shared-tmdb-key.sh"

mkdir -p "$dist"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# torrserver_file copies (or downloads and verifies) a pinned TorrServer file.
torrserver_file() { # <asset name> <dest> [target]
  if [ -n "${TORRSERVER_FROM:-}" ]; then
    install -m "$([ "$1" = LICENSE ] && echo 0644 || echo 0755)" "$TORRSERVER_FROM/$1" "$2"
  elif [ "$1" = LICENSE ]; then
    "$root/scripts/fetch-torrserver.sh" --license "$2"
  else
    TS_TARGET="$3" "$root/scripts/fetch-torrserver.sh" "$2"
  fi
}

archives=()
for target in $targets; do
  os="${target%/*}"
  arch="${target#*/}"
  if [ "$os" != linux ]; then
    echo "$target: archives are for Linux; macOS gets the DMG from scripts/macapp.sh" >&2
    exit 1
  fi
  name="moviestracker_${version}_${os}_${arch}"
  dir="$work/$name"
  mkdir -p "$dir/licenses"
  echo "Building $name"

  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -C "$root" -trimpath \
    -ldflags "-s -w -X main.version=$version -X main.sharedTMDBKey=$shared_tmdb_key" -o "$dir/moviestracker" ./cmd/server
  torrserver_file "TorrServer-gst-$os-$arch" "$dir/torrserver" "$target"
  torrserver_file LICENSE "$dir/licenses/TorrServer-LICENSE"

  sed -e "s|^archive_target=\$|archive_target=$os/$arch|" -e "s|^archive_version=\$|archive_version=$version|" \
    "$root/packaging/install.sh" > "$dir/install.sh"
  chmod 0755 "$dir/install.sh"
  install -m 0644 "$root/LICENSE" "$dir/LICENSE"
  install -m 0644 "$root/NOTICE" "$dir/NOTICE"
  install -m 0644 "$root/packaging/README.txt" "$dir/README.txt"
  cat > "$dir/licenses/TorrServer-SOURCE.txt" <<SOURCE
The torrserver program in this archive is TorrServer $ts_version by YouROK,
unmodified, licensed under the GNU General Public License v3 (see
TorrServer-LICENSE). Its complete source code is available at:

  https://github.com/YouROK/TorrServer/tree/$ts_version

Moviestracker runs it as a separate program and talks to it over HTTP.
SOURCE

  tar -C "$work" -czf "$dist/$name.tar.gz" "$name"
  archives+=("$name.tar.gz")
done

if [ "$(uname -s)" = Darwin ] && [ "${MACAPP:-yes}" != no ]; then
  DIST="$dist" "$root/scripts/macapp.sh" "$version"
  archives+=("Moviestracker-$version.dmg")
fi

(
  cd "$dist"
  if command -v sha256sum >/dev/null; then
    sha256sum "${archives[@]}"
  else
    shasum -a 256 "${archives[@]}"
  fi
) > "$dist/checksums.txt"
