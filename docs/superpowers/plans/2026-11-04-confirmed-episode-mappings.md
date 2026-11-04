# Confirmed Episode Mappings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Native execution is recommended because the store, completion and shared form use the same small set of interfaces.

**Goal:** Persist explicitly confirmed episode associations for an exact torrent/file on both apps, while preserving file resume and canonical watched history.

**Architecture:** Account-scoped keyed mapping stores back one shared file-menu section. Shared authenticated handlers validate canonical episodes; local/browser adapters supply file context. Finished progress checks a confirmed record before exact filename inference.

**Tech Stack:** Existing Go, Templ, Datastar, DaisyUI, Memory/JSON/Firestore stores; no new runtime dependency.

**Spec:** [Approved design](../specs/2026-11-04-confirmed-episode-mapping-design.md).

## Global Constraints

- One episode or a consecutive range within one season; season zero supports specials.
- Identity: authenticated account, normalized 40-character hash and positive TorrServer file ID; another release requires its own confirmation.
- Save/edit/clear never changes seek position or existing watched history; no backfill on Save.
- Keep records outside the recent-progress window; include export/deletion and local username-reuse cleanup.
- Shared UI/player; declarative Datastar, native dialog/controls, semantic tokens, English/Russian text; no inline styles.
- Production account/history and real TorrServer remain unchanged. VoiceOver remains unchanged; speech unverified. Device handoff stays skipped.
- Public stores, signed-in HTTP/SSE, shared rendered UI and disposable browser/media boundaries were explicitly approved. One failing behavior, minimal implementation, repeat; external fakes only.

## Review Focus

1. A file or season switch while a reply is delayed must not populate or save another file's form (Task4/5).
2. Confirmation overridden by an unavailable mapping store must not fall back to a wrong filename episode (Task3).
3. Account deletion racing a write, or recreation of a local username, must not restore the old mapping (Task2/4).
4. Episode numbering gaps, specials and very large submitted ranges must be validated before allocating/marking a range (Task1/4).
5. A failed atomic JSON write must leave live reads and reopened state unchanged; returned records cannot alias mutable storage (Task1).

## Files and responsibilities

- Create `internal/store/episode_mapping.go`, `episode_mapping_file.go`, `episode_mapping_firestore.go`, `episode_mapping_memory.go`: contract/validation and backend-specific persistence.
- Create `internal/store/episode_mapping_test.go`, `episode_mapping_file_test.go`, `episode_mapping_firestore_test.go`: public store capabilities, failure/reopen/emulator/account lifecycle.
- Modify `internal/store/store.go`, `account.go`, `memory.go`, `firestore.go`: capability, account export/deletion and account-session fencing.
- Create `internal/handlers/episode_mappings.go`, `episode_mappings_test.go`: signed-in mapping routes, canonical validation and context handling.
- Modify `internal/handlers/episodes.go`, `progress.go`, `handlers.go`, `users.go`: completion lookup, local wiring and deletion guard.
- Modify `internal/web/server.go` and relevant public fixture implementations: web route/store wiring.
- Create `internal/views/episode_mapping.go`, `episode_mapping.templ`; extend `torrserver.templ` and existing web adapters only for shared file context/requests. Generated files and assets are committed.
- Extend `internal/handlers/episode_progress_test.go`, `users_test.go`, `internal/web/progress_test.go`; create `internal/web/episode_mappings_test.go`; update translation/render coverage.

### Task 1: Exact file mapping persists through public stores

**Interfaces:** Create `store.EpisodeMapping{Hash string; Index int; File string; TMDBID int; Season int; First int; Last int}` and `EpisodeMappingStore` with `EpisodeMapping(ctx context.Context, uid, hash string, index int) (EpisodeMapping, bool, error)`, `SaveEpisodeMapping(ctx context.Context, uid string, m EpisodeMapping) error`, `ClearEpisodeMapping(ctx context.Context, uid, hash string, index int) error`. `OpenEpisodeMappingFile(path string) (*EpisodeMappingFile, error)` supplies the local backend; `DeleteAccountMappings(ctx context.Context, uid string) error` is its cleanup capability.

