// TorrServer page helpers. Loaded only on /torrserver as an ES module.
// Everything that Datastar can express declaratively (modals, toasts,
// heartbeat polling, state) lives in the templates; this file only wraps
// third-party/browser APIs that have no declarative equivalent.
import Hls from "hls.js";

let currentHls = null;
let currentUrl = null;

const videoEl = () => document.getElementById("torr-video");

// Links leave this browser for other devices (and players), so they use the
// server's address for them ($linkOrigin): never localhost.
const absoluteUrl = (path, origin) =>
  path.startsWith("http") || path.startsWith("magnet:") ? path : (origin || window.location.origin) + path;

const destroyHls = () => {
  currentUrl = null;
  if (currentHls) {
    // Clear first: tearing down fires a final "subtitles off" switch that
    // must not overwrite the remembered choice.
    const hls = currentHls;
    currentHls = null;
    hls.destroy();
  }
  // hls.js leaves its subtitle <track> elements behind (still "showing");
  // the next stream would inherit them and skip its own subtitle selection.
  videoEl()?.querySelectorAll("track").forEach((t) => t.remove());
};

// Subtitles can be picked from the captions button over the video or the
// <video>'s native captions menu (hls.js exposes the HLS subtitle renditions
// as text tracks). Either way the choice is remembered and reported to
// Datastar as a torr-subtitle event carrying the track id (-1 = off).
const SUB_KEY = "torrSubtitle";

const loadSubPref = () => {
  try {
    return JSON.parse(localStorage.getItem(SUB_KEY)) || null;
  } catch {
    return null;
  }
};

const saveSubPref = (track) => {
  try {
    localStorage.setItem(SUB_KEY, JSON.stringify(track ? { lang: track.lang, name: track.name } : null));
  } catch {}
};

const notifySubtitle = (id) => videoEl()?.dispatchEvent(new CustomEvent("torr-subtitle", { detail: id }));

window.setSubtitleTrack = (id) => {
  if (currentHls) {
    currentHls.subtitleTrack = id;
    return;
  }
  // Native HLS (Safari): the renditions are the video's text tracks.
  const video = videoEl();
  if (!video) return;
  [...video.textTracks].forEach((t, i) => (t.mode = i === id ? "showing" : "disabled"));
  notifySubtitle(id);
};

// Resolves the remembered choice against this manifest's subtitle renditions:
// the exact one (language + name), else any in that language. hls.js applies
// it via setSubtitleOption once the manifest is parsed.
const subtitlePreference = (tracks) => {
  const pref = loadSubPref();
  if (!pref?.lang) return undefined;
  const match =
    tracks.find((t) => t.lang === pref.lang && t.name === pref.name) ?? tracks.find((t) => t.lang === pref.lang);
  return match && { lang: match.lang, name: match.name };
};

// Starts playback. Reloading the same <video> (e.g. after an audio-track
// switch, which is a new GStreamer stream) resumes at the current position.
// Repeat calls for the stream already loaded are ignored: a second reload
// would read currentTime after the first one reset it to 0.
window.playHlsVideo = (url) => {
  const video = videoEl();
  if (!url || !video || url === currentUrl) return;
  const resumeAt = video.currentSrc || currentHls ? video.currentTime : 0;
  destroyHls();
  currentUrl = url;

  if (url.includes(".m3u8") && Hls.isSupported()) {
    const hls = new Hls({ enableWorker: true, lowLatencyMode: true, startPosition: resumeAt > 0 ? resumeAt : -1 });
    currentHls = hls;
    hls.loadSource(url);
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
      const pref = subtitlePreference(data.subtitleTracks ?? []);
      if (pref) hls.setSubtitleOption(pref);
      video.play().catch(() => {});
    });
    hls.on(Hls.Events.SUBTITLE_TRACK_SWITCH, (_, data) => {
      if (hls !== currentHls) return;
      saveSubPref(hls.subtitleTracks[data.id]);
      notifySubtitle(data.id);
    });
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (!data.fatal) return;
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad();
      else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
      else destroyHls();
    });
  } else {
    // Native HLS (Safari) or progressive stream.
    if (resumeAt > 0) {
      video.addEventListener("loadedmetadata", () => (video.currentTime = resumeAt), { once: true });
    }
    video.src = url;
    video.play().catch(() => {});
  }
};

window.stopHlsVideo = () => {
  const video = videoEl();
  if (video) {
    video.pause();
    video.removeAttribute("src");
    video.load();
  }
  destroyHls();
};

// Copies a stream/magnet/playlist link. Resolves to true on success so the
// caller can drive the toast via a Datastar signal.
window.copyLink = async (path, origin) => {
  const text = absoluteUrl(path, origin);
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
    // Clipboard API is unavailable on insecure origins (plain-HTTP LAN access).
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("readonly", "");
    textarea.className = "fixed opacity-0 pointer-events-none";
    document.body.append(textarea);
    textarea.select();
    const ok = document.execCommand("copy");
    textarea.remove();
    return ok;
  } catch (err) {
    console.error("Failed to copy:", err);
    return false;
  }
};

// Launches an external player via its URL scheme.
window.openInPlayer = (player, streamPath, origin) => {
  const fullUrl = absoluteUrl(streamPath, origin);
  const targets = {
    vlc: `vlc://${fullUrl}`,
    iina: `iina://weblink?url=${encodeURIComponent(fullUrl)}`,
    potplayer: `potplayer://${fullUrl}`,
  };
  if (targets[player]) window.location.href = targets[player];
};
