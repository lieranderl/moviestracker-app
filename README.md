# Moviestracker

**English** · [Русский](README.ru.md)

[![CI](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml/badge.svg)](https://github.com/lieranderl/moviestracker-app/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lieranderl/moviestracker-app?sort=semver)](https://github.com/lieranderl/moviestracker-app/releases/latest)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

A self-hosted movie and TV catalog with built-in streaming. It shows what is
trending (trailers, cast, seasons, ratings), finds sources for a title, and
plays them in the browser or on a TV through
[TorrServer](https://github.com/YouROK/TorrServer), which it runs for you. The
self-hosted app needs no cloud account, and its pages never expose your
TorrServer or keys.

- **Discover:** trending titles, search, trailers, cast, seasons, IMDb ratings.
- **Find sources:** filter by quality and HDR, then by tracker and voice.
- **Play anywhere:** direct streams, or HLS transcoded by GStreamer so MKV/AC3/DTS
  play in any browser; playlists and share links for VLC and smart TVs.
- **Multi-user:** local accounts (administrators and viewers) and a live
  dashboard of what is playing.
- **TorrServer apps:** an authenticated gateway lets TorrServe, Lampa and similar
  apps share the same TorrServer.
- **English and Russian** interface, and TMDB metadata in either language.

> Moviestracker does not host, upload, seed or distribute any media. Use it
> only for content you have the right to access. This product uses the TMDB
> API but is not endorsed or certified by TMDB.

## Get it

| Where | Download |
| --- | --- |
| **macOS 13+** (Apple Silicon, Intel), menu bar app | `Moviestracker-<version>.dmg` |
| **Windows 10/11** x64 (preview), tray app | `Moviestracker-Setup-<version>-x64.exe` |
| **Linux, NAS, home server** (`amd64`, `arm64`) | `ghcr.io/lieranderl/moviestracker` |
| **Any browser**, cloud version | [moviestracker.net](https://moviestracker.net) |

Binaries are on the [latest release](https://github.com/lieranderl/moviestracker-app/releases/latest)
with `checksums.txt` and build attestations:

```bash
gh attestation verify Moviestracker-<version>.dmg -R lieranderl/moviestracker-app
```

### Quick start

- **macOS / Windows:** install, open the app (the installers are not signed
  yet, so the OS asks for confirmation once), then create the administrator
  account on the setup page that opens. TorrServer is bundled. On macOS,
  install GStreamer from **Settings → Sources** for in-browser MKV playback;
  the Windows build carries it already.
- **Docker:** save [compose.yaml](compose.yaml) in a folder, then:

  ```bash
  docker compose up -d
  docker compose logs moviestracker   # one-time setup code
  ```

  Open `http://<host>:8095`, enter the code and create the administrator.
  Data lives in the `/data` volume. Update with
  `docker compose pull && docker compose up -d`.

Other devices on the LAN open the address shown in the app menu (for example
`http://192.168.1.20:8095`) and sign in with an account.

- **Web version:** sign in with Google; favourites and settings are kept in
  your account. It runs no TorrServer: your browser talks to your own
  TorrServer directly, which must be at an `https://` address or on the same
  computer (`http://localhost:8090`).

## Technology

| Layer | Stack |
| --- | --- |
| Language | Go 1.27, standard library `net/http` (`ServeMux`) |
| Views | [Templ](https://templ.guide), type-safe components compiled to Go |
| Interactivity | [Datastar](https://data-star.dev) v1: server-driven state over SSE, no client framework |
| Styling | [Tailwind CSS](https://tailwindcss.com) v4, [DaisyUI](https://daisyui.com) v5 |
| Icons | Lucide, rendered server-side |
| Custom JS | Only where Datastar cannot: hls.js player (`frontend/player.js`), browser-side TorrServer calls for the web app (`frontend/torrserver.js`), theme (`frontend/theme.js`) |
| Frontend build | Bun (CSS, JS bundles, icon generation) |
| Streaming | TorrServer (child process) with GStreamer for HLS |
| Data sources | TMDB (metadata), JacRed (sources), IMDb rating service |
| Local storage | One owner-only JSON state file, replaced atomically |
| Cloud app | `cmd/web` on Cloud Run, Google sign-in, Firestore (`internal/store`, `internal/releases`) |
| Desktop shells | macOS menu bar app in Swift (`macos/`), Windows tray app in Go with `fyne.io/systray` (`cmd/tray`) |
| Packaging | Docker image (`linux/amd64`, `linux/arm64`, runs as uid 1000), macOS DMG, Windows installer |
| CI/CD | GitHub Actions: tests, linters, CodeQL, release with attestations |

## How it works

- The server renders HTML with Templ and keeps pages live by streaming
  HTML/signal patches over Server-Sent Events. Shared topics (`internal/live`)
  poll TorrServer once per topic instead of once per viewer.
- Auth is local: bcrypt-hashed accounts, persisted sessions (only token hashes
  stored), login rate limiting, host allowlist against DNS rebinding, and a
  strict CSP.
- TorrServer runs as a managed child process, bound to `127.0.0.1` behind a
  generated password and restarted with backoff. A separate gateway
  (ports 8090/8091) exposes it to other apps with per-app logins.
- Share links are HMAC-signed and expire after 7 days.
- The self-hosted app and the cloud web app render the same `internal/views`
  components and use the same player; only adapters (auth, storage, browser-side
  TorrServer calls) differ.
- Interface text is English in code and translated at render time
  (`internal/i18n`); a test fails if a Russian translation is missing.

```
cmd/server   self-hosted app          internal/handlers, views   HTTP + Templ UI
cmd/tray     Windows tray app         internal/engine, gateway   TorrServer
macos/       macOS menu bar app       internal/update            release check
cmd/web      cloud app (Cloud Run)    internal/tmdb, jacred      data sources
frontend/    player, theme, icons     internal/auth, store       accounts, Firestore
```

## Development

Requires Go 1.27, [Bun](https://bun.sh) and [Air](https://github.com/air-verse/air)
(for `make dev`). Templ, golangci-lint, staticcheck, gosec and govulncheck run
through `go tool`. Docker is needed only for the image.

```bash
make dev        # dev server with Air hot reload
make web-dev    # cloud web app on http://localhost:8080
make test       # go test -race ./...
make lint       # vet, staticcheck, golangci-lint, gofmt
make ci         # full pipeline: format, lint, templ, assets, tests, security, build
make build      # self-contained binary at bin/server
```

`make firestore` starts the Firestore emulator for `make test-store`. All
settings and environment variables are in
[docs/CONFIGURATION.md](docs/CONFIGURATION.md); design notes in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/DATASTAR.md](docs/DATASTAR.md),
[docs/TESTING.md](docs/TESTING.md) and [docs/MAINTAINING.md](docs/MAINTAINING.md).
Changes go through pull requests on a feature branch; `main` is protected and
squash-merged once CI passes. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Privacy and data

TMDB, JacRed and the IMDb rating service see the titles you look up, and
TorrServer talks to BitTorrent peers while playing. The self-hosted app also
asks GitHub once a day for a newer release (it can be switched off in
**Settings → Updates**), and the Mac app downloads GStreamer when you ask it to.
Nothing else is contacted: no analytics, no telemetry. Data is kept in `~/Library/Application Support/moviestracker`
(macOS), `%LOCALAPPDATA%\Moviestracker` (Windows) or the `/data` volume (Docker).

Report security problems privately: see [SECURITY.md](SECURITY.md).

## License

[AGPL-3.0-only](LICENSE). See [NOTICE](NOTICE) for bundled software. TorrServer,
shipped with the apps and the Docker image, is a separate GPL-3.0 program with
its license and source link included.
