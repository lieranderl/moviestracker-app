# Optional completion prompt (X02) — design draft

Goal: offer rating, explicit watched state and similar titles after a movie
ends, using existing controls. Never interrupt credits, auto-rate or introduce
automatic playback. Reuse the shared player and title identity adapters.

## Trigger decision

`store.Progress.Finished` currently means 92% played or the final three minutes.
Keep that existing watched-state rule separate: it is too early for this prompt.
The prompt uses the player's actual `ended` event, a known canonical movie
identity and evidence of forward playback rather than seeking to the end.
Unknown identity and series files bring no movie prompt. A replay/reconnect of
the same ended file must not bring duplicate prompts in that player session.

Proposed guard: require at least 60 seconds of actual forward playback since
playback started or the most recent seek/reconnect. Paused, waiting, seeking and
position jumps do not contribute; replaying an already prompted file in the
same page session cannot prompt again. This deliberately favors missing a
prompt over showing one after a seek. The implementation plan must define and
test this evidence at the native media-event boundary. `ended` alone is
insufficient because seeking can also reach it. No server inference from a
client-supplied position may trigger a prompt early.

Existing file-queue behavior takes precedence: if the current ending advances
to another queued file, suppress the movie prompt. Only the final file with
verified canonical movie identity can offer it. Series-next behavior remains
unchanged; uncertain identity or queue state suppresses the prompt.

## Presentation

Show a dismissible panel in the existing player after playback has ended; do
not stack a second modal over the player or steal focus. Reuse the existing
rating/watch-state actions and a link to the canonical title's similar titles.
Explicit actions use the existing authenticated endpoints. Dismissal affects
only the panel; no rating/watch/history mutation follows opening or dismissal.
New file selection clears old panel state. Pause, close and retry do not trigger
it. Keep any existing series-next behavior separate from this movie feature.

## Proposed verification

Native media-event tests exercise normal ending, seeking, reconnect/retry,
repeated ended events, unknown identity, series files and changing file. Shared
render/HTTP tests verify identity/account isolation and existing actions. A
disposable playable fixture verifies ending, dismissal, focus and narrow layout
without modifying the user's real viewing data.

Status: design, 60-second guard, queue precedence and test boundaries approved
by the user on 3 November 2026.
Test boundaries: native media events, rendered shared UI, signed-in HTTP/SSE and
a disposable playable browser fixture. No real-account viewing writes.
