#!/usr/bin/env bash
# Redraws cmd/tray/moviestracker.ico, the Windows app and tray icon,
# from the Mac app's icon (macos/icon.swift) without its macOS margin. Runs on
# a Mac; the .ico is committed, so Windows builds do not need this.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

swift "$root/macos/icon.swift" "$work" >/dev/null
# The rounded square fills 832 of the 1024 pixels; Windows icons fill the canvas.
sips --cropToHeightWidth 832 832 "$work/AppIcon.iconset/icon_512x512@2x.png" --out "$work/square.png" >/dev/null
sizes=(16 20 24 32 40 48 64 128 256)
for n in "${sizes[@]}"; do
  sips -z "$n" "$n" "$work/square.png" --out "$work/$n.png" >/dev/null
done
python3 - "$work" "$root/cmd/tray/moviestracker.ico" "${sizes[@]}" <<'PY'
import struct, sys
work, out, sizes = sys.argv[1], sys.argv[2], [int(n) for n in sys.argv[3:]]
images = [open(f"{work}/{n}.png", "rb").read() for n in sizes]
ico = struct.pack("<HHH", 0, 1, len(sizes))
offset = 6 + 16 * len(sizes)
for n, png in zip(sizes, images):
    ico += struct.pack("<BBBBHHII", n % 256, n % 256, 0, 0, 1, 32, len(png), offset)
    offset += len(png)
open(out, "wb").write(ico + b"".join(images))
PY
echo "Wrote cmd/tray/moviestracker.ico"
