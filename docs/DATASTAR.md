# Datastar & Hypermedia Conventions

This document outlines Datastar v1.0 reactive patterns, Server-Sent Events (SSE) streaming, and Templ integration in Moviestracker.

## 1. Core Principles
- **Hypermedia & Signal-Driven Reactivity**:
  - The server remains the primary source of truth for application state and HTML rendering.
  - Client-side interactions (timers, toggles, intervals, active tabs, class manipulation) MUST be expressed declaratively with Datastar signals and attributes (`data-signals`, `data-on:*`, `data-on-interval`, `data-class`, `data-attr:*`).
  - Datastar merges HTML fragments directly into the DOM using SSE, eliminating client-side state hydration and heavy JavaScript bundles.
- **DaisyUI 5 Component Styling**:
  - Use pure DaisyUI 5 classes (`hero`, `fieldset`, `label`, `input`, `dropdown`, `card`, `alert`, `badge`, `btn`, `carousel`).
  - Author **zero custom CSS selectors in templates**. Use DaisyUI semantic color tokens (`bg-base-100`, `text-base-content`, `rounded-box`, `primary`, `neutral`).
  - Literal colors are permitted only inside the centralized DaisyUI theme definition in `frontend/app.css`. Small authored CSS utilities are allowed strictly for capabilities not expressible clearly with DaisyUI/Tailwind (e.g. YouTube iframe transition choreography).

## 2. Server-Sent Events (SSE) Streaming
- **Persistent Streams vs Single-Shot Polling**:
  - Establish persistent real-time streams with `data-init`:
    ```templ\n    <div data-init=\"@get('/api/live-stats?stream=true')\">\n        @LiveStatsFragment(...)\n    </div>\n    ```
  - For single manual refreshes, provide an explicit trigger:
    ```templ\n    <button data-on:click=\"@get('/api/live-stats')\">Refresh</button>\n    ```
- **Backend Streaming Implementation**:
  - Use the official Datastar Go SDK (`github.com/starfederation/datastar-go/datastar`):
    ```go\n    sse := datastar.NewSSE(w, r)\n    // Send element patch rendered with Templ\n    _ = sse.PatchElementTempl(views.LiveStatsFragment(...))\n    ```
  - Always respect context cancellation (`<-r.Context().Done()`) to prevent goroutine leaks.
  - The server bounds concurrent SSE streams to 128 slots, returning `HTTP 429 Too Many Requests` with a `Retry-After` header when capacity is reached.

## 3. Keep Code DRY: Event Delegation Bubbling
- **Avoid Repeated Handlers on Siblings**:
  - Do NOT attach identical `@post` or `@get` attributes to every button or list item.
- **Use Parent Event Bubbling**:
  - Attach a single `data-on:click` handler to the parent container and read the target's dataset:
    ```templ\n    <div\n        class=\"grid grid-cols-3 gap-2\"\n        data-on:click=\"evt.target.closest('button')?.dataset.op && @post('/api/counter?op=' + evt.target.closest('button').dataset.op)\"\n    >\n        <button data-op=\"decrement\">- 1</button>\n        <button data-op=\"reset\">Reset</button>\n        <button data-op=\"increment\">+ 1</button>\n    </div>\n    ```

## 4. Reactive Signals & Form Binding
- **Signal Declaration**:
  - Initialize local state using `data-signals`:
    ```templ\n    <div data-signals=\"{ count: 0, submitting: false, errorMessage: '' }\">\n    ```
- **Two-Way Input Binding**:
  - Bind form inputs to signals with `data-bind`:
    ```templ\n    <input type=\"email\" data-bind:email value=\"alex@example.com\" />\n    ```
- **Dynamic Text, Attributes, and Classes**:
  - Render signal text: `<span data-text=\"$count\"></span>`
  - Toggle classes conditionally: `data-class=\"{ 'block': $isActive, 'hidden': !$isActive }\"`
  - Bind attributes: `data-attr:disabled=\"$submitting\"` or `data-attr:src=\"$show ? 'url' : null\"`

## 5. Dual-Mode Handlers
- Form endpoints (like `/api/login`) check for Datastar request classification via `datastar.IsDatastarRequest(r)`.
- Handlers respond with SSE patches when called by Datastar (`@post`) and support standard HTTP redirects for non-JS progressive enhancement.

## 6. Strict Datastar Priority (Zero-JS Rule)
- **Datastar is the First & Default Choice**: Always use Datastar signals and attributes for:
  - Slide switches, tabs, accordions, and carousels.
  - Active state highlighting (`data-class`).
  - Auto-rotations and countdowns (`data-on-interval__duration.1s`).
  - Dynamic loading/unloading of media attributes (`data-attr:*`).
  - Hover reactions (`data-on:mouseenter`, `data-on:mouseleave`).
- **Custom JS is Strictly a Last Resort**:
  - Imperative JavaScript remains strictly limited to third-party/browser APIs (hls.js playback and clipboard in `frontend/player.js`, YouTube `postMessage` player control). Icons are server-rendered by the `Icon` templ component.
  - Never interpolate user/server data into Datastar expressions with `fmt.Sprintf`; pass it via `data-*` attributes and read `el.dataset` / `evt.target.closest(...).dataset`.
  - Keep interactive inputs (search, forms) outside elements that are re-patched by SSE streams; streams should only patch when rendered HTML changed.
  - NEVER author custom JavaScript files or listeners (`document.querySelector`, `setInterval`, manual DOM toggling) for features that Datastar can handle declaratively.

