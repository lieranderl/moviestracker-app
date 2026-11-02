# Viewing Audit Fixes Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restore web library localization and accessible season navigation.

**Architecture:** Forward optional title naming in the web feed adapter; extend
the shared series links with current-page state driven by the existing signal.

**Tech Stack:** Existing Go, Templ, Datastar and DaisyUI.

**Spec:** `docs/superpowers/specs/2026-11-02-viewing-audit-fixes-design.md`

## Global constraints

Keep personal accounts unchanged, preserve unrelated files, no new dependencies
or duplicate web UI. User confirmed HTTP, rendered HTML and browser seams.

## Review focus

- External providers without optional naming must retain saved-name fallback.
- Naming failures must not remove saved entries or turn the page into an error.
- Localized filtering must pass through the real web adapter, not a local-only fixture.
- Current-season state must update after SSE navigation, not only initial HTML.
- Native navigation links must remain usable without JavaScript.

### Task 1: Web title naming

Files: `internal/web/lists.go`, `internal/web/localized_titles_test.go`.
Interface: signed-in GET `/lists/watched?q=Дюна` and optional
`Title(context.Context, string, int) (tmdb.MediaItem, error)` on the adapter.

- [x] Add one real-router regression with a language-aware external TMDB fixture;
      save Dune in English and find it by Russian name. Run red.
- [x] Forward naming through the adapter; run green.
- [x] Incrementally verify favourites, custom lists, recommendation headings,
      provider failure and providers without optional naming; preserve fallback.
- [x] Run affected race tests and commit the focused localization fix.

### Task 2: Season navigation

Files: `internal/views/tv.templ`, generated template, existing view tests.
Interface: rendered `views.TVPage`; browser-native keyboard operation.

- [x] Write one failing rendered-navigation test for a current season link.
- [x] Remove tab role, add initial/reactive `aria-current`; generate and run green.
- [x] Verify Tab/Enter and current-state updates using disposable browser data.
- [x] Run affected race checks; commit the focused accessibility fix.

### Task 3: Delivery

- [x] Run full `make ci`, review diff and update roadmap delivery evidence.
- [ ] Push branch, create Conventional Commit PR and enable squash auto-merge
      after required CI. Check final PR state.

Unresolved questions: none.
