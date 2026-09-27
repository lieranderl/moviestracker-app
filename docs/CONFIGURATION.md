# Configuration and reference

Most people never need this page: Moviestracker is set up in the browser
(Settings → Sources) and keeps everything in its data folder. This page is
for running it from source, on a server, or in a container.

## Pages and who can open them

| Route | Access | What it shows |
| --- | --- | --- |
| `/setup` | first run only | Disclaimer and the administrator account; every other page redirects here until it exists |
| `/login` | public | Disclaimer (accepted once per browser, versioned cookie), then sign-in with a local account |
| `/settings/sources` | admin | TMDB key (releases work out of the box with a shared key; add your own any time, with a how-to guide), JacRed instance, TorrServer (managed or existing), IMDb ratings; each is checked before it is saved |
| `/settings/{engine,streaming,storage,network,sharing,gstreamer,security}` | admin | TorrServer's settings grouped by topic, GStreamer settings, and cancelling shared links |
| `/settings/updates` | admin | This version, a newer release if GitHub has one (the DMG or installer to download, or the `docker pull` for the image), and the switch for the daily check |
| `/settings/users` | admin | Accounts: add viewers or administrators, reset passwords (signs that person out), change roles, delete; the last administrator stays |
| `/dashboard` | signed in | Live overview: what plays now (and on which device), torrent totals with sparklines, engine, storage, system and source health |
| `/movies` | signed in | Home: trending billboard, weekly trending rails, and lazily streamed now playing / popular / top rated rails |
| `/movie/{id}`, `/tv/{id}` | signed in | Title pages: hero, credits, seasons & episodes (TV), JacRed sources (quality and HDR picked before searching; tracker and voice filters), trailers, recommendations |
| `/person/{id}` | signed in | Biography, known-for rail, movie/TV filmography |
| `/search?q=` | signed in | Live multi-search across movies, TV and people; trending discovery when blank |
| `/torrserver` | signed in | TorrServer torrent manager and player (Direct or HLS per file, Play all and a playlist for torrents of several videos); Add Torrents takes magnet, http(s) `.torrent` and `torrs://` links, info-hashes and `.torrent` files |
| `/s/<token>/…` | anyone with the link | Signed 7-day stream links for VLC, TVs and `.m3u` playlists; one file each |

`/` sends signed-in users to `/movies` and everyone else to `/login`.

**New releases.** Once a day Moviestracker asks GitHub for its latest stable release (`api.github.com`, with an ETag, so an unchanged answer is not sent again). When one is newer, administrators see it in the navbar and Settings → Updates, and the menu bar and tray apps offer **Download Moviestracker vX.Y.Z…**. Nothing is downloaded or installed by itself: the Mac DMG and the Windows installer install over the running version and keep accounts and settings; the image is updated with `docker compose pull && docker compose up -d`. TorrServer and GStreamer come with the release they were tested with. The request carries nothing about the install, its accounts or what plays; like any web request, GitHub sees the internet address it comes from. Turn the check off in Settings → Updates. Development builds never check.

## Environment variables

Everything is set in the browser under Settings → Sources and kept in the data directory. Environment variables (also read from `.env`):

