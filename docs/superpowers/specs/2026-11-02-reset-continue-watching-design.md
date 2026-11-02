# Reset an individual Continue watching entry

User-approved scope: individual removal only, without Clear all. Preserve favourites, lists, ratings, movie watched state, and episode history. Implement on `codex/reset-continue-watching`, based on synced main.

One shared action on each existing card removes that card's file and resets its resume position. Existing progress is file-based; this does not reset other episodes or releases. Playing again records fresh progress and restores the card.

Technical choice: keep a zero-position record rather than deleting it. The existing resume handler uses TorrServer's timecode only when Moviestracker has no record; the reset must suppress that fallback. Do not change the user's external TorrServer timecodes. Existing local retention limits still apply.

Extend the existing ProgressStore with an idempotent ResetProgress(ctx, uid, hash, index) operation. Validate the file identity, preserve metadata and ordering, keep account-session fences, and persist local resets across restarts. A zero-position record is omitted from recent progress.

Extend the shared progress HTTP adapter with authenticated DELETE /api/progress?hash=...&index=.... On success, patch the shared Continue watching row; remove its visible content when empty. Failures retain controls and show a translated retryable message. Use Datastar, semantic DaisyUI buttons, English/Russian copy, accessible labels and pending state; no new client script.

Validation uses the previously approved public store contracts (memory, Firestore emulator, local file), authenticated HTTP/SSE, shared view tests, and browser checks. Cover isolation, revoked sessions, invalid identities, idempotency, restart persistence, TorrServer fallback suppression, other progress/history preservation, and failure feedback. Run full CI. Push a focused PR; keep this feature on its requested new branch for review.
