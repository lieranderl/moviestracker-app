# Architecture & Backend Lifecycle

This document describes the architectural tiers, server lifecycles, admission controls, and memory invariants for the Moviestracker codebase. Moviestracker runs locally (a computer or home server) and is used from the LAN.

## 1. High-Level Tiers
The application follows a clean 3-tier server-rendered hypermedia architecture:

- **Presentation Layer (`cmd/server`, `internal/handlers`)**:
  - `net/http` standard library router (`http.ServeMux`) with method-based routing patterns.
  - Datastar Server-Sent Events (SSE) handlers streaming HTML fragment patches.
  - Security, recovery, request-logging and host-allowlist (DNS rebinding) HTTP middleware.
  - Setup gate: until the first account exists, every request except `/setup`, `/api/setup`, static assets and health probes redirects to `/setup`.
  - Dedicated unauthenticated health endpoints (`/healthz` and `/readyz`).
- **Application & Domain State (`internal/config`, `internal/auth`, `internal/sources`, `internal/tmdb`, `internal/jacred`, `internal/torrserver`)**:
  - Data directory (`MT_DATA_DIR`): one owner-only JSON state file with accounts, sessions, sources and the TorrServer address, replaced atomically on every change.
  - Local accounts with bcrypt password hashes; the first one (created at setup) is the administrator. Unknown usernames cost the same bcrypt comparison as wrong passwords.
  - Session manager with bounded capacity and TTL eviction, persisted so sessions survive restarts; only SHA-256 hashes of tokens are stored, and users are resolved from the accounts on every request.
  - Disclaimer consent gate: `/api/login` refuses without the versioned `mt_consent` cookie; setup records consent itself.
  - Sources (TMDB key, JacRed URL/key, IMDb service, TorrServer address) are set by the administrator; each is checked live before it is saved, and the clients are swapped atomically so changes apply without a restart. Environment variables override them and make them read-only.
  - Upstream TMDB media catalog client with request coalescing, TTL caching, and bounded concurrency.
  - TMDB detail models (movie, TV, season, person, multi-search), one `append_to_response` request each, cached in a bounded (512 entries) TTL cache whose concurrent misses share one fetch; unknown ids surface as `tmdb.ErrNotFound` (404 page), other failures as a 502 page.
  - JacRed torrent search (on request only): movies are filtered by release year; series are searched one season at a time (`season=N`), keeping releases dated to the show's first year or that season's year. Releases are stripped of non-magnet or non-HTTP(S) links, deduped by info hash, sorted by seeders and cached for an hour (failures are not cached). `jacred.su` is searched through its search API (`api.jacred.su/api/search`, key as `Authorization: Bearer`, 120 results a page); other instances through the jacred-fdb API (`/api/v1.0/torrents`, key as `apikey`). Refusals become `jacred.ErrKeyNeeded` (401), `jacred.ErrBlocked` (403) and `*jacred.LimitError` (429, with `Retry-After`), which the title page and the Sources check explain; the check also reports the key's searches left today (`X-RateLimit-Remaining`). Quality (`?quality=2160,1080`, any of 2160/1080/720/480) and HDR (`?hdr=1`) are picked before searching: one quality and HDR are filtered by the search API (`quality=`, `videotype=hdr`), which then returns all such releases rather than the best-seeded 120; several qualities are one unfiltered search filtered here, since the API takes one. Tracker and voice filters work on the results in the browser.
  - TorrServer client with one server-side address; releases are added with the TMDB title and poster. The stream proxy forwards only media paths to it. Listing, stats and playlists read TorrServer's `list` only, because its per-torrent endpoints keep torrents awake.
  - Gateway for other apps (`internal/gateway`): off until an admin turns it on (Settings → Other apps); then a second port (`MT_TORRSERVER_LISTEN`, `:8090`) passes TorrServer's API through to the TorrServer in use, with Moviestracker's login. Each app has its own login (random password, kept as a bcrypt hash; a header that passed is remembered in memory until the login changes, since TVs poll every second). Stopping TorrServer, writing its settings and `wipe` are refused; players without a login may stream only torrents already in TorrServer's list, as TorrServer itself allows streams without a login. Private, loopback, link-local and tailnet (100.64.0.0/10) addresses only, unless the internet is allowed; ten failed logins lock an address out for a minute. A request the gateway passed on that reaches it again is refused (508), so an external TorrServer address pointing at the gateway cannot loop.
  - Managed engine (`internal/engine`): TorrServer runs as a child process with `--ip 127.0.0.1 --httpauth` and a generated password in its `accs.db`; it is restarted with doubling backoff after a crash and stopped with Moviestracker. An engine left by a crashed run (it accepts our password and refuses a wrong one) is shut down on start; any other program on the port is left alone. The mode (managed/external) is saved on the first start.
  - Share links (`internal/streamlink`): `/s/<hash>.<file>.<expiry>.<hmac>/…` lets external players stream one file (and its GStreamer HLS) for 7 days without a session; the HMAC secret lives in the data directory.