| Variable | Default | Purpose |
| --- | --- | --- |
| `MT_LISTEN` | `:8095` | Listen address; the default serves every network interface so TVs and phones can connect |
| `MT_DATA_DIR` | `moviestracker` in the user config directory | Where accounts, sessions and sources are kept (the Mac app sets `~/Library/Application Support/moviestracker`, the Windows tray app `%LOCALAPPDATA%\Moviestracker`) |
| `MT_TORRSERVER_LISTEN` | `:8090` | Where TorrServer apps (TorrServe, Lampa) connect once **Settings → Other apps** is on; nothing listens there until then |
| `MT_LAN_ADDRESS` | *(empty: found)* | The address TVs and phones use to reach this machine, put in links made while Moviestracker is opened as `localhost`; `off` keeps `localhost` (the Docker image's default) |
| `MT_HOSTNAMES` | *(empty)* | Extra hostnames to answer to (comma-separated) besides IP addresses, `localhost` and `<hostname>.local` |
| `MT_SECURE_COOKIES` | `false` | Secure cookies and HSTS, for HTTPS setups |
| `MT_TORRSERVER_BIN` | *(empty)* | TorrServer program for managed mode; otherwise `torrserver` (`torrserver.exe` on Windows) next to Moviestracker or `<data dir>/engine/bin/torrserver` |
| `MT_GSTREAMER_SCRIPT` | *(empty)* | macOS: the GStreamer install script for development runs; the app uses the one in its bundle |
| `TMDB_API_KEY`, `JACRED_URL`, `JACRED_APIKEY`, `IMDB_SERVICE_URL`, `TORRSERVER_URL` | *(empty)* | Override the matching Sources setting and make it read-only |
| `TORRSERVER_USER`, `TORRSERVER_PASSWORD` | *(empty)* | Login for an existing TorrServer started with `--httpauth` (also settable in Sources) |
| `TRUSTED_PROXY_CIDRS` | *(empty)* | Comma-separated trusted reverse proxy CIDRs (e.g. `127.0.0.1/32`) |

## External services

**TMDB key.** Releases carry a shared TMDB key, so search works right after setup; it is never shown in the pages. Every install shares it, so: should TMDB disable it (for example because someone misuses it), search stops until an update ships a new one; TMDB sees the requests as Moviestracker's; and busy moments elsewhere can slow it. A free key of your own (Settings → Sources) avoids all of that, and **Use the shared key instead** goes back. The key is built in by `scripts/macapp.sh` and `scripts/winapp.sh` from `MT_SHARED_TMDB_KEY` or the git-ignored `.tmdb-shared-key` file, and by the `Dockerfile` from the `tmdb_key` build secret, never from git; like any key inside a program, it can be read out of the binary, so use a key made for this purpose only. Development builds use `TMDB_API_KEY` from `.env`.

Streaming is local, but discovery needs outbound internet access to TMDB (`api.themoviedb.org`, `image.tmdb.org`) and a JacRed instance (the public `https://jacred.su` by default, or your own [jacred-fdb/jacred](https://github.com/jacred-fdb/jacred)).

**JacRed key.** From 9 October 2026 (00:00 Moscow time) jacred.su answers searches only with a key. Releases carry Moviestracker's jacred.su project key, with unlimited searches, so search works without setup; it is used for jacred.su only (never sent to another JacRed), never shown, and built in like the TMDB key (`MT_SHARED_JACRED_KEY` or the git-ignored `.jacred-shared-key`, the `jacred_key` Docker build secret). A key of your own replaces it, and **Use Moviestracker's key instead** goes back. A free personal key, from «Мой ключ» at [jacred.su/account](https://jacred.su/account), allows 100 searches a day; add it in Settings → Sources (or `JACRED_APIKEY`). Each title whose sources are opened uses one search, and repeats within an hour come from the cache. Without a key, or with a wrong one, the title page says a key is needed; with the day's searches used up, it says when they renew. Moviestracker talks to jacred.su through `api.jacred.su`, sending the key as a Bearer header; a private jacred-fdb instance gets it as `apikey`. IMDb ratings come from a small public rating service and are optional.

## Docker

The image (`ghcr.io/lieranderl/moviestracker`, linux/amd64 and linux/arm64) is how Moviestracker runs on Linux, a NAS or a home server; [compose.yaml](../compose.yaml) runs it. It holds `moviestracker`, TorrServer's GStreamer build (pinned in `scripts/torrserver.lock`) and Debian 13's GStreamer 1.26 with the plugin sets TorrServer lists.

- **Processes:** `tini` (PID 1) runs `moviestracker`, which runs TorrServer on `127.0.0.1` behind generated credentials and restarts it if it stops, as the native apps do. Everything runs as uid 1000. Only port 8095 is published; publish 8090 as well for TorrServer apps (Settings → Other apps).
- **Storage:** `/data` is the only volume: `moviestracker.json` (accounts, sessions, sources) and `engine/` (TorrServer's database, settings and log). The root filesystem can be read-only; GStreamer's plugin registry goes to `/tmp`, a tmpfs. A bind-mounted folder must be writable by uid 1000 (or run the container as its owner with `user:`); otherwise Moviestracker stops with a message saying so.
- **Health and shutdown:** `moviestracker --health` asks `/healthz` (the image's `HEALTHCHECK`; passing probes are not logged). `SIGTERM` closes open pages, then stops TorrServer; allow 30 seconds (`stop_grace_period`).
- **Setup:** a browser reaching the container through Docker's network is never "this machine", so the first account needs the setup code from `docker compose logs moviestracker`.
- **Links for TVs:** the image sets `MT_LAN_ADDRESS=off`, because the container's own address is not one other devices can reach; set it to the host's address if you open Moviestracker as `localhost`.
- **External TorrServer:** `TORRSERVER_URL` (with `TORRSERVER_USER` and `TORRSERVER_PASSWORD` if it uses `--httpauth`) or Settings → Sources; the bundled one then does not run.
- **Licences:** `/usr/share/doc/moviestracker/` holds Moviestracker's AGPL-3.0 licence and NOTICE, TorrServer's GPL-3.0 licence and source link, and `GSTREAMER.txt`; every Debian package's licence is in `/usr/share/doc/<package>/copyright`.

`make docker-build` builds it locally (with Docker, or Apple's `container` CLI; `make container-run` runs it that way), and `make docker-smoke` runs `scripts/ci/docker-e2e.sh`, the end-to-end test CI runs.

## Building releases

`make dmg VERSION=v0.1.0` builds the Mac app and its DMG (`scripts/macapp.sh`): universal `moviestracker-server` and `torrserver` joined with `lipo`, the Swift menu bar app from `macos/Moviestracker/main.swift` (Command Line Tools are enough), icons drawn from Lucide's clapperboard by `macos/icon.swift`, all signed ad hoc. Each TorrServer build is downloaded and checked against the SHA-256 in `scripts/torrserver.lock`; its GPL-3.0 license and source link travel with it.

`make winapp VERSION=v0.1.0` builds the Windows installer (`scripts/winapp.sh`, in Git Bash on Windows): `moviestracker-server.exe`, the tray app `Moviestracker.exe` from `cmd/tray` (Go, [fyne.io/systray](https://github.com/fyne-io/systray); its portable part is `internal/tray`), and TorrServer's `TorrServer-gst-windows-amd64.exe`, which carries GStreamer inside. [Inno Setup 7](https://jrsoftware.org/isinfo.php) packs them with `packaging/windows/moviestracker.iss` into `Moviestracker-Setup-<version>-x64.exe`, a per-user install without administrator rights. The programs build anywhere (`GOOS=windows`); only the installer needs Windows. `scripts/windows-icon.sh` redraws the tray and program icon, `cmd/tray/moviestracker.ico`, on a Mac.

Official releases are built by GitHub Actions when a version tag is pushed; see [MAINTAINING.md](MAINTAINING.md).
