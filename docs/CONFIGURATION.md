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
| `/settings/users` | admin | Accounts: add viewers or administrators, reset passwords (signs that person out), change roles, delete; the last administrator stays |
| `/dashboard` | signed in | Live overview: what plays now (and on which device), torrent totals with sparklines, engine, storage, system and source health |
| `/movies` | signed in | Home: trending billboard, weekly trending rails, and lazily streamed now playing / popular / top rated rails |
| `/movie/{id}`, `/tv/{id}` | signed in | Title pages: hero, credits, seasons & episodes (TV), JacRed sources, trailers, recommendations |
| `/person/{id}` | signed in | Biography, known-for rail, movie/TV filmography |
| `/search?q=` | signed in | Live multi-search across movies, TV and people; trending discovery when blank |
| `/torrserver` | signed in | TorrServer torrent manager and HLS player; Add Torrents takes magnet, http(s) `.torrent` and `torrs://` links, info-hashes and `.torrent` files |
| `/s/<token>/…` | anyone with the link | Signed 7-day stream links for VLC, TVs and `.m3u` playlists; one file each |

`/` sends signed-in users to `/movies` and everyone else to `/login`.

## Environment variables

Everything is set in the browser under Settings → Sources and kept in the data directory. Environment variables (also read from `.env`):

| Variable | Default | Purpose |
| --- | --- | --- |
| `MT_LISTEN` | `:8095` | Listen address; the default serves every network interface so TVs and phones can connect |
| `MT_DATA_DIR` | `moviestracker` in the user config directory | Where accounts, sessions and sources are kept |
| `MT_HOSTNAMES` | *(empty)* | Extra hostnames to answer to (comma-separated) besides IP addresses, `localhost` and `<hostname>.local` |
| `MT_SECURE_COOKIES` | `false` | Secure cookies and HSTS, for HTTPS setups |
| `MT_TORRSERVER_BIN` | *(empty)* | TorrServer program for managed mode; otherwise `torrserver` next to Moviestracker or `<data dir>/engine/bin/torrserver` |
| `MT_GSTREAMER_SCRIPT` | *(empty)* | macOS: the GStreamer install script for development runs; the app uses the one in its bundle |
| `TMDB_API_KEY`, `JACRED_URL`, `JACRED_APIKEY`, `IMDB_SERVICE_URL`, `TORRSERVER_URL` | *(empty)* | Override the matching Sources setting and make it read-only |
| `TORRSERVER_USER`, `TORRSERVER_PASSWORD` | *(empty)* | Login for an existing TorrServer started with `--httpauth` (also settable in Sources) |
| `TRUSTED_PROXY_CIDRS` | *(empty)* | Comma-separated trusted reverse proxy CIDRs (e.g. `127.0.0.1/32`) |

## External services

**TMDB key.** Releases carry a shared TMDB key, so search works right after setup; it is never shown in the pages. Every install shares it, so: should TMDB disable it (for example because someone misuses it), search stops until an update ships a new one; TMDB sees the requests as Moviestracker's; and busy moments elsewhere can slow it. A free key of your own (Settings → Sources) avoids all of that, and **Use the shared key instead** goes back. The key is built in by `scripts/release.sh` and `scripts/macapp.sh` from `MT_SHARED_TMDB_KEY` or the git-ignored `.tmdb-shared-key` file, never from git; like any key inside a program, it can be read out of the binary, so use a key made for this purpose only. Development builds use `TMDB_API_KEY` from `.env`.

Streaming is local, but discovery needs outbound internet access to TMDB (`api.themoviedb.org`, `image.tmdb.org`) and a JacRed instance (the public `https://jacred.su` by default, or your own [jacred-fdb/jacred](https://github.com/jacred-fdb/jacred)). IMDb ratings come from a small public rating service and are optional.

## Container

The image is non-root and read-only; mount a volume at `/data` for its state:

```bash
make docker-build
docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  -p 8095:8095 -v moviestracker:/data moviestracker:local
```

## Building releases

```bash
make release VERSION=v0.1.0
```

writes `dist/` with the Linux archives (amd64, arm64), `Moviestracker-<version>.dmg` when run on a Mac, and `checksums.txt`. `make dmg VERSION=v0.1.0` builds only the Mac app (`scripts/macapp.sh`): universal `moviestracker-server` and `torrserver` joined with `lipo`, the Swift menu bar app from `macos/Moviestracker/main.swift` (Command Line Tools are enough), icons drawn from Lucide's clapperboard by `macos/icon.swift`, all signed ad hoc. Each TorrServer build is downloaded and checked against the SHA-256 in `scripts/torrserver.lock`; its GPL-3.0 license and source link travel with it.

Official releases are built by GitHub Actions when a version tag is pushed; see [MAINTAINING.md](MAINTAINING.md).