- **View Layer (`internal/views`)**:
  - Type-safe, compile-time HTML components generated by [Templ](https://templ.guide).
  - Semantic DaisyUI 5 utility classes and components.
  - Pinned, embedded static assets (Datastar v1.0.4, Tailwind CSS, Lucide icons).

## 2. Server Lifecycle & Graceful Shutdown
- **Entrypoint (`cmd/server/main.go`)**:
  - Configures explicit server timeouts:
    - `ReadHeaderTimeout`: 5s (mitigates Slowloris attacks)
    - `IdleTimeout`: 120s (keeps keep-alive connections bounded)
    - `MaxHeaderBytes`: 1 MB
  - `WriteTimeout` is left disabled on the server struct to allow long-lived SSE streaming. External unary dependencies enforce explicit context deadlines (e.g. an 8-second deadline for TMDB catalog retrieval).
- **Graceful Shutdown**:
  - Traps `SIGINT`, `SIGTERM`, `SIGHUP`, and `SIGQUIT`.
  - Initiates `server.Shutdown(ctx)` with a 10-second grace period.
  - Active SSE streams observe context cancellation and release permits, and background session managers close cleanly before process exit.

## 3. Admission Controls & Resource Bounding
- **Concurrent SSE Stream Limiter**:
  - SSE connections consume open sockets and goroutines. The server enforces a strict concurrency ceiling (`MaxSSEStreams`, defaulting to 128 in production).
  - Incoming stream requests acquire a slot; if full, the server immediately returns `HTTP 429 Too Many Requests` with a `Retry-After` header.
  - Every stream requires a signed-in user.
- **Login Rate Limiter & Trusted Proxies**:
  - Protects against brute-force attacks using a token-bucket rate limiter per client IP.
  - Client IP state is tracked in a bounded map with periodic eviction to prevent memory growth under IP churn.
  - When `TRUSTED_PROXY_CIDRS` is configured, client identity is resolved by traversing `X-Forwarded-For` right-to-left, selecting the first untrusted IP. If no trusted proxy is configured or the header is malformed, the peer `RemoteAddr` is used.
- **Live Topics (`internal/live`)**:
  - Every live view (the TorrServer list, a title's download card, the player's stats and media info) subscribes to shared topics instead of polling: `engine` (every 5s), `torrents` (2s) and `player:<hash>` (1s, which also sends the GStreamer heartbeat).
  - One goroutine polls a topic while anyone watches it and stops 30s after the last viewer leaves; subscribers are told only when the snapshot changes (one-slot mailbox, newest wins) and a new or reconnecting stream starts from the current snapshot, so reconnects never reach TorrServer.
  - The player opens one stats stream with `data-effect`; changing file or audio track reopens it and closing the player sends `stop=1`.
- **TorrServer page player (`/torrserver`)**:
  - Each file plays as Direct (the original, streamed as is) or HLS (TorrServer's GStreamer conversion, with an audio-track pick); cards have no Play of their own, only their files do.
  - Opening the player on a torrent asks `/api/torrserver/queue` for its videos: the server patches the Playlist menu and the `$_queue` signal, which the video's `ended` event and Next follow; Play all starts the first video.
  - Keyboard (Space/K, arrows, M, F) and wheel volume are Datastar handlers on the player; the wheel follows Macs' inverted "natural" scrolling (`webkitDirectionInvertedFromDevice`, else assumed on a Mac). In fullscreen a top bar shows the title and its place in the playlist.
  - VLC and IINA open through their URL schemes (`frontend/player.js`); browsers do not say whether that worked, so a page that keeps the focus for 1.5s reports that the player did not open.
- **Dashboard (`/dashboard`)**:
  - One SSE stream keeps six cards current from shared topics: `engine`, `torrents` (whose poller also keeps 2-minute speed histories for the sparklines), `plays` (sessions from the stream proxy), `system` (gopsutil every 5s; TorrServer storage settings and folder sizes at most every minute) and `sources` (outcome of real TMDB/JacRed/IMDb calls, recorded by wrappers around their clients).
  - Playback sessions (`internal/streams`) are keyed by device and torrent; direct streams report the byte range read, HLS the segment requested; a session ends 30s after its last request unless a response is still being sent.
- **Settings (`internal/handlers/settings_spec.go`)**:
  - Each TorrServer setting is described once (key, label, range, unit, inverted switches, format check) and the page, validation and conversion come from that description. Everything posted is checked before anything is saved.
  - TorrServer replaces its whole settings object on save, so the client reads it as raw JSON fields and writes it back with only the changed fields replaced; settings Moviestracker does not show (Rutor, Torznab, TMDB, MCP, SSL…) and fields of newer TorrServer versions are kept.
  - Engine settings make TorrServer reconnect; the page warns while streams play through the proxy. Command-line-only options (proxy, public IPs, max size, watch folder) are saved in the data directory and restart the managed engine; they are read-only for an external TorrServer. GStreamer settings apply at once.
  - A managed engine's first start turns TorrServer's Bonjour announcement off (once per engine directory).
- **Bounded Request Readers**:
  - All JSON/form request bodies are wrapped with `http.MaxBytesReader(w, r.Body, 1<<20)` (1 MB limit) before decoding.

## 4. TMDB Media Catalog & Resilience
- **Coalesced TTL Caching**:
  - Media catalog queries are cached in-memory with a 10-minute TTL.
  - Concurrent requests coalesce via `golang.org/x/sync/singleflight` so that simultaneous cold misses result in exactly one upstream fetch cycle.
- **Cold Request Ceiling**:
  - A cold catalog fetch executes at most 9 external HTTP requests: 1 for trending movies, 1 for trending TV series, and up to 7 hero item enrichments (details + videos) with at most 3 in flight.
- **Stale Fallback**:
  - If a refresh fails while expired cached catalog data is available, the client logs a warning and returns the stale catalog data to maintain availability.
- **Latency Bounding**:
  - The `/movies` route bounds catalog retrieval with an 8-second context timeout, rendering a degraded dashboard gracefully if the upstream provider exceeds the deadline.

## 5. Title Pages & Upstream Boundaries
- Title handlers (`/movie/{id}`, `/tv/{id}`, `/person/{id}`, `/search`) share one prologue: signed-in user, positive integer id, configured provider, `CatalogTimeout` deadline.
- Torrent search and TorrServer hand-off resolve the title server-side from `?type=movie|tv&id=N`, so clients can only search for real TMDB titles; magnets are accepted only with a 40-hex BTIH.
- The per-torrent stats stream (`/api/torrserver/torrent-stats?stream=true`) takes an SSE slot, follows the shared `torrents` topic and patches only when the rendered HTML changes.

## 6. Health Probes
- **Liveness (`GET /healthz`)**:
  - Returns `200 OK`, `Content-Type: text/plain; charset=utf-8`, and body `ok\n`.
  - Verifies basic HTTP listener responsiveness.
- **Readiness (`GET /readyz`)**:
  - Returns `200 OK`, `Content-Type: text/plain; charset=utf-8`, and body `ready\n`.
  - Verifies that routing and internal stores are initialized and ready to accept traffic.

## 7. Frontend Styling & Intentional Exceptions
- **DaisyUI Semantic Tokens**:
  - Semantic tokens (`bg-base-100`, `text-base-content`, `primary`, `neutral`) remain mandatory in all templates.
- **Centralized Themes**:
  - Literal hex colors are allowed only inside a centralized DaisyUI theme definition in `frontend/app.css`.
- **Authored Utilities Exception**:
  - Small authored CSS utilities are allowed strictly for capabilities not expressible clearly with DaisyUI/Tailwind, such as third-party iframe transition choreography for hero video playback.
- **Imperative JavaScript Exception**:
  - Imperative JavaScript remains strictly limited to third-party/browser APIs (hls.js playback, clipboard, native `<dialog>` and popover calls, YouTube `postMessage` player control).

## 8. Running Locally & in a Container
- **Local binary**: `make build` then run `bin/server`; it listens on `:8095` (`MT_LISTEN`) and keeps its state in `MT_DATA_DIR`. Plain HTTP on the LAN is the default; set `MT_SECURE_COOKIES=true` behind HTTPS.
- **Docker (Linux, NAS, home servers)**: the `Dockerfile` builds a multi-arch image (linux/amd64, linux/arm64) with `moviestracker`, TorrServer's GStreamer build (pinned in `scripts/torrserver.lock`) and Debian's GStreamer; `tini` is PID 1, everything runs as uid 1000, the root filesystem can be read-only (`/tmp` a tmpfs), and `/data` is the one volume: Moviestracker's state and TorrServer's (`/data/engine`). Moviestracker manages the bundled TorrServer exactly as the native apps do (`internal/engine`, loopback only), so only port 8095 is published; `TORRSERVER_URL` switches to an external one. `moviestracker --health` is the health check. `compose.yaml` runs it; `scripts/ci/docker-e2e.sh` tests it.
- **Native apps**: on a Mac, `scripts/macapp.sh` builds `Moviestracker.app` in a DMG: a Swift menu bar app (`macos/Moviestracker`) that runs the bundled server with `MT_PARENT_PID`, so the server stops when the app is gone, and restarts it with backoff if it exits. The server restarts TorrServer and replaces an orphaned one left by a crash. On macOS the server can download GStreamer's official runtime for its managed TorrServer (`internal/gstinstall` runs the bundle's `macos/install-gstreamer.sh`: pinned version and SHA-256, only the parts TorrServer uses) into the data folder; admins start it from Settings → Sources, the TorrServer page or the dashboard, which follow its progress on the `gstreamer` live topic. When it is done, and at every start, the server sets it as TorrServer's GStreamer path (unless someone chose another) and restarts TorrServer once.
- **New releases** (`internal/update`): the server asks GitHub's `releases/latest` once a day (ETag; failed checks retried hourly; never from a `dev` build or while an admin has it off) and keeps the newer release with this platform's file (the DMG on macOS, the installer on Windows, none on Linux, which is the Docker image). A middleware puts it in the request context, so every page's navbar announces it to administrators; Settings → Updates shows the download. The menu bar and tray apps read `GET /api/update` (from this machine only, no session; 204 when nothing is newer) every 10 minutes and open the download on click; only `https://github.com/` links are offered. Nothing is installed automatically.
- **Single process**: sessions, SSE slots and caches are process-local; run one instance per data directory.
- **Outbound access**: TMDB and the configured JacRed instance must be reachable from the machine for discovery; streaming stays on the LAN.

## 9. Packages

The project is layered:

- `cmd/server`: process configuration, HTTP lifecycle, and graceful shutdown (10s grace period); it also stops when the app that started it goes away (`MT_PARENT_PID` from the Mac app, `MT_STOP_WITH_STDIN` from the Windows tray app)
- `cmd/tray` (Windows only): the tray app `Moviestracker.exe`; `internal/tray` is its portable part (runs and restarts the server, the menu) and is tested on every OS
- `internal/config`: the data directory's state file (accounts, sessions, sources, TorrServer address), written atomically with owner-only permissions; environment overrides and the keys built into releases. `Store.State()` returns a snapshot sharing no slice or map with the stored state, so pages can read it while `Update` runs
- `internal/auth`: local accounts (bcrypt) and sessions that survive restarts (only token hashes are stored)
- `internal/sources`: builds the TMDB/JacRed/IMDb clients from the saved sources and checks new ones before saving
- `internal/handlers`: routing, setup gate, request validation, middleware (host allowlist, CSRF), SSE admission control (ceiling 128 streams with 429 Retry-After), and health probes (`/healthz`, `/readyz`)
- `internal/tmdb`: resilient media catalog provider with coalesced caching (10m TTL), cold ceiling of 9 requests (7 hero slides), stale fallback, and an 8s deadline; movie, TV, season, person and search details fetched in one `append_to_response` call each through a bounded, coalescing TTL cache
- `internal/jacred`: JacRed torrent search (jacred.su search API or jacred-fdb API, year filter, unsafe-link rejection, info-hash dedupe, 1h cache)
- `internal/engine`: runs TorrServer as a supervised child process (loopback only, generated credentials, restart with backoff); it ends with Moviestracker (Linux: parent-death signal, Windows: a job object)
- `internal/update`: asks GitHub whether a newer stable release is out, and picks this platform's download
- `internal/streamlink`: signs the stream links external players open
- `internal/live`: shared pollers behind every live view (one TorrServer call per topic, whatever the number of open pages)
- `internal/streams`: playback sessions seen by the stream proxy, for the dashboard
- `internal/stats`: torrent totals, sparkline histories and machine stats (gopsutil)
- `internal/torrserver`: TorrServer client and HLS stream proxy
- `internal/views`: Templ pages and patchable fragments; carousels share one `carousel` component with back/forward buttons for mouse screens (`pointer-fine`)
- `frontend` and `static`: pinned asset sources and generated embedded assets
