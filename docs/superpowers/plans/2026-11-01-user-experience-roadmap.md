# Moviestracker user experience roadmap

Date: 1 November 2026. Status: approved roadmap; Phase 1 in progress.

## Goal and delivery order

Make choosing, starting, and continuing a movie or series easier. Prioritize
individual viewing, then household collaboration. This document consolidates
the source review and the signed-in website review performed on the date above.
It does not claim that the proposed features are implemented.

Deliver each item as a focused, independently reviewable change. Complete the
relevant dependencies before starting an item; phases express priority, not a
requirement to bundle every item into one release. Produce a separate
implementation plan for each feature before changing application code.

Priority: **P1** next work; **P2** subsequent improvement; **P3** later capability.
These are delivery priorities, not security severity ratings.

Relative effort: **S** one narrow change; **M** several related slices;
**L** coordinated UI, storage, and integration work. Estimates describe scope,
not promised delivery dates. Items remain pending unless the delivery log
records implementation; implementation does not imply deployment.

Surfaces: **Both** means self-hosted app and cloud web app; **Web** means the
cloud app; **App** means self-hosted deployments. Shared features use the same
components and player, with transport and authentication handled by adapters.

## Evidence and limits

| Evidence | Classification | Observation and owning source |
| --- | --- | --- |
| E1 | Verified deployed UI defect | Opening Add to list on a movie placed part of the popup beyond the left viewport edge. Reproduce against the intended build before fixing. [List picker](../../../internal/views/custom_lists.templ). |
| E2 | Verified deployed UI defect | Live search returned results, but opening a movie and using browser Back restored an empty search and trending rows. [Search view](../../../internal/views/search.templ), [handlers](../../../internal/handlers/search.go). |
| E3 | Verified source defect | Updates returns at most 100 items, while MarkUpdatesSeen acknowledges all unseen records before rendering. Undisplayed or concurrently arriving items can be acknowledged. [Firestore store](../../../internal/store/firestore.go), [alerts handler](../../../internal/web/alerts.go). |
| E4 | Source-derived performance risk | Recommendations try up to six seeds sequentially with per-lookup deadlines and patch rows after the loop. No production latency measurement was taken. [Recommendations](../../../internal/handlers/recommendations.go). |
| E5 | Source-derived scaling risk | Favourites and watch states are read as whole result sets; recommendations and page-initialized alerts reuse these reads. Alert discovery checks only five pages per feed. [Store](../../../internal/store/firestore.go), [alert discovery](../../../internal/web/alerts.go). |
| E6 | Verified documentation mismatch | The privacy description lists fewer data categories than the store supports and directs deletion requests to a public issue tracker. This is not a legal compliance assessment. [Privacy view](../../../internal/views/web_privacy.templ), [store contracts](../../../internal/store/store.go). |
| E7 | Verified deployed UI limitations | Full torrent paths obscure episode identifiers; Watched lacks library search/sorting; some saved titles remain in their original saved language; season cards lack episode tracking. [File rows](../../../internal/views/torrserver.templ), [lists](../../../internal/views/lists.templ), [episodes](../../../internal/views/tv.templ). |
| E8 | Verified deployed UI inconsistency | Original playback was offered without HLS, but its tooltip suggested HLS as a fallback. The reason HLS was absent was not diagnosed. [Playback controls](../../../internal/views/torrserver.templ). |
| E9 | Improvement proposals | Action hierarchy, home-page ordering, source comparison, discovery feedback, following, handoff, and collaboration are product proposals, not verified functional defects. |

The signed-in review confirmed rendering, live search, source results, library
navigation, TorrServer listing, file expansion, and dialog Escape/focus return.
It did not change lists, ratings, settings, or start video playback. Screenshots
and account-specific details are excluded from this repository document.

Earlier review verification passed targeted race tests for handlers, web, store,
live, auth, and views, plus 45 frontend tests. That run did not include a running
Firestore emulator or the full CI gate. It is historical review evidence, not
validation of any future implementation. Actual playback, subtitle/audio
switching, full keyboard/screen-reader behavior, fixed viewport coverage, and
cross-browser compatibility remain unverified in this review.

## Phase 1 — Correctness, reliability, and trust

