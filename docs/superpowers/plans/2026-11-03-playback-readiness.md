# Playback Readiness Implementation Plan

Use superpowers:executing-plans. Inline implementation; independent final review.

Goal: shared existing file menu explains connection and conversion capability;
container hints never imply codec compatibility. Refresh checks Echo only.

Design: docs/superpowers/specs/2026-11-02-playback-readiness-design.md.
Design/public render, HTTP/SSE, recorded external requests and disposable-browser
boundaries approved 3 November 2026. No production viewing or server mutations.

## 1. Connection-only refresh (vertical tracer)

Files: shared handler routes, new readiness HTTP test; existing Echo client.

- [x] One failing signed-in GET /api/torrserver/readiness test: fake TorrServer
      records only /echo and /gst/echo, SSE reports reachability/gst/gstKnown.
- [x] Implement bounded Echo-only route; no torrent-list, files, probe, stream,
      playlist or add calls. Reject signed-out access.
- [x] Incrementally cover disconnected/unknown/known-unsupported conversion and
      changed-server response suppression. Keep the existing live state current.

## 2. Shared file-menu explanation

Files: internal/views/torrserver.templ, web_player.templ, new shared readiness
component/adapter helper, Russian catalog and rendered regressions.

- [x] One failing public rendered-page regression. Reuse the native file dialog;
      add compact connection/conversion status, filename/container hint and
      Direct playback: Not checked. No additional modal.
- [x] Local signals derive from Echo; web expressions use existing tsStatus.
      Refresh transport is adapter-only; browser reuses tsCheck with its existing
      newest-response cancellation and keeps the selected server status current.
- [x] Extend shared row dataset/open action for a server-rendered extension hint.
      No filename or extension can show a compatible/guaranteed verdict.
- [x] Disconnection offers existing retry/settings; known conversion offers HLS
      with current file identity. Original VLC/IINA actions remain available.
- [x] Test rendered app/web parity, unknown states and translated text. Recorded
      external browser transport checks prove Refresh never loads/probes/plays.

## 3. Verify and deliver

- [x] Disposable fixture: menu open, Refresh, failed retry, long name, keyboard,
      Escape/focus, desktop/375 px and English/Russian. No real-account changes.
- [x] Generate templates/icons/assets; affected race/frontend tests; full make ci.
- [x] Required independent branch review; resolve demonstrated findings.
- [ ] Update roadmap, focused Conventional Commit PR, required CI and squash merge.

Unresolved questions: none. Presets PR delivery precedes implementation here.
