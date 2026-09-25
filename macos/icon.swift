// Draws Moviestracker.app's icons from Lucide's "clapperboard" (ISC licence,
// see NOTICE) into a folder:
//
//   swift macos/icon.swift <out dir>
//
// out/AppIcon.iconset  the app icon at every size iconutil needs
// out/MenuIcon.png     the menu bar template image, and MenuIcon@2x.png
import AppKit

let clapperboard = """
<path d="m12.296 3.464 3.02 3.956"/>\
<path d="M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4c1.1-.3 2.2.3 2.5 1.3z"/>\
<path d="M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>\
<path d="m6.18 5.276 3.1 3.899"/>
"""

func glyph(_ color: String, width: Double) -> NSImage {
    let svg = """
    <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" \
    stroke="\(color)" stroke-width="\(width)" stroke-linecap="round" stroke-linejoin="round">\(clapperboard)</svg>
    """
    guard let image = NSImage(data: Data(svg.utf8)) else { fatalError("cannot draw the clapperboard SVG") }
    return image
}

func png(_ pixels: Int, to url: URL, draw: (NSRect) -> Void) {
    guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                                     bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                     colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { fatalError() }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    draw(NSRect(x: 0, y: 0, width: pixels, height: pixels))
    NSGraphicsContext.restoreGraphicsState()
    guard let data = rep.representation(using: .png, properties: [:]) else { fatalError() }
    do { try data.write(to: url) } catch { fatalError("\(url.path): \(error)") }
}

// The app icon: the clapperboard on a tile in the theme's blues, sized like
// macOS icons (an 824/1024 tile with a small margin).
func appIcon(_ r: NSRect) {
    let s = r.width
    let tile = r.insetBy(dx: s * 100 / 1024, dy: s * 100 / 1024)
    let path = NSBezierPath(roundedRect: tile, xRadius: s * 185 / 1024, yRadius: s * 185 / 1024)
    NSGradient(starting: NSColor(srgbRed: 0x10 / 255, green: 0x67 / 255, blue: 0xCE / 255, alpha: 1),
               ending: NSColor(srgbRed: 0x3D / 255, green: 0x59 / 255, blue: 0xA1 / 255, alpha: 1))?
        .draw(in: path, angle: -90)
    let g = tile.insetBy(dx: tile.width * 0.2, dy: tile.width * 0.2)
    glyph("white", width: 1.75).draw(in: g)
}

let out = URL(fileURLWithPath: CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ".")
let iconset = out.appendingPathComponent("AppIcon.iconset")
try? FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
    png(size, to: iconset.appendingPathComponent("icon_\(size)x\(size).png"), draw: appIcon)
    png(size * 2, to: iconset.appendingPathComponent("icon_\(size)x\(size)@2x.png"), draw: appIcon)
}
// Menu bar: black on clear; macOS tints template images for light and dark bars.
for (name, px) in [("MenuIcon.png", 18), ("MenuIcon@2x.png", 36)] {
    png(px, to: out.appendingPathComponent(name)) { r in
        glyph("black", width: 2).draw(in: r.insetBy(dx: r.width / 18, dy: r.width / 18))
    }
}