| ID | Priority / effort | Surface | Dependency | Change and acceptance criteria |
| --- | --- | --- | --- | --- |
| R01 | P1 / S | Both | None | **List picker (E1):** keep the entire popup within the viewport; preserve usable labels and keyboard access. Verify left/right edges, narrow widths, and long translated labels. |
| R02 | P1 / M | Both | None | **Search navigation (E2):** preserve the live query in the URL without adding a history entry for every keystroke. Opening a result and returning restores query, results, and scroll; direct search URLs and the native GET form still work. |
| R03 | P1 / M | Web | None | **Alert acknowledgment (E3):** acknowledge only displayed IDs, paginate older items, and provide a separate Mark all read action. With more than 100 unseen items, undisplayed items remain unread; arrivals after the read snapshot remain unread. |
| R04 | P1 / M | Both | None | **Recommendation latency (E4):** one overall deadline, bounded concurrent lookups, and useful partial rows. Preserve deterministic order and deduplication; slow or failed seeds cannot multiply the total deadline or erase successful rows. |
| R05 | P1 / L | Web; shared callers | None | **Library reads (E5):** bounded seed retrieval, cursor-paginated library reads, and reusable per-user summaries. Show the same user-visible results without repeatedly loading the full library; invalidate derived summaries after changes. |
| R06 | P1 / S | Web | None | **Privacy description (E6):** cover progress, watch states, alerts, lists, public sharing, reviews, and preferences. Replace public deletion-request guidance with a confirmed private contact route; do not invent an address or publish account information. |
| R07 | P2 / L | Web | R06 | **Export/deletion:** provide authenticated export and account-data deletion, including child collections and shared-link mappings. Require deliberate confirmation for deletion; retries complete safely and revoked shared links stop resolving. Specify authentication, retention, and recovery in the feature plan. |

Interface direction: add displayed-ID acknowledgment and cursor-based reads to
the store contracts; retain an explicit bulk acknowledgment capability. Scope
the latter to records present when the action starts. Give recommendation
generation one request budget and a bounded seed interface. Export/deletion
must operate across the complete user-data inventory, not only the user root.

## Phase 2 — Easier browsing and playback

| ID | Priority / effort | Surface | Dependency | Change and acceptance criteria |
| --- | --- | --- | --- | --- |
| R08 | P2 / M | Both | R01 | **Title actions:** make Resume primary when available, otherwise Find sources; collection actions remain secondary. Existing trailer, watch-state, rating, favourite, and list actions stay reachable without competing for primary emphasis. |
| R09 | P2 / S | Both | None | **Returning-user home:** place Continue watching above the large hero when unfinished items exist. New users retain a useful discovery-first home; no empty progress section is added. |
| R10 | P1 / M | Both | None | **Readable files (E7):** show a useful basename or mapped episode code/name, with full release paths in details. Repeated directory prefixes cannot hide the differentiating episode identifier. Existing file selection and playback links remain correct. |
| R11 | P2 / S | Both | None | **Source destination:** label the action Add to the selected server; handle an absent or long server name clearly. Switching servers updates the label and actual destination together. |
| R12 | P1 / M | Both | None | **Playback capabilities (E8):** explain unavailable HLS and offer an applicable alternative. No tooltip suggests a hidden/unavailable action; unknown capability is distinct from confirmed unsupported capability. |
| R13 | P2 / M | Both | R05 for Web | **Library controls:** search, movie/series filter, counts, and sorting by title, saved/watched date, or personal rating. Preserve controls in shareable URLs; define missing-rating order and count semantics in the feature plan. |
| R14 | P2 / M | Both | None | **Localized titles (E7):** render library titles and recommendation seed headings in the current language using canonical IDs. Cache/batch lookups and fall back to saved names without blocking the library on a provider failure. |
| R15 | P2 / M | Both | None | **State/recovery consistency:** loading, retry, empty, disconnected, and missing-file states identify the next useful action. Cancelled or late responses cannot overwrite a newer selection; missing progress targets offer title/source recovery. |
| R16 | P1 / M | Both | R01, R08, R10 | **Accessibility:** verify keyboard navigation, focus visibility/return, touch targets, reduced motion, labels, and status announcements. Record and fix demonstrated failures; do not infer compliance from screenshots alone. |