## 7. DaisyUI Priority & Style Exceptions
- **Use DaisyUI Components**: Always check DaisyUI documentation first (`carousel`, `hero`, `card`, `badge`, `btn`, `alert`, `fieldset`, `modal`, `navbar`).
- **Tailwind Only for Layout**: Use Tailwind utilities solely for layout arrangements (grid, flex, padding, margins, z-index, sizing).
- **Semantic Theme Tokens**: Always use DaisyUI semantic theme tokens (`primary`, `secondary`, `neutral`, `base-100`, `base-200`, `base-300`, `base-content`).
- **Centralized Theme Exemption**: Literal hex colors are restricted to the centralized theme definition in `frontend/app.css`.
- **Authored Utilities Exemption**: Small authored utilities are permitted only for capabilities not expressible cleanly with DaisyUI/Tailwind (e.g. video crossfade choreography).
- **Zero Inline Styles**: Never use `style=\"...\"` attributes; they violate the strict CSP `style-src 'self'`.

## 8. Server Capabilities & Browser Persistence (TorrServer page)
- **Capability signals**: the server pushes `$gst` (TorrServer GStreamer support) with every live patch; GStreamer-only controls use `data-show="$gst"` instead of server-side branching, so capability changes apply without re-rendering.
- **localStorage persistence**: seed signals from storage in `data-signals` (never-throwing expressions, e.g. `split('\n')` rather than `JSON.parse`), persist with a `data-effect`, and let the server be the writer: handlers patch `$torrServers`/`$torrActive` and the effect saves them.
- **Minimal request payloads**: `@post` sends every signal by default; scope it with `{filterSignals: {include: /^newServerUrl$/}}` (or `/^$/` for none).

## 9. Expression Pitfalls
- **Statements inside `&&`**: `$a && (x = 1; @get(...))` is not valid JavaScript, and a syntax error in an attribute fails silently at runtime. Use `if ($a) { x = 1; @get(...) }` whenever the right side has more than one statement.
- **`??` with `&&` or `||`**: `a ?? b && c` is a syntax error; write `a ?? (b && c)`.
- Tests assert on rendered attributes, not on whether the browser can run them: check new expressions in a browser (the console names the failing expression).

## 10. SSE Patterns in Moviestracker
- **Two-phase stream on request** (`/api/torrents`): nothing is searched until the user presses Find sources (series pick a season, sent as `$sourceSeason`). The handler patches a searching skeleton immediately, then replaces `#torrent-results` with results or an explanation. Sort buttons set `$sort` and re-run the same `@get`, which JacRed's cache answers instantly; the server sorts. Quality and HDR are picked before the search (`$qual`, a checkbox group bound to one array, and `$hdr`) and go into the URL (`&quality=2160,1080&hdr=1`); changing them, or the season, searches again with `if ($sourcesRequested) { … }`. Tracker and voice filters work on the rendered rows (`$trackers`, `$voices`, names kept in `data-*` attributes, never in expressions).
- **Server-rendered menu + signals** (`/api/torrserver/queue`): when the player opens on a torrent (a `data-effect` on `$playerOpen && $activeHash`), the server patches the Playlist menu (`#torr-playlist`) and the `$_queue` / `$_queueHash` signals. Datastar has no client-side loop, so lists are rendered on the server; Next and the video's `ended` event read `$_queue`.
- **Infinite scroll** (`/browse/{list}`): the server renders page 1 and a loader under the grid with `data-on-intersect__once="@get('/api/browse/{list}?page=2')"` (once: scrolling away and back before the page arrives asks for it only once). The response appends the next 20 cards to `#browse-grid` (`mode append`) and replaces the loader with the next page's, or an end note after TMDB's last page (at most 500). Titles the page before showed are left out, since TMDB's pages shift.
- **Fan-in stream** (`/api/discover`): the home page renders skeleton rails under one `data-init`; the handler fetches every TMDB list concurrently and patches each rail by id as its list arrives (failed lists collapse to an empty hidden section).
- **Action → region + signals** (`/api/torrents/add`): one response patches a new `#torr-activity` card (whose own `data-init` opens a stats stream) and the `$toast`/`$toastError` signals.
- **Patch-on-change stream** (`/api/torrserver/torrent-stats`): render every tick, send only when the HTML differs, stop on disconnect or timeout.
- **Progressive tabs** (`/tv/{id}` seasons): real `?season=N` links; a delegated `data-on:click` calls `evt.preventDefault()` and `@get`s just the episode list plus the `$season` signal.
- **Live search** (`/search`): a native GET form for shareable URLs; the input's `data-bind:q` creates `$q` from its server-rendered `value` (user text never enters an expression) and `data-on:input__debounce.300ms` patches `#search-results`.
- **Upstream data stays in `data-*`**: magnets, video keys and titles travel as `data-magnet`/`data-video`/`data-title` and are read from `evt.target.closest(...).dataset`; `TestUpstreamTextNeverEntersDatastarExpressions` guards this.
