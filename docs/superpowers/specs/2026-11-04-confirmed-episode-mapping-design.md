# Confirmed episode mappings (R18)

Status: design and test boundaries approved by the user on4 November2026. No mapping implementation has started; implementation plan review follows.

## Intent and agreed choices

Connect a known series' video file to its canonical episodes when its filename is uncertain. Keep file-based resume. Episode watched history continues to belong to the series/season/episode, so replacing a release does not lose that history or transfer a seek position.

The user chose:

- One episode or a consecutive range of episodes.
- Remember confirmation for the exact torrent and file. Another release needs its own confirmation.

## Approaches

Chosen: account-owned mappings keyed by normalized torrent info hash and TorrServer file ID. This uses the same file identity as existing progress and prevents another release from silently inheriting a guess.

Rejected: reuse by filename across releases. It saves clicks but identical or generic names do not prove the same episodes. Filename-only inference remains useful solely where the existing exact parser already recognizes an unambiguous season and episode range.

An in-memory association would disappear on restart and cannot satisfy R18 persistence.

## Shared interaction

Extend the existing native file-menu dialog in `internal/views/torrserver.templ`; app and web use the same mapping section and form. Existing playback, readiness and external-player controls remain usable when mapping metadata is unavailable.

For a video file associated with a known TMDB series, show its current episode association and an explicit Edit action. The shared form offers Season, First episode and Last episode, with episode numbers and names. Selecting the same first/last episode maps one episode. Specials use season zero. The proposed range stays within one season; cross-season compilations keep manual episode controls.

Save explicitly confirms the association. Clear removes only the confirmed file mapping and returns to existing exact-filename behavior, or to unconfirmed when the filename is uncertain. Label that fallback in the confirmation text. Saving, editing or clearing never starts playback, seeks, marks episodes watched, or reverses existing watched history.

Unknown/movie-only torrent identity does not offer episode mapping. It remains playable. A series-selection or torrent-title correction workflow is outside this change.

Use native labeled controls and existing DaisyUI components, Datastar requests/SSE and English/Russian render translations. Keep focus in the mapping section after Save or validation failure; native dialog Escape/close restores its invoking file action. No second setup screen or custom client state framework.

## Identity and persistence

A confirmed record contains hash, file ID, canonical series ID, season, first/last episode and the file path shown at confirmation. Hash is lowercase and validated as an existing supported40-character info hash; file ID and series ID are positive. The path is descriptive verification context, never a lookup across releases.

Keep records separate from the recent-progress window: resetting resume, removing a Continue card, or progress aging out must not erase a confirmed association. Re-adding the same torrent/file may reuse its record. Clearing the mapping or deleting the account removes it.

Expose a public mapping-store capability with keyed read, save and clear operations. Use real Memory/Firestore implementations for web and an owner-only, atomically replaced JSON file for the local app. Reads and writes always include the authenticated account identity. Never accept a user ID from the submitted form. A failed local write must leave both persisted and in-memory state unchanged.

Firestore uses one bounded record per file under the account, rather than loading the entire mapping collection for playback. Include mappings in account export and account deletion. Do not change existing account-data privacy headers. Local account removal must clear its mappings as it does other owned viewing data.

A request updates one file mapping. Do not scan an account's library or request every season to open a file menu. Load only the known series/selected season needed for confirmation, with the project's existing deadlines and metadata cache.

## Validation and completion

Validate the selected series and season against TMDB, including every episode in the inclusive range. Missing episodes, reversed ranges, unsupported seasons, invalid identity and unavailable metadata cannot produce a saved confirmation. Metadata errors offer retry and preserve the last saved record.

The local adapter can verify torrent/file metadata from its configured TorrServer. The cloud adapter supplies browser-held torrent context, following existing progress boundaries; the service cannot independently reach a private browser-only server. Treat that context as account-owned input and validate its shape and canonical episode membership. Do not add a cloud proxy to arbitrary TorrServer URLs.

On a subsequent accepted finished progress report, a matching confirmed record takes precedence over exact filename inference. Require the same account/hash/file ID and consistent known series/file context; a mismatch cannot update another series' episodes. Without a confirmed record, retain the existing exact-filename behavior. Uncertain files still mark no episode automatically.

Reuse the current `Progress.Finished` rule and `EpisodeStore.MarkEpisodes` canonical watched history. A confirmed range marks its episodes, never the whole series. Marking is idempotent and preserves other episodes. A mapping save does not backfill a past completion. Editing/clearing does not undo a previous completion; existing explicit episode controls handle corrections.

Mapping lookup failure must not silently fall back to a potentially overridden filename and mark the wrong episodes. Preserve file progress and fail closed for episode changes; record the failure through the existing progress/error path so retry is safe. Do not report a mapping write as successful before persistence succeeds.

## Request and UI isolation

Extend the existing request adapters for file identity and known series context; keep shared markup in `internal/views`. Bind returned fragments to their exact file context. Changing the selected file, closing the dialog, choosing another season or signing out must prevent a delayed response from changing a different file's panel or saved identity.

Do not trust hidden form fields as authorization. Scope all storage access to the signed-in account, validate input/body sizes, use existing CSRF protection and public error handling, and avoid logging private server addresses or file paths.

## Proposed acceptance boundaries

Reuse the previously approved categories, with this concrete R18 behavior subject to approval:

1. Public store contracts on Memory, local JSON/reopen and Firestore emulator: exact file/account isolation; single/range persistence; clear; invalid records; failed writes; concurrent distinct-file writes; mappings surviving progress reset; export/account cleanup.
2. Signed-in HTTP/SSE through actual app/web routes with external TMDB/TorrServer fixtures: canonical validation, unauthorized requests, save/edit/clear, provider/store failures, explicit override versus exact filename, completion of a range, idempotence, mismatched file/series and unchanged file resume.
3. Shared rendered UI and disposable Chromium/native Safari: both transports, English/Russian, desktop375px narrow layout, keyboard/dialog focus, Save failure/retry, file/season changes and deliberately delayed replies. Actual completion uses generated playable media or the approved native-media fixture boundary.

Follow one public seam and one failing behavior at a time. Do not fake internal domain packages or add tests that merely mirror private structure. Run full `make ci`, independent review, PR/remote checks, squash merge and normal hosted deployment. Desktop/container release publication remains a separate delivery step.

Production account/history and real TorrServer stay unchanged. VoiceOver stays unchanged and speech remains unverified. No device handoff, cross-release seek transfer, automatic uncertain guessing, retrospective watched updates or household collaboration.

## Approval items

Approve this proposed shared interaction, the same-season range limit, identity/validation and completion behavior, persistence/account lifecycle, and the concrete public test boundaries above. After approval, write the implementation plan before product code.