Interface direction: extend existing view models with display labels,
capability explanations, and library query state. Keep canonical IDs, raw file
identity, and playback URLs separate from presentation. Do not duplicate
controls in web-only templates.

## Phase 3 — Episode progress and personal discovery

| ID | Priority / effort | Surface | Dependency | Change and acceptance criteria |
| --- | --- | --- | --- | --- |
| R17 | P2 / L | Both | R10 | **Episode tracking:** per-episode watched/unwatched state, explicit season actions, and Next unwatched. Use canonical series/season/episode identity; finishing one episode never marks a whole series watched. Existing whole-title marks do not silently backfill episode history. |
| R18 | P2 / L | Both | R17 | **Playback mapping:** connect known files to episodes while retaining file-based resume. Episode completion survives release changes; uncertain mappings require confirmation. Do not promise seek-position transfer between different cuts/releases. |
| R19 | P2 / M | Both | R17 | **Spoiler preference:** optionally hide unwatched episode summaries and images. Reveal-on-demand works accessibly; future/unknown episodes do not leak their summaries while protection is enabled. |
| R20 | P2 / L | Both | R04, R05 | **Recommendation feedback:** Not interested, More like this, and rating-informed ranking. Feedback persists per user, exclusions apply consistently, and an undo/reset path exists. Use transparent rules and existing metadata first. |
| R21 | P2 / M | Both | R20 | **Pick something tonight:** runtime, genre, language, and watched exclusions produce a small shortlist with reroll. Explain no matches and let the user relax constraints; never silently ignore selected constraints. |
| R22 | P2 / M | Both | R13, R21 | **Saved discovery presets:** save, rename, apply, and delete named filter sets. Applying a preset restores supported filters; obsolete fields fail safely without corrupting other preferences. |
| R23 | P2 / M | Both | R15 | **Quick card actions and undo:** add supported watch/list actions without leaving discovery. Undo restores the prior state, remains scoped to the affected item, and handles concurrent changes without silently overwriting them. |

Interface direction: add minimal store capabilities for episode state,
confirmed file mappings, spoiler preferences, discovery presets, and
recommendation feedback. Preserve existing file progress and private reviews.
Detailed schemas, endpoints, limits, and concurrency rules belong in each
feature's implementation plan.

## Phase 4 — Sources, following, and device continuity

| ID | Priority / effort | Surface | Dependency | Change and acceptance criteria |
| --- | --- | --- | --- | --- |
| R24 | P2 / M | Both | R11, R12 | **Source comparison:** select releases side by side; compare quality, HDR, size, bitrate, audio, subtitles, and seeds. Distinguish reported metadata from verified playback support; missing values remain unknown rather than implying compatibility. |
| R25 | P2 / L | Both | R17 | **Series following/calendar:** follow/unfollow a series and show upcoming episodes. Distinguish date-only air dates from timed events; use a timezone only where the provider supplies a time. Handle ended series and unknown dates. |
| R26 | P2 / L | Web first | R03, R05 | **Durable release discovery:** process feed arrivals using persisted checkpoints and idempotent updates. Catch-up covers user absence within the available feed history; duplicates create no duplicate notifications. State upstream history limits explicitly. |
| R27 | P2 / M | Both where supported | R25, R26 | **Notification preferences:** in-app notifications first, per-title preferences, and clear labels for air dates versus source availability. Muting prevents future notices; read counts stay consistent with R03. Specify the App's discovery lifecycle before promising background alerts there. |
| R28 | P3 / L | Both | R12, R18 | **Device handoff:** QR opens the intended title/file using expiring, revocable authorization without embedded TorrServer credentials. A recipient authenticates or has appropriate scoped access; an unreachable server or missing file offers recovery. Existing long-lived share-link behavior is not silently changed. |

Interface direction: add follow/preferences storage and durable discovery
checkpoints. Separate airing events from release events. Handoff needs a scoped
token capability distinct from browser credentials; choose its lifetime and
revocation rules in the implementation specification.

## Phase 5 — Household interaction

