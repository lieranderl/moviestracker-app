# Discovery Presets Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Save, apply, rename and delete account-owned Discover presets in both apps.

**Architecture:** Add a bounded preset store capability on existing user data and
an atomic local JSON backend. Inject it into the shared Catalog. Reuse Discover
query parsing/encoding and sibling Datastar forms with native management dialogs.

**Tech Stack:** Existing Go, Templ, Datastar, DaisyUI, memory/file/Firestore stores.

**Spec:** `docs/superpowers/specs/2026-11-02-discovery-presets-design.md`

## Global constraints

- 20 presets/account; 1–60 trimmed Unicode runes/name; stable IDs and newest-first order.
- Save only media type, genre, year, rating, sort, runtime and original language.
- Shared app/web UI, English/Russian translations, native dialogs, strict CSP.
- No production-account mutation; use disposable providers/accounts/media.
- Public store, authenticated HTTP/SSE and browser seams confirmed by user.

## Review focus

- Concurrent creates must not exceed the account limit or overwrite another preset.
- Failed file persistence and revoked sessions cannot produce successful writes.
- Rename preserves filters, creation order and unknown stored Firestore fields.
- Obsolete filters produce a visible notice; failed genre metadata cannot silently broaden results.
- Preset forms cannot nest inside Discover's auto-submitting GET form.

### Task 1: Persist a preset through public store contracts

Files: new `internal/store/discovery_presets.go`, `discovery_preset_file.go`,
`discovery_presets_test.go`; existing `store.go`, `memory.go`, `firestore.go`, account files.

Interfaces: `DiscoveryPresetStore` with `DiscoveryPresets`,
`CreateDiscoveryPreset`, `RenameDiscoveryPreset`, `DeleteDiscoveryPreset`.
`DiscoveryPreset` holds ID, Name, Filters, CreatedAt; filters are typed primitives.

- [x] Add one public-store create/list regression; run red, implement memory/file/
      Firestore capability and run green.
- [x] Add one behavior at a time for rename/delete, 20-limit and validation,
      cross-account isolation/copies, concurrent creates, file restart/write failure.
- [x] Extend public account export/deletion and revoked-session tests; implement
      complete inventory and transaction fences, then run emulator contracts.
- [x] Commit the verified storage slice.

### Task 2: Save and apply through authenticated HTTP

Files: new `internal/handlers/discovery_presets.go` and HTTP tests; Catalog,
Discover parser, local setup/routes and web setup.

Routes: POST `/api/discovery-presets` creates from current query plus name;
POST `/api/discovery-presets/{id}/rename`; DELETE `/api/discovery-presets/{id}`;
GET `/discover/presets/{id}` validates the owner's preset and redirects to the
canonical Discover URL. All mutation responses patch the shared preset section.

- [x] Start with one failing signed-in save/apply regression; implement only
      that tracer behavior, preserving ordinary Discover URLs.
- [x] Incrementally cover rename/delete, signed-out/foreign IDs, invalid names,
      body bounds, failed writes, unsupported filters and provider failures.
- [x] Applying obsolete fields leaves the saved record intact; use a transient
      notice flag in the redirect and never save page/reroll or unknown input.
- [x] Wire memory/file/web adapters without duplicate rendering; run affected
      race tests; commit after the full gate.

### Task 3: Shared controls and browser journey

Files: new shared preset template; `discover_page.templ`, generated outputs,
`ru.go`, translation inventory, web privacy inventory and tests.

- [x] Add rendered-control regression; implement compact sibling Save/Apply
      controls and native Rename/Delete dialogs using existing helpers.
- [x] Add translated errors, pending disabling, read-failure Retry and obsolete
      notice; keep successful Discover results available if presets cannot load.
- [x] Verify save/change/apply/reload/rename/delete, keyboard/Escape/focus,
      desktop/375 px and English/Russian with disposable accounts.
- [x] Run icons/assets/templates, frontend and affected race tests.

### Task 4: Verify and deliver

- [x] Run full `make ci` and emulator checks; independent whole-branch review.
- [x] Resolve any review findings through red/green tests; check final diff.
- [ ] Update roadmap evidence; push focused Conventional Commit PR and enable
      squash auto-merge after CI. Verify merge and applicable deployment status.

Unresolved questions: none. PR #144 merged at ad2f002; this branch was rebased onto it.

Verification (3 November 2026): affected race suites and Firestore emulator
contracts passed. Disposable Chrome verified save/change/apply/reload/rename/
delete, Escape and focus return, pending recovery, English desktop and Russian
375 px without document overflow. A failed local write kept the dialog and its
input. Review findings on local account cleanup/fencing and unknown file fields
were reproduced and fixed. Full gate and remote delivery remain in progress.

Full `make ci` passed before rebasing onto the latest dependency-only main.
Revalidation on the refreshed base remains required before PR delivery.
