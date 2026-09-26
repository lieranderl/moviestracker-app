#!/usr/bin/env bash
# Builds the Windows installer: dist/Moviestracker-Setup-<version>-x64.exe,
# with the tray app, the server and the pinned TorrServer that carries its
# own GStreamer.
#
#   scripts/winapp.sh v0.1.0
#
# The programs build anywhere; the installer needs Inno Setup 7 (ISCC.exe),
# so it is made on Windows (Git Bash). Elsewhere the script stops after
# filling the stage folder, unless ISCC points to a way to run it.
#
# Environment:
#   DIST             output folder (default: dist)
#   ISCC             Inno Setup's compiler (default: found on PATH or in Program Files)
#   TORRSERVER_FROM  folder with TorrServer-gst-windows-amd64.exe and LICENSE
#                    already fetched (skips the download)
#   MT_SHARED_TMDB_KEY  the shared TMDB key built in, used when a user has no key
#                    of their own (default: the git-ignored .tmdb-shared-key file)
#   MT_SHARED_JACRED_KEY  Moviestracker's jacred.su project key, used for jacred.su
#                    without a key of one's own (default: .jacred-shared-key)
#
# The programs are not signed: Windows SmartScreen asks to confirm the first
# run of a downloaded installer ("More info" → "Run anyway").
set -euo pipefail

version="${1:?usage: winapp.sh <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
dist="${DIST:-$root/dist}"
ts_version="$(awk '$1 == "version" {print $2}' "$root/scripts/torrserver.lock")"
# shellcheck source=scripts/shared-keys.sh
. "$root/scripts/shared-keys.sh"
# Windows wants a numeric version: v1.2.3-rc.1 → 1.2.3.
numeric="${version#v}"
numeric="${numeric%%[-+]*}"
[[ "$numeric" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a version: $version" >&2; exit 1; }

mkdir -p "$dist"
work="$(mktemp -d)"
syso="$root/cmd/tray/rsrc_windows_amd64.syso"
trap 'rm -rf "$work" "$syso"' EXIT
stage="$work/stage"
mkdir -p "$stage/licenses"

echo "Building the Windows programs $version"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -C "$root" -trimpath \
  -ldflags "-s -w -X main.version=$version -X main.sharedTMDBKey=$shared_tmdb_key -X main.sharedJacRedKey=$shared_jacred_key" \
  -o "$stage/moviestracker-server.exe" ./cmd/server
# The tray app's icon, version and manifest (a GUI program, sharp on high-DPI
# screens) go in as a Windows resource the Go linker picks up.
(cd "$root/cmd/tray" && go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --out rsrc --manifest gui \
  --icon moviestracker.ico --product-name Moviestracker --file-description Moviestracker \
  --product-version "$numeric.0" --file-version "$numeric.0" --original-filename Moviestracker.exe \
  --copyright "Moviestracker contributors, AGPL-3.0")
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -C "$root" -trimpath \
  -ldflags "-s -w -H windowsgui -X main.version=$version" -o "$stage/Moviestracker.exe" ./cmd/tray

if [ -n "${TORRSERVER_FROM:-}" ]; then
  install -m 0755 "$TORRSERVER_FROM/TorrServer-gst-windows-amd64.exe" "$stage/torrserver.exe"
  install -m 0644 "$TORRSERVER_FROM/LICENSE" "$stage/licenses/TorrServer-LICENSE.txt"
else
  TS_TARGET=windows/amd64 "$root/scripts/fetch-torrserver.sh" "$stage/torrserver.exe"
  "$root/scripts/fetch-torrserver.sh" --license "$stage/licenses/TorrServer-LICENSE.txt"
fi
install -m 0644 "$root/LICENSE" "$stage/LICENSE.txt"
install -m 0644 "$root/NOTICE" "$stage/NOTICE.txt"
cat > "$stage/licenses/TorrServer-SOURCE.txt" <<SOURCE
torrserver.exe is TorrServer $ts_version by YouROK, unmodified, licensed
under the GNU General Public License v3 (see TorrServer-LICENSE.txt). It
includes GStreamer (LGPL), which it unpacks on first use. Its complete
source code is available at:

  https://github.com/YouROK/TorrServer/tree/$ts_version

Moviestracker runs it as a separate program and talks to it over HTTP.
SOURCE

iscc="${ISCC:-}"
if [ -z "$iscc" ]; then
  for candidate in ISCC "/c/Program Files/Inno Setup 7/ISCC.exe" "/c/Program Files (x86)/Inno Setup 7/ISCC.exe"; do
    if command -v "$candidate" >/dev/null 2>&1; then
      iscc="$candidate"
      break
    fi
  done
fi
if [ -z "$iscc" ]; then
  echo "Inno Setup 7 (ISCC.exe) not found: the programs are built, the installer is not." >&2
  echo "Build it on Windows, or set ISCC." >&2
  exit 1
fi

winpath() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }
setup="$dist/Moviestracker-Setup-$version-x64.exe"
rm -f "$setup"
# MSYS_NO_PATHCONV: Git Bash would take /DName=value for a path and rewrite it.
MSYS_NO_PATHCONV=1 "$iscc" /Q \
  "/DAppVersion=$version" "/DNumericVersion=$numeric" \
  "/DStage=$(winpath "$stage")" "/DOutputDir=$(winpath "$dist")" \
  "$(winpath "$root/packaging/windows/moviestracker.iss")"
echo "$setup"