| ID | Priority / effort | Surface | Dependency | Change and acceptance criteria |
| --- | --- | --- | --- | --- |
| R29 | P3 / L | Web first | R23 | **Movie-night voting:** extend shared lists with a shortlist, invitations, votes, and live results. Define participant identity and tie behavior in the feature plan; duplicate submissions cannot create duplicate votes. |
| R30 | P3 / M | Web first | R29 | **Sharing permissions:** separate read permission from voting permission; add expiry and revocation. Existing public read-only links never acquire voting access implicitly. Revoked invites cannot submit new votes. |
| R31 | P3 / L | Both | R17, R23; R30 for sharing | **Household queue/preferences:** reuse existing accounts and identity adapters. Shared ordering does not merge individual viewing progress, ratings, or preferences; edits respect the queue's permissions. |
| R32 | P3 / L | Web | R29, R30 | **Cross-instance consistency:** use persistent shared collaboration state, with reconnectable SSE snapshots. Votes survive reconnects and separate service instances converge; cancel subscriptions and bound resource use. Ship with voting, not as a later correctness fix. |

The local live hub is process-local. Use it for local views, not as the cloud's
cross-instance source of truth. Specify collaboration transactions, event
delivery, reconnect behavior, and operating limits before implementing R29.

Synchronized playback is deferred to a separate design: buffering, seek drift,
and TorrServer's shared upstream tasks require their own compatibility work.

## Ranked exploration backlog

These are proposals to validate after the main viewing journey improves, not
implementation commitments. Rank reflects the individual-viewing-first goal.

| Rank / ID | Idea | Effort / surface | Dependency | Validation and candidate acceptance |
| --- | --- | --- | --- | --- |
| 1 / X01 | **Playback readiness check:** explain connection, format, and conversion readiness before starting. | M / Both | R12, R15, R24 | Establish which facts are available without starting playback. Unknown support stays unknown; show the best available next action. |
| 2 / X02 | **End-of-film prompt:** rate, mark watched, or choose something similar. | S / Both | R20, R23 | Validate timing and dismissibility. Never interrupt credits, auto-submit a rating, or infer completion from seeking alone. |
| 3 / X03 | **Guided onboarding:** choose an existing TorrServer or install the local app. | M / Both, adapted | R12, R15 | Test with a new user. Existing users can skip it; setup steps explain capability and network limitations without exposing credentials. |
| 4 / X04 | **Queue maintenance:** replace unavailable releases while preserving title history. | M / Both | R18, R31 | Validate recovery without deletion. Replacement preserves canonical episode history; removal is an explicit user action. |
| 5 / X05 | **TV-friendly navigation:** predictable remote focus and larger controls. | L / Both | R16 | Identify actual supported TV browsers/remotes first. Complete browse-to-play with directional input and clear focus; do not assume desktop keyboard tests prove TV support. |
| 6 / X06 | **Private viewing journal:** personal history and monthly summaries. | M / Both | R17, R18, R07 for Web | Define trustworthy history first. Current recent-file progress is capped and cannot reconstruct lifetime history; label partial history and keep summaries private. |

## Architecture and interface constraints

- Extend shared views and the existing player; keep request attributes, signal
  namespaces, authentication, and browser-only TorrServer access in adapters.
- Use Datastar declarative state/SSE patches, DaisyUI components and semantic
  tokens, native dialogs, server-rendered Lucide icons, and the existing CSP.
  Browser APIs unavailable declaratively remain narrowly scoped exceptions.
- New user-facing strings use English keys and Russian translations. Keep
  provider metadata separate from translated interface text.
- Reuse existing account, list-sharing, rating, resume, and notification
  capabilities; do not present them as missing features or rebuild them.
- Add storage contracts only as needed by a feature. Implement equivalent
  behavior in the applicable memory, local-file, and Firestore backends.
- Additive schema changes preserve old data. Do not infer episode history from
  title-level marks or copy a seek position between unrelated media files.
- Follow [architecture](../../ARCHITECTURE.md),
  [Datastar conventions](../../DATASTAR.md), and
  [testing conventions](../../TESTING.md). Recheck current official docs through
  Context7 when specifying library/API behavior.
- Defaults: existing stack, no paid AI dependency, in-app notifications first,
  no automatic playback/downloading, and no behavioral analytics by default.

