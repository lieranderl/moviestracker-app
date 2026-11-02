# Reset Continue Watching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove an individual Continue watching card and reset its saved resume position.

**Architecture:** Extend the existing ProgressStore and shared Progress HTTP handler. Reuse the shared Continue watching view; retain zero-position records to suppress external resume fallback.

**Tech Stack:** Existing Go, Templ, Datastar, DaisyUI, memory/Firestore/file stores.

**Spec:** `docs/superpowers/specs/2026-11-02-reset-continue-watching-design.md`

## Global Constraints

- Individual removal only; preserve library, watched states and episode history.
- Shared app/web UI, Datastar-first interactions, semantic DaisyUI styling, English/Russian strings.
- Existing public store and authenticated HTTP/SSE test seams; no internal mocks.
- New branch; additive data behavior; no external TorrServer changes.

## Review Focus

- Old TorrServer timecodes must not restore a reset resume prompt.
- Other users and files must remain untouched; revoked sessions cannot mutate records.
- Repeated resets and missing records are harmless.
- Local reset persists across restart and failed persistence must not report success.
- Failed writes/reads retain useful UI feedback; removing the final entry removes the row heading.

### Task 1: Reset progress through the public store contract

Files: `internal/store/store.go`, `memory.go`, `firestore.go`, `progress_file.go`, `progress_test.go`, `account_test.go`.

Interface: `ResetProgress(context.Context, string, string, int) error` resets an existing file's position to zero; RecentProgress omits zero-position records.

- [x] Add one contract regression for resetting Dune while preserving another file/user. Run red, implement three stores and zero-position filtering, run green.
- [x] Add restart, invalid identity, idempotency, and revoked-session coverage incrementally; verify each behavior at the same seam.

### Task 2: Shared HTTP action and card control

Files: `internal/handlers/progress.go`, `internal/views/progress.templ`, `internal/views/movies.templ`, `internal/i18n/ru.go`; tests in `internal/web/progress_test.go`, `internal/handlers/progress_test.go`, and shared view tests.

Interface: authenticated `DELETE /api/progress?hash=<40 hex>&index=<positive file ID>`; SSE patches the shared row/status. Export the existing Continue watching component for handler reuse.

- [ ] Add one authenticated HTTP regression for removal and reset with a stale external timecode; run red, implement action/shared UI, run green.
- [ ] Incrementally cover invalid/signed-out requests, final-row removal, preserved history and other entries, store failure feedback, and local adapter behavior.
- [ ] Verify labelled controls outside the play link, pending disabling, keyboard/touch use, and Russian translations.

### Task 3: Delivery

- [ ] Generate Templ/icons/assets; run affected race tests with Firestore emulator, frontend tests, and full `make ci`.
- [ ] Review the final diff, verify browser behavior on desktop/mobile, and commit focused changes.
- [ ] Push `codex/reset-continue-watching` and open a Conventional Commit PR. Do not merge this requested new feature branch automatically.

Unresolved questions: none.