- [x] Write one public Memory contract test `TestConfirmedEpisodeMappingBelongsToOneAccountAndFile`: save hash`aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111`, index1, file`episode.mp4`, series95396, season2, First3/Last4. Read returns that literal range; another account/index/hash finds none. Run `go test ./internal/store -run ConfirmedEpisodeMapping -count=1`; establish the missing-capability red.
- [x] Implement the minimal Memory contract with normalized identity and account-session checks. Rerun the test to green.
- [x] Add one validation behavior at a time: empty owner, invalid hash/index/show, negative season, zero/reversed episodes and oversized file path fail; uppercase valid hash reads the same record. Basic storage validation does not make TMDB network calls. Rerun each red/green cycle.
- [x] Apply the contract to a real owner-only atomic JSON file. Add `TestConfirmedMappingSurvivesReopenAndFailedWrite`: save3–4, make the owned disposable directory unwritable, attempt5–6, require error and still3–4 both live and after reopening. Restore permission. Implement clone/write/commit after success; do not mutate live state first.
- [x] Add distinct-file concurrent writes and idempotent clear; reopen retains each accepted record. Returned values are independent copies. Run `go test -race ./internal/store -run 'Confirmed|EpisodeMapping' -count=1`.
- [x] Commit focused store/file changes: `feat(store): persist confirmed episode file mappings`.

### Task 2: Firestore, export and account lifecycle

**Interfaces:** Embed `EpisodeMappingStore` in `store.Store`. Memory/Firestore implement Task1 methods. Account adds `EpisodeMappings []EpisodeMapping` with JSON key`episodeMappings`. Firestore uses `users/{uid}/episodeMappings/{normalizedHash-index}` and the existing deletion/session transaction guard.

- [x] Run Task1 contracts against the real Firestore emulator; introduce one failing store behavior before its implementation. Require one keyed document lookup, no collection scan on playback.
- [x] Add `TestAccountExportAndDeletionIncludeConfirmedMappings`: export contains the saved3–4 range for its owner only; deletion removes it; an old `WithAccountSession` cannot recreate it. Implement Memory export/deletion and Firestore collection export/cleanup plus atomic session validation for writes/clear.
- [x] Add `TestResettingResumePreservesConfirmedEpisodeMapping`: reset progress/remove its recent card; mapping still reads3–4. Implement no coupling to recent-progress pruning.
- [x] Run `FIRESTORE_EMULATOR_HOST=localhost:8086 go test -race ./internal/store -run 'Confirmed|EpisodeMapping|Account' -count=1` using the existing pinned emulator. Verify the emulator is live; start only if absent. Fix affected public fixtures that must satisfy the extended Store interface.
- [x] Commit: `feat(store): include episode mappings in account lifecycle`.

### Task 3: Finished progress uses the confirmed association

**Interfaces:** `func (e *Episodes) UseFileMappings(s store.EpisodeMappingStore)` opts the existing episode service into mapping lookup. Replace its private `markFinished` input with `(ctx context.Context, uid string, p store.Progress) error`; Progress passes the accepted canonical file record. Existing `MarkFinishedEpisodesIn` public wiring remains.

- [x] Add HTTP capability `TestFinishingConfirmedFileMarksItsCanonicalEpisodeRange`: save a mapping for an uncertain filename to series95396/S02E03–04; unfinished progress changes no episode; a subsequent finished report marks3and4 only, preserves another watched episode and file resume, and never marks the whole series. Verify red, implement minimum, rerun green.
- [x] Add one behavior at a time: confirmed mapping overrides a misleading exact filename; another hash/index/account or inconsistent known series/path cannot use it; no record retains existing exact-filename behavior; uncertain/no mapping changes none. No confirmation Save backfills past progress.
- [x] Add a public failure test through an unavailable external persistence boundary: failed mapping lookup marks nothing instead of falling back; progress remains saved. Return/report failure through the existing progress error path and make retry idempotent. Do not mock Episodes or other domain packages.
- [x] Run `go test -race ./internal/handlers ./internal/web -run 'Episode|Progress' -count=1` and commit: `feat(episodes): apply confirmed mappings at file completion`.

### Task 4: Authenticated confirmation routes and local account fencing

**Interfaces:** `NewEpisodeMappings(s store.EpisodeMappingStore, user func(*http.Request) (string, bool), catalog *Catalog, torrent func(context.Context, string) (torrserver.Torrent, bool)) *EpisodeMappings`; `Register(mux *http.ServeMux)` registers `GET/POST/DELETE /api/episodes/mapping`. GET renders the panel/options; POST explicitly saves; DELETE clears. A local torrent resolver verifies current metadata; web resolver is nil and validates browser-supplied account-owned context. Local handlers share the existing account-data mutex with username create/delete and preset operations.

