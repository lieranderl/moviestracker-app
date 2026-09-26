#!/usr/bin/env bash
# Builds Moviestracker.app, one app for Apple Silicon and Intel Macs, and a
# DMG to install it from: dist/Moviestracker-<version>.dmg
#
#   scripts/macapp.sh v0.1.0
#
# Environment:
#   DIST             output folder (default: dist)
#   TORRSERVER_FROM  folder with TorrServer-gst-darwin-{arm64,amd64} and LICENSE
#                    already fetched (skips the download; used by tests)
#   MT_SHARED_TMDB_KEY  the shared TMDB key built in, used when a user has no key
#                    of their own (default: the git-ignored .tmdb-shared-key file)
#   MT_SHARED_JACRED_KEY  Moviestracker's jacred.su project key, used for jacred.su
#                    without a key of one's own (default: .jacred-shared-key)
#
# The app is signed ad hoc, not with a Developer ID: macOS asks to confirm the
# first launch in System Settings → Privacy & Security.
set -euo pipefail

version="${1:?usage: macapp.sh <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
dist="${DIST:-$root/dist}"
ts_version="$(awk '$1 == "version" {print $2}' "$root/scripts/torrserver.lock")"
# shellcheck source=scripts/shared-keys.sh
. "$root/scripts/shared-keys.sh"

mkdir -p "$dist"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
app="$work/dmg/Moviestracker.app"
contents="$app/Contents"
mkdir -p "$contents/MacOS" "$contents/Resources/licenses"

torrserver_file() { # <asset name> <dest> [target]
  if [ -n "${TORRSERVER_FROM:-}" ]; then
    install -m "$([ "$1" = LICENSE ] && echo 0644 || echo 0755)" "$TORRSERVER_FROM/$1" "$2"
  elif [ "$1" = LICENSE ]; then
    "$root/scripts/fetch-torrserver.sh" --license "$2"
  else
    TS_TARGET="$3" "$root/scripts/fetch-torrserver.sh" "$2"
  fi
}

echo "Building Moviestracker.app $version"
for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -C "$root" -trimpath \
    -ldflags "-s -w -X main.version=$version -X main.sharedTMDBKey=$shared_tmdb_key -X main.sharedJacRedKey=$shared_jacred_key" -o "$work/moviestracker-$arch" ./cmd/server
  torrserver_file "TorrServer-gst-darwin-$arch" "$work/torrserver-$arch" "darwin/$arch"
done
lipo -create -output "$contents/MacOS/moviestracker-server" "$work/moviestracker-arm64" "$work/moviestracker-amd64"
lipo -create -output "$contents/MacOS/torrserver" "$work/torrserver-arm64" "$work/torrserver-amd64"

# The menu bar app itself.
for target in arm64-apple-macos13 x86_64-apple-macos13; do
  # No back-deployment libraries: macOS 13 has the runtime the app needs, and
  # the Command Line Tools ship one of them without an Intel slice.
  swiftc -swift-version 5 -O -runtime-compatibility-version none -target "$target" \
    -o "$work/Moviestracker-$target" "$root/macos/Moviestracker/main.swift"
done
lipo -create -output "$contents/MacOS/Moviestracker" "$work"/Moviestracker-*-apple-macos13

swift "$root/macos/icon.swift" "$work/icons"
iconutil -c icns -o "$contents/Resources/AppIcon.icns" "$work/icons/AppIcon.iconset"
cp "$work/icons/MenuIcon.png" "$work/icons/MenuIcon@2x.png" "$contents/Resources/"
install -m 0755 "$root/macos/install-gstreamer.sh" "$contents/Resources/"

short="${version#v}"
cat > "$contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>Moviestracker</string>
	<key>CFBundleDisplayName</key>
	<string>Moviestracker</string>
	<key>CFBundleIdentifier</key>
	<string>app.moviestracker</string>
	<key>CFBundleExecutable</key>
	<string>Moviestracker</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$short</string>
	<key>CFBundleVersion</key>
	<string>$short</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>LSMinimumSystemVersion</key>
	<string>13.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.entertainment</string>
	<key>NSHumanReadableCopyright</key>
	<string>AGPL-3.0. Includes TorrServer $ts_version (GPL-3.0).</string>
</dict>
</plist>
PLIST

install -m 0644 "$root/LICENSE" "$root/NOTICE" "$contents/Resources/"
torrserver_file LICENSE "$contents/Resources/licenses/TorrServer-LICENSE"
cat > "$contents/Resources/licenses/TorrServer-SOURCE.txt" <<SOURCE
The torrserver program in this app is TorrServer $ts_version by YouROK,
unmodified, licensed under the GNU General Public License v3 (see
TorrServer-LICENSE). Its complete source code is available at:

  https://github.com/YouROK/TorrServer/tree/$ts_version

Moviestracker runs it as a separate program and talks to it over HTTP.
SOURCE

# Ad hoc signatures: required on Apple Silicon, and they keep the bundle sealed.
codesign --force --sign - "$contents/MacOS/moviestracker-server" "$contents/MacOS/torrserver"
codesign --force --sign - "$app"

ln -s /Applications "$work/dmg/Applications"
# The app is not signed with a Developer ID, so macOS refuses the first open
# of a downloaded copy; the DMG says what to do.
cat > "$work/dmg/If macOS won't open it.txt" <<'HELP'
Moviestracker is not signed with an Apple Developer ID yet, so the first
time you open it macOS says it "can't be opened" or that Apple "could not
verify" it. Nothing else needs installing: the app has all it needs.

1. Drag Moviestracker to Applications and open it once (the refusal is
   expected). Click Done, not Move to Trash.
2. Open System Settings → Privacy & Security, scroll down to Security,
   and click "Open Anyway" next to the message about Moviestracker.
3. Confirm with your password or Touch ID, then choose Open.

After that it opens normally. A clapperboard icon appears in the menu bar
(there is no Dock icon) and the setup page opens in your browser.

Right-click → Open no longer skips this check on macOS 15 and newer.
HELP
dmg="$dist/Moviestracker-$version.dmg"
rm -f "$dmg"
# hdiutil now and then fails with "Resource busy" (a disk image service still
# busy, often on CI runners): try again a few times, and show why it failed.
for attempt in 1 2 3 4 5; do
  if hdiutil create -volname "Moviestracker" -srcfolder "$work/dmg" -format UDZO "$dmg" >"$work/hdiutil.log" 2>&1; then
    break
  fi
  cat "$work/hdiutil.log" >&2
  rm -f "$dmg"
  if [ "$attempt" = 5 ]; then
    echo "error: could not create $dmg" >&2
    exit 1
  fi
  echo "hdiutil create failed (attempt $attempt of 5), trying again in $((attempt * 5))s" >&2
  sleep $((attempt * 5))
done
echo "$dmg"
