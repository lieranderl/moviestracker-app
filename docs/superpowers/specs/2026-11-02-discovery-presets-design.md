# Saved discovery presets (R22)

## Goal

Save a useful Discover filter combination once and reuse it on later visits,
on both the self-hosted app and cloud web app. Extend the existing Discover
form; retain its GET URLs and tonight shortlist. No new discovery screen.

Approved by user on 2 November 2026, including limits, UI and store/HTTP/browser
test boundaries. Implement inline; production account data remains unchanged.

## Decisions

- Up to **20 presets per account**, each named with **1–60 trimmed Unicode
  characters**. Stable random IDs; names need not be unique. The UI identifies
  each preset separately and renaming never replaces another preset.
- Save the current media type, genre, year, minimum rating, sort, maximum
  runtime and original language. Never save pagination, tonight draw number,
  CSRF/session data or arbitrary query parameters.
- The shared Discover page shows a compact Saved filters section next to the
  existing form, with Apply and a native management dialog for rename/delete.
  Save current filters uses a named input and explicit submit. Do not expand
  the management controls by default or add a separate setup flow.
  Naming and management forms are siblings of the auto-submitting Discover
  form, never nested inside it. A small preset component is necessary because
  Discover has no existing account-owned filter manager; reuse generic form
  and dialog components rather than introducing a page.
- Applying a preset navigates to the ordinary canonical Discover URL. It does
  not change global preferences, auto-play or download anything. Reload,
  browser Back and copied filter URLs continue to work.
- Revalidate applied filters against current supported controls and genre
  metadata. Remove obsolete values with a visible notice; if genre metadata
  cannot be fetched, show retry rather than silently dropping the genre.
  Unknown future stored fields are ignored without modifying other data.
  Applying obsolete filters leaves the saved record unchanged; the canonical
  redirect carries a transient notice flag for the resulting page.
- Explicit Save creates a preset; Rename changes only its name; Delete
  removes only that preset. There is no automatic overwrite of a preset when
  the user experiments with filters. Failed writes retain the dialog and show
  a translated error. Pending controls are disabled declaratively.

## Storage and adapters

Introduce `DiscoveryPresetStore` with list/create/rename/delete capabilities.
Keep typed filter fields in store records, independent of provider/view types.
Memory uses its existing account lock/session fence; Firestore stores a bounded
map on the user document through an account-fenced transaction. The self-hosted
app uses an owner-only atomic JSON file, reverting in-memory changes if the
write fails. No new Firestore index or background process is required.

Lists return copies; another account cannot read or mutate a preset. Limits
are enforced atomically, including concurrent creates. Missing rename returns
not found; repeated deletion is harmless. A revoked session cannot recreate
data after deletion. Include presets in account export/deletion and privacy
inventory. Old records without presets keep their existing behavior.
List presets by creation time, newest first, with ID as a deterministic tie
breaker; count name length in Unicode runes. Rename changes only the targeted
name and preserves unknown stored fields. A list-read failure shows a retry
inside the preset section without hiding otherwise available Discover results.

The shared Catalog handles HTTP/SSE and renders the existing views. The local
and web adapters inject their respective store and identity functions. Reuse
existing native dialogs, Datastar attributes, semantic DaisyUI components,
Lucide icons, English/Russian translation and strict CSP.

## Alternatives

Browser bookmarks are useful but cannot provide named account-owned presets,
rename/delete controls or cross-device persistence. Browser-only localStorage
would diverge from account export and the shared app/web design. Persistent
presets through the existing user stores fit the accepted roadmap.

## Confirmed acceptance boundaries

- Public store contract on memory, file and Firestore emulator: create/list,
  account isolation, copies, rename, delete, 20-preset limit, validation,
  failed persistence, concurrent operations and revoked sessions.
- Signed-in HTTP/SSE on app/web: save current validated filters, apply canonical
  URL, obsolete/provider-failure behavior, rename/delete, failed writes,
  signed-out access, validation, export/deletion and translated UI.
- Disposable browser: save → change filters → apply → reload → rename → delete;
  keyboard, native-dialog Escape/focus return, desktop/375 px, English/Russian.

## Delivery

Use one vertical red/green slice at a time, then affected race tests, emulator,
frontend tests, full `make ci`, independent review and a focused PR. No mutation
of the user's production viewing data during validation.

Unresolved questions: none. Everything uses the existing stack.
