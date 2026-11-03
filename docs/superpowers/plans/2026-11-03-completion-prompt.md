# Completion Prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Offer existing rating/watch-state controls and similar movies after a qualifying movie ends.

**Architecture:** One dismissible panel inside the shared player. Datastar native media events track eligibility; a read-only authenticated endpoint resolves canonical movie identity from that account's saved progress. Reuse existing rating/watch controls with player-specific response targets so delayed saves cannot replace controls for another file.

**Tech Stack:** Go, templ, Datastar 1.0, DaisyUI 5; existing stores and endpoints.

**Spec:** docs/superpowers/specs/2026-11-02-completion-prompt-design.md (approved 3 November 2026).

## Global Constraints

- Actual `ended` plus at least 60 seconds of forward playback since start/latest seek/reconnect; position jumps, paused/waiting time do not count.
- Known canonical movie only. Unknown identity, series, unknown queue, or an existing next queued file suppress the prompt.
- One prompt per file/page session. New selection/retry clears visible state and eligibility; never clear the duplicate guard on retry.
- No new automatic rating, watch-state or history write. Existing progress/watch-completion behavior remains separate.
- Shared app/web component and player; declarative Datastar events/signals; native dialog remains the existing player.
- User account and real TorrServer unchanged. Only disposable media/accounts may be written during tests.

## Review Focus

- Seek near the end, playback-rate change and background event gaps must not manufacture 60 seconds of playback (Task 2).
- A slow response for an old file must not display a prompt or overwrite a newer file's controls (Tasks 1, 3).
- Saving a rating must preserve the private note that is loaded with it (Task 3).
- Failed identity/store reads suppress actions; another account's progress/review must never supply them (Task 1).
- Queue advance, retries and repeated ended events must preserve normal playback and avoid duplicate prompts (Task 2).

## Task 1 — Read-only, account-owned completion response

Files: new `internal/handlers/completion.go`, `completion_test.go`; route registration in `internal/handlers/handlers.go` and `internal/web/server.go`; new shared `internal/views/completion.templ` and rendered regression.

Interface: `NewCompletion(progress store.ProgressStore, reviews store.ReviewStore, titles store.TitleStateStore, user func(*http.Request) (string, bool))`; `Register(*http.ServeMux)` adds authenticated GET `/api/player/completion?hash=<40hex>&index=<positive>&nonce=<positive>`.

- [x] Red: signed-in HTTP request with that user's saved movie progress returns a panel for that canonical movie, rating/watch-state snapshot and similar link; public stores remain unchanged.
- [x] Green: validate identifiers, bounded store reads, canonical `Kind == movie`/positive TMDB ID, no-store SSE. Unknown/series/missing progress returns 204; no guessing from filename/title.
- [x] Incremental red/green: signed-out 401, invalid target 400, account isolation, failed reads, unknown identity, series. No TorrServer/catalog/media requests.
- [x] Render one shared panel with target key `c_<hash>_<index>_<nonce>`; patch only the slot whose live data-request-key matches. Visibility also requires current file/nonce, player open and completion-open signal. Dismiss does no HTTP request.
- [x] Verify affected handler/web/view race suites; commit read-only vertical slice.

## Task 2 — Actual completion eligibility at native media events

Files: `internal/views/completion.templ`, shared `torrserver.templ`, adapter expressions in `web_player.templ`; approved disposable native-media fixture.

Interface: shared completion signals, reset/timeupdate/ended attribute expressions; `completionEnded(browser bool)` preserves the existing `playerNext(browser)` behavior after eligibility handling.

- [x] Red: rendered app/web player plus native playable fixture ends after over 60 seconds of actual playback and requests exactly one completion panel.
- [x] Green: track bounded positive position/time deltas only while playing and not seeking/waiting. Count elapsed playback time, accounting for playback rate; reject long event gaps and position jumps. Reset on seek, emptied/reconnect and new playback nonce/file.
- [x] At ended, require known queue with current file at final position, elapsed >=60, and no prior prompt for the server/file identity. Set request key then GET Task 1; queue advance keeps existing behavior.
- [x] Incremental public/native checks: below 60 seconds, seek-to-end, paused/waiting time, reconnect, repeated ended, new file, series/unknown identity, unknown queue, next queued file, rate change and long event gap.
- [x] Verify direct and HLS/retry events share the same reset path; no imperative UI listeners/timers. Commit eligibility slice.

## Task 3 — Reuse existing explicit actions safely

Files: `internal/views/reviews.templ`, `watch_states.templ`, `completion.templ`; `internal/handlers/reviews.go`, `title_states.go`; corresponding signed-in HTTP regressions.

Interface: optional validated player target scope on existing review/watch URLs. Default title-page IDs/signals stay the existing behavior. Completion controls use IDs and review signal paths qualified by Task 1's key; responses target that same key.

- [x] Red: completion rating/watch action uses the existing authenticated endpoint, updates only the acting user's selected movie and patches only its qualified control target. Old-file replies cannot patch the new panel.
- [x] Green: extend existing shared components through a small context/adapter scope, with qualified target IDs and rating/note signals for the player. No duplicated rating/watch layout or separate mutation endpoint.
- [x] Load existing rating and note together before enabling rating; preserve note in explicit save. New panel reads failure never masquerades as empty values. Keep existing rating-marks-watched semantics explicit.
- [x] Incremental red/green: saved note preservation, failed write/retry, invalid scope, account isolation, stale response, ordinary title-page behavior unchanged. Similar link goes to the canonical movie's existing `#similar` rail.
- [x] Browser: dismiss without mutation/focus steal, existing player close/Escape, explicit rating/watch actions on disposable account only, long titles, desktop/375 px English/Russian.
- [x] Generate templates/icons/assets; affected race tests, full `make ci`; independent whole-branch review and local roadmap update.
- [ ] Focused PR, all remote CI, squash merge and verify normal web deployment.

Unresolved questions: none in the approved design. Design and inline plan approved; implementation and final verification in progress. Delivery remains pending.
