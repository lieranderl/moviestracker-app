# Playback readiness (X01) — approved design

Goal: tell the viewer what is known before they choose a playback route, without
starting a torrent, a stream, a conversion task or a metadata probe as a side
effect. Extend the shared file menu and existing connection/stream explanations.

## Evidence and proposed scope

The adapters already expose server reachability and separate known/unknown
GStreamer capability (`gst`/`gstKnown` locally, `tsStatus.hls`/`hlsKnown` in the
browser). A file already present in the UI supplies its name/container; tracker
metadata and filename extensions cannot establish browser codec compatibility.

Add a compact readiness group to the existing file menu. No new modal or setup
screen: opening the existing menu displays connection and conversion status,
known filename/container, and a direct-playback result of Not checked. Expose
known unsupported conversion separately from a failed/unknown check. A status
failure links to existing connection retry/settings; known conversion offers
HLS; the original-file VLC/IINA actions remain available when reachable.

Readiness itself reuses the most recent connection check. An explicit Refresh
only checks Echo/connection capabilities; it never calls stream, files/probe,
playlist-play or add operations. A filename such as `.mp4` is a container hint,
not a green compatibility badge. Do not label HLS as guaranteed in every browser.
Unknown metadata stays unknown; actual playback errors retain current recovery.

Use one shared UI component, Datastar attributes and translated semantic DaisyUI
copy. Transport-specific connection values/refresh stay in adapters. No custom
JavaScript for state, timers or presentation.

## Proposed verification

Public rendering and authenticated app/web HTTP/SSE boundaries show connected,
disconnected, unknown conversion and known unsupported states. External
TorrServer request recording proves readiness never starts a stream, conversion,
file metadata load, probe or add. Browser fixtures cover a ready connection,
failure/retry, a long filename and keyboard/mobile file-menu use. Real media
compatibility remains a separate test with disposable media and accounts.

Status: design and test boundaries approved by the user on 3 November 2026.
Production server and account mutations remain outside the verification scope.