- [ ] Add signed-in HTTP `TestUserCanConfirmOneEpisodeOrRange`: GET known series/file, POST season2/3–4, keyed store read equals submitted canonical range. Save itself leaves watched episodes and progress unchanged. Establish red, implement validation/save, green.
- [ ] Add validation/failure cycles: season0 special; equal first/last; missing/gapped episodes; reversed/huge range; malformed file identity; provider failure; persistence failure; signed-out requests; supplied foreign UID ignored. Validate membership before allocating a range. Preserve the previous confirmed record on any failure.
- [ ] Add GET/DELETE behavior: existing saved record shown; explicit clear returns to labeled filename fallback/unconfirmed; no history reversal or seek change. Use existing body limits, CSRF/middleware, deadlines, safe logging and no-store headers.
- [ ] Wire Memory/Firestore to actual web routes and local JSON to actual app routes. Rename/extend the existing `presetAccounts` guard to an account-data guard shared by mappings/presets/user create/delete; hold read lock while rechecking authentication and mutating local mappings.
- [ ] Add local public lifecycle tests: deletion cleanup plus recreating the same username exposes no old mapping; concurrent late requests cannot repopulate it; failed cleanup is retriable before username reuse. Preserve preset behavior.
- [ ] Run `go test -race ./internal/handlers ./internal/web -run 'EpisodeMapping|Confirmed|User|Account' -count=1`; commit: `feat(episodes): add account-scoped confirmation routes`.

### Task 5: Shared file menu and full browser behavior

**Interfaces:** `views.EpisodeMappingContext{Hash string; Index int; File string; TMDBID int}` and `EpisodeMappingView{Context EpisodeMappingContext; Series *tmdb.TVDetails; Season *tmdb.Season; Saved *store.EpisodeMapping; Problem string; Editing bool}`; `EpisodeMappingPanel(v EpisodeMappingView)` is the only mapping markup. Both transports mount it inside existing `torrFileMenuFor`. Add the known series ID to the shared file row/context; movie/unknown files do not request mapping UI.

- [ ] Add one shared rendered/HTTP test proving both actual transports render the same labeled Season/First/Last/Save/Clear controls for a known series file and omit them for movie/unknown identity. Confirm failure, implement only the missing shared component and adapter context, rerun green.
- [ ] Use native named form controls and Datastar request/SSE attributes; fetch only selected series/season options. Add English/Russian copy and its translation coverage. No server/browser TorrServer credentials enter the form or stored mapping.
- [ ] Bind fragment targets and submitted identity to the exact current file; ensure latest season response wins. No response for a previous file/dialog may reset global mapping form state. Test deliberately delayed fileA/season1 replies after fileB/season2 is selected and a reply after close/sign-out. Document the actual request-cancellation mechanism from current Datastar docs before implementing it.
- [ ] Disposable app/web browser loop: file More → Edit → confirm single/range → reopen/restart → same mapping; validation/store failure → retry; clear → labeled fallback; other account/file isolated. Check English/Russian at desktop/375px, keyboard focus after Save/error, Escape/close focus return and ordinary playback unchanged.
- [ ] Generated playable/native-media completion uses the confirmed range; another release retains canonical episode history but no mapping or transferred seek. Confirm no watched change on Save. Run relevant rendered/i18n/race suites and regenerate Templ/icons/assets through repository commands. Commit: `feat(player): confirm episode mappings in the shared file menu`.

### Task 6: Whole feature verification and delivery

- [ ] Self-audit every spec section against authoritative store, HTTP and browser evidence; keep production-media/VoiceOver limits explicit. Confirm all five Review Focus behaviors have their owning tests.
- [ ] Run `make test-store`, full `make ci`, and a final diff/whitespace check after staging regenerated assets. Inspect every result, including emulator availability; a skipped emulator test is not a pass.
- [ ] Request a fresh independent whole-branch review, fix important findings and rerun only affected checks/full checks when changes justify it.
- [ ] Commit approved design/plan and acceptance records with explicit owned paths; preserve unrelated `scripts/jacred-probe.py`. Push `codex/confirmed-episode-mappings`, create Conventional Commit PR, inspect remote review/checks, squash merge after all required checks pass.
- [ ] Verify normal web deployment for the actual merge commit, traffic revision and readiness. Update roadmap/audit with delivered/unverified limits; do not equate hosted delivery with republished desktop/container releases.

## Self-review and handoff

Spec coverage: shared interaction Task5; exact identity/persistence Tasks1–2; canonical validation and local/cloud boundary Task4; completion/resume/history Task3; isolation/failures Tasks1/3/4/5; privacy/account lifecycle Tasks2/4; checks/delivery Task6. No mapping product code before plan review. Unresolved product questions: none; user approved same-season range and exact identity. Execution-method confirmation remains.
