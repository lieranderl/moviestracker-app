# Moviestracker

[![CI](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml/badge.svg)](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lieranderl/moviestracker-app?sort=semver)](https://github.com/lieranderl/moviestracker-app/releases/latest)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

**Your own movie and TV catalog for every screen in your home: on a Mac, a
Windows PC, or a NAS or home server.**

Moviestracker shows what is trending, with trailers, cast, seasons and ratings,
finds sources for a title, and plays them in your browser or on your TV
through [TorrServer](https://github.com/YouROK/TorrServer), which it runs for
you. It is free, runs entirely on your own machine, and needs no account
anywhere.

- **Discover:** trending movies and series, trailers, cast and crew, seasons
  and episodes, recommendations, IMDb ratings, and live search.
- **Play anywhere:** in any browser, including Safari and iPhone, or send a link
  or playlist to VLC, IINA, Infuse or a smart TV on your network.
- **Share the house:** accounts for the family (administrators and viewers)
  and a live dashboard of what is playing and on which device.
- **Works with TorrServer apps:** TorrServe on an Android TV, Lampa and other
  TorrServer apps can use the same TorrServer and torrent list, each with a
  login of its own ([more](#torrserver-apps-torrserve-lampa)).
- **Private by design:** everything stays on your machine. Pages never show
  your TorrServer or keys, and links for TVs expire after a week.

> Moviestracker does not host, upload, seed or distribute any media. Use it
> only for content you have the right to access. This product uses the TMDB
> API but is not endorsed or certified by TMDB.

## Get it

| Where | Download | Guide |
| --- | --- | --- |
| **macOS 13+** (Apple Silicon, Intel) | `Moviestracker-<version>.dmg` | [Install on your Mac](#install-on-your-mac) |
| **Windows 10/11** (64-bit, preview) | `Moviestracker-Setup-<version>-x64.exe` | [Windows](#windows-preview) |
| **Linux, NAS, home servers** (amd64, arm64) | `ghcr.io/lieranderl/moviestracker` | [Docker](#linux-nas-and-home-servers-docker) |

Downloads are on the [latest release](https://github.com/lieranderl/moviestracker-app/releases/latest),
with `checksums.txt`. Every file and image is built by this repository's
release workflow, which anyone can check with the
[GitHub CLI](https://cli.github.com):

```bash
gh attestation verify Moviestracker-<version>.dmg -R lieranderl/moviestracker-app
```

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

A few files use audio or video browsers cannot decode at all, such as the
rare AAC Main audio: the player then says what it cannot play, and the
file's **Direct** link plays in VLC, IINA, Infuse or on a TV.

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

## Windows (preview)

You need 64-bit Windows 10 or 11. Download
`Moviestracker-Setup-<version>-x64.exe` from the
[latest release](https://github.com/lieranderl/moviestracker-app/releases/latest)
and run it. It installs for your Windows account only and needs no
administrator rights.

1. The installer is not signed yet, so Windows SmartScreen may say it
   "protected your PC". Click **More info**, then **Run anyway**.
2. Keep **Start Moviestracker when I sign in** ticked, and finish. A
   clapperboard icon appears in the taskbar's notification area, and the
   setup page opens in your browser.
3. The first time Moviestracker and TorrServer start, Windows Firewall asks
   whether to allow them. Allow **private networks**, so your TV and phone
   can reach them.

MKV files play in the browser straight away: TorrServer's Windows build
carries GStreamer inside, so there is nothing else to install. The first MKV
takes a few extra seconds while TorrServer unpacks it.

The tray icon's menu (click the clapperboard) has the same items as the
Mac's menu bar icon: open Moviestracker, the dashboard, the address for TVs
and phones (click to copy), start when you sign in, logs, restart,
uninstall and quit.

- **Update:** run the newer installer. Accounts and settings stay.
- **Uninstall:** choose **Uninstall Moviestracker…** in the tray menu, or
  remove it in **Settings → Apps → Installed apps**. It asks whether to
  delete your accounts and settings too.
- **Where things are:** the programs in
  `%LOCALAPPDATA%\Programs\Moviestracker`, accounts, settings, TorrServer's
  data and the log (`moviestracker.log`) in `%LOCALAPPDATA%\Moviestracker`.

## Linux, NAS and home servers (Docker)

On Linux, a NAS (Synology, QNAP, Unraid, TrueNAS…) or a home server,
Moviestracker runs in Docker. The image is for `linux/amd64` and
`linux/arm64` (Raspberry Pi 4/5, most NAS boxes) and has everything: it runs
its own TorrServer with GStreamer, so MKV files play in the browser.

1. Save [compose.yaml](compose.yaml) in a folder, then start it:

   ```bash
   docker compose up -d
   ```

2. Find the one-time setup code in its log:

   ```bash
   docker compose logs moviestracker
   ```

3. Open `http://<the server's address>:8095`, enter the code and create the
   administrator account.

Without Compose:

```bash
docker run -d --name moviestracker --restart unless-stopped -p 8095:8095 \
  -v moviestracker-data:/data --read-only --tmpfs /tmp --cap-drop ALL \
  --security-opt no-new-privileges --stop-timeout 30 \
  ghcr.io/lieranderl/moviestracker:latest
```

- **Your data:** accounts, settings and TorrServer's torrent list live in the
  `/data` volume and survive updates. To use a folder of your own instead
  (`./moviestracker:/data`), give it to uid 1000 (`sudo chown 1000:1000
  moviestracker`), or set `user:` in `compose.yaml` to the folder's owner.
- **Only port 8095** is published. TorrServer stays inside the container,
  behind a password Moviestracker generates; to let
  [TorrServer apps](#torrserver-apps-torrserve-lampa) in, publish 8090 too.
- **Names and links:** Moviestracker answers to IP addresses and `localhost`.
  To use a name such as `http://nas.local:8095`, add it to `MT_HOSTNAMES` in
  `compose.yaml`. If you open it as `localhost`, set `MT_LAN_ADDRESS` to the
  server's address, so links for TVs and phones point there.
- **Your own TorrServer** (advanced): set `TORRSERVER_URL`, or choose it in
  **Settings → Sources**. The bundled one then stays off.
- **Update:** `docker compose pull && docker compose up -d`.
- **Uninstall:** `docker compose down` keeps the data volume; add `-v` to
  delete it too.
- **Logs:** `docker compose logs moviestracker`. `docker compose ps` shows
  whether it is healthy.

HDR-to-SDR conversion needs TorrServer's `hdrtonemap` plugin, which only its
Windows build has. The other conversions work the same as in the apps. All
settings are in [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## TorrServer apps: TorrServe, Lampa

TorrServer apps, such as TorrServe on an Android TV or Lampa, can use the
TorrServer Moviestracker runs, on the Mac, on Windows and in Docker. They see
the same torrents: what you save in Moviestracker shows up in the app, and the
other way round.

1. In **Settings → Other apps**, switch on **Open TorrServer to other apps**.
2. Under **Logins for apps**, make a login for the TV or app (for example
   *Living room TV*). Its password is shown once: enter it in the app then.
3. In the app, add the address the page shows, such as
   `http://192.168.1.20:8090`, with that username and password.

- Apps can play, add and remove torrents. TorrServer's settings stay
  Moviestracker's, and apps cannot stop it.
- Video players the app hands a stream to need no login, as with TorrServer
  itself, but only for torrents already saved.
- Only devices on your home network (and your
  [Tailscale](https://tailscale.com) network) get in. **Also allow from the
  internet** opens it further. Logins then travel unencrypted, so for access
  away from home a VPN such as Tailscale or WireGuard is the safer way.
- Removing a login in **Settings → Other apps** signs that app out at once.
- Port 8090 is the one TorrServer apps suggest. If a TorrServer of its own
  already uses it, stop that one, or choose another port with
  `MT_TORRSERVER_LISTEN`. In Docker, publish the port too (`"8090:8090"`, in
  `compose.yaml`).

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
footer link does; see [NOTICE](NOTICE) for the software Moviestracker
bundles. TorrServer, shipped in the Mac app, the Windows installer and the
Docker image, is a separate program under the GPL-3.0, with its license and
source link included.