## Verification, rollout, and completion gates

Before code, confirm public test seams in the phase implementation plan:
authenticated HTTP/SSE, store contracts, external provider boundaries, and
browser journeys. Follow one failing test, minimal implementation, then the
next behavior. Do not mock internal domain packages or inspect private state.

| Gate | Required scenarios |
| --- | --- |
| Navigation/UI | Search query/result/scroll restoration; popup viewport boundaries; keyboard labels/focus; dialog Escape; narrow layouts; English/Russian and light/dark rendering. |
| Alerts/storage | More than 100 unseen items; concurrent arrival; pagination boundaries; duplicate feed processing; provider/history gaps; account isolation; export/deletion including child records and shared mappings. |
| Performance | Slow/failed recommendation seeds; one overall deadline; bounded concurrency; stable deduplication; bounded library reads; summary invalidation; cancelled requests. |
| Viewing | Episode versus series completion; confirmed/ambiguous mappings; changing releases; resume after reconnect; unavailable file/server; capability-specific fallbacks; spoiler behavior. |
| Collaboration | Repeated votes; permission boundaries; expired/revoked invites; reconnects; separate service instances; independent household progress. |
| Browser coverage | Add Chromium and WebKit checks for search Back, popup boundaries, dialogs, source failures, and resume. Use deterministic external fixtures for CI; real-media compatibility remains a separate recorded check. |

For application changes run applicable frontend tests and Go race tests,
Firestore emulator checks when storage changes, and full `make ci` before
merge. Existing packaging/container checks remain in CI. This roadmap-only
change needs document/link/coverage checks; it does not add application tests.

Deliver focused commits and Conventional Commit PRs under the repository's
branch/CI rules. Deployment, production data changes, and enabling new external
notification channels are outside documentation approval. Roll out schema
additions before dependent behavior and document rollback compatibility in each
implementation plan.

Use privacy-preserving operational measurements for request latency, failures,
SSE reconnects, and bounded read volume. Establish a baseline before claiming
improvement. Do not log private review text, viewing histories, credentials, or
handoff tokens; do not introduce per-user behavioral analytics by default.

An item is complete only when its acceptance criteria pass on its stated
surfaces, relevant CI passes, and documentation matches shipped behavior. Keep
pending, implemented, and deployed status separate in future updates here.

## Next implementation slice

Continue with **R10**, then **R12**. They address directly observed
friction without introducing new persistent product concepts. Continue Phase 1
in focused PRs before the larger episode/discovery and collaboration work.

## Delivery log

- **R01 — implemented, deployment unverified (1 November 2026):** replaced the
  clipped title-page dropdown with the existing native dialog helper. The same
  shared form, endpoints, and list fragments serve both apps. Browser checks
  reproduced the old picker starting 107 px outside a 731 px viewport, then
  verified the dialog bounds and accessible labels in 16 combinations of
  movie/series, app/web, English/Russian, and 320/1280 px widths. Additional
  checks covered the original width, narrow light-theme rendering, landscape,
  keyboard access, Escape/Close/backdrop dismissal and focus return, and list
  creation/membership using disposable in-memory data. The user chose browser
  checks plus existing tests; automated browser infrastructure was not added.
  Existing view/translation/handler race tests and the full `make ci` gate
  passed. Real-device and additional browser-engine checks remain outside this
  slice's recorded verification.
- **R02 — implemented, deployment unverified (1 November 2026):** live search
  now replaces the current history entry with the typed query (`?q=`), keeping
  other parameters, the fragment and browser history state; clearing the box
  removes `q`. Initial focus no longer uses native autofocus, so it cannot
  scroll a restored page back to the input. Returning from a result relies on
  the server-rendered direct search URL. A local browser check (self-hosted
  app, Chromium pane) confirmed typing kept the history length unchanged, and
  opening a result then Back restored the query, six movie results and the
  590 px scroll position. Handler tests cover the rendered URL effect and focus
  behaviour; WebKit, the web app surface and Russian rendering were not checked
  in the browser.

Unresolved questions for this roadmap document: none. Feature-specific API,
schema, lifecycle, and product decisions must be resolved in the corresponding
implementation plans before code; they are not delegated silently to execution.
