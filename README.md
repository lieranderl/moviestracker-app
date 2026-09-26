# Moviestracker

[![CI](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml/badge.svg)](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lieranderl/moviestracker-app?sort=semver)](https://github.com/lieranderl/moviestracker-app/releases/latest)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

**Your own movie and TV catalog, on your Mac, for every screen in your home.**

Moviestracker shows what is trending, with trailers, cast, seasons and ratings,
finds sources for a title, and plays them in your browser or on your TV
through [TorrServer](https://github.com/YouROK/TorrServer), which it runs for
you. It is free, runs entirely on your computer, and needs no account
anywhere.

- **Discover:** trending movies and series, trailers, cast and crew, seasons
  and episodes, recommendations, IMDb ratings, and live search.
- **Play anywhere:** in any browser, including Safari and iPhone, or send a link
  or playlist to VLC, IINA, Infuse or a smart TV on your network.
- **Share the house:** accounts for the family (administrators and viewers)
  and a live dashboard of what is playing and on which device.
- **Private by design:** everything stays on your Mac. Pages never show your
  TorrServer or keys, and links for TVs expire after a week.

> Moviestracker does not host, upload, seed or distribute any media. Use it
> only for content you have the right to access. This product uses the TMDB
> API but is not endorsed or certified by TMDB.

## Install on your Mac

You need macOS 13 Ventura or newer, on Apple Silicon or Intel.

1. Download **`Moviestracker-<version>.dmg`** from the
   [latest release](https://github.com/lieranderl/moviestracker-app/releases/latest).
2. Open it and drag **Moviestracker** to **Applications**.
3. Open Moviestracker from Applications. The first time, macOS refuses to
   open it, because the app is not signed with an Apple Developer ID yet:
   - click **Done** (not *Move to Trash*);
   - open **System Settings → Privacy & Security**, scroll to *Security*, and
     click **Open Anyway** next to Moviestracker;
   - confirm with your password or Touch ID, then choose **Open**.

   On macOS 15 and newer, right-click → Open no longer skips this step.
   The DMG includes these steps in *If macOS won't open it.txt*.
4. A clapperboard icon appears in the menu bar (there is no Dock icon), and
   your browser opens the setup page. Create your administrator account, and
   you are done.

Nothing else needs installing: TorrServer is inside the app, and search works
right away with Moviestracker's shared TMDB key.

### Play MKV files in the browser

Most movie files are MKV, often with AC3 or DTS sound, which browsers cannot
play. Moviestracker can fix that with [GStreamer](https://gstreamer.freedesktop.org):
on **Settings → Sources** (or the menu's **Set Up Browser Playback**), click
**Install GStreamer**. It downloads the official GStreamer runtime (about
146 MB, checked against a pinned checksum) into Moviestracker's own folder.
No Homebrew, Terminal or password is needed, and it is removed with the app.

Without GStreamer, MKV files still play in VLC, IINA, Infuse and on TVs
through stream links.

### Watch on a TV, phone or tablet

The menu shows your Mac's address for other devices, for example
`http://192.168.1.20:8095`. Click it to copy it, then open it in the browser on
your phone or tablet, or on your TV. On the TorrServer page every file has:

- **Play**, to watch in the browser;
- **Link**, a stream link for VLC, IINA, Infuse or a TV's player;
- **.m3u8**, a playlist of the whole torrent for those players.

Choose **Direct** for the original file (best quality, for VLC and TVs) or
**HLS** for a stream converted as it plays (for Safari, iPhone and Apple TV).
Links work for 7 days; **Settings → Security** cancels all of them at once.

Other devices on your network can open Moviestracker only with an account.

### The menu bar icon

| Menu item | What it does |
| --- | --- |
| Open Moviestracker, Dashboard | Opens it in your browser |
| On a TV or phone: http://… | Copies the address for other devices |
| Set Up Browser Playback | Opens the GStreamer setup |
| Start at Login | Starts Moviestracker when you log in (on by default) |
| Show Logs | Opens the log, handy when something goes wrong |
| Restart | Restarts Moviestracker and TorrServer |
| Uninstall Moviestracker… | Removes it; see below |
| Quit Moviestracker | Stops Moviestracker and TorrServer |

### Update

Download the new DMG and drag Moviestracker to Applications again, replacing
the old one. Your accounts, settings and torrent list are kept. When a new
version pins a newer GStreamer, the dashboard offers an **Update** button.

### Uninstall

Choose **Uninstall Moviestracker…** in the menu. It removes the app,
TorrServer with its torrent list, and the GStreamer Moviestracker downloaded.
You choose whether to also delete Moviestracker's accounts and settings.

Everything Moviestracker keeps is in
`~/Library/Application Support/moviestracker`.

### Troubleshooting

- **The browser says it cannot connect.** Check that the menu bar icon says
  Moviestracker is running; if not, choose **Restart**, then **Show Logs**.
- **Another program uses port 8095.** Quit it, then restart Moviestracker.
- **A TV or phone cannot connect.** It must be on the same network as the
  Mac. Allow incoming connections for Moviestracker if the macOS firewall
  asks.
- **Search shows nothing.** The shared TMDB key may be busy: add your own free
  key in **Settings → Sources**.
- Still stuck? [Open an issue](https://github.com/lieranderl/moviestracker-app/issues/new/choose)
  with the version (in the page footer) and the relevant part of the log.

## Linux (preview)

Linux builds for amd64 and arm64 (Raspberry Pi 4/5) install as a systemd
service with the same features, minus the menu bar icon:

```bash
tar xzf moviestracker_<version>_linux_<arch>.tar.gz
cd moviestracker_<version>_linux_<arch>
sudo ./install.sh
```

The installer starts the service at boot, offers to install GStreamer with
your package manager, and prints the address to open. A browser on another
device needs the one-time setup code it shows to create the first account.
Running a newer archive's `install.sh` upgrades in place.
`sudo /usr/local/lib/moviestracker/uninstall.sh` removes it (add `--purge` to
delete accounts and settings too). Logs: `journalctl -u moviestracker`.

Details, the container image and all settings are in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Where the data comes from

- **TMDB** for titles, images, cast and trailers: Moviestracker's shared key
  works out of the box, or add your own free key.
- **JacRed** for finding sources: the public [jacred.su](https://jacred.su),
  or your own [JacRed](https://github.com/jacred-fdb/jacred).
- **IMDb ratings** from a small rating service; they can be switched off.
- **TorrServer** does the streaming, inside the app or one you already run
  (on a NAS, for example).

These services see the titles you look up, as any website would, and
TorrServer talks to BitTorrent peers while it plays. Apart from that,
Moviestracker contacts nothing: no analytics, no telemetry, no accounts.

## Contributing

Bug reports, ideas and pull requests are welcome: see
[CONTRIBUTING.md](CONTRIBUTING.md). It is built with Go, [Templ](https://templ.guide),
[Datastar](https://data-star.dev) and [DaisyUI](https://daisyui.com): the server
renders HTML and keeps pages live over server-sent events, with no
JavaScript framework. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) explains
how it fits together.

Security problems: please report them privately, see [SECURITY.md](SECURITY.md).

## License

[GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0-only). You may
use, change and share Moviestracker freely. If you run a changed version that
other people use over a network, you must offer them its source code, as the
footer link does; see [NOTICE](NOTICE) for the software Moviestracker bundles. TorrServer, shipped inside the app and archives, is a
separate program under the GPL-3.0, with its license and source link
included.
