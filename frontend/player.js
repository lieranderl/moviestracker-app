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

// A stream the browser cannot decode is reported to Datastar as a
// torr-unplayable event naming what it cannot play ("" once a stream starts),
// so the player says so instead of staying black.
const notifyUnplayable = (what) => videoEl()?.dispatchEvent(new CustomEvent("torr-unplayable", { detail: what }));

const aacProfiles = { 1: "AAC Main", 2: "AAC LC", 3: "AAC SSR", 4: "AAC LTP", 5: "HE-AAC", 29: "HE-AAC v2" };

// part names a part of a file in the page's language: "the HEVC video",
// or in Russian "видео HEVC".
const russianParts = { audio: "звук", video: "видео", format: "формат" };
const part = (name, kind) =>
  document.documentElement.lang === "ru" ? `${russianParts[kind]} ${name}` : `the ${name} ${kind}`;

// anyFormat is the file's format when the browser does not say which part it refuses.
const anyFormat = () => (document.documentElement.lang === "ru" ? "формат видео или звука" : "the video or audio format");

// describeCodec names an HLS CODECS entry for people: "mp4a.40.1" is
// "the AAC Main audio".
const describeCodec = (codec) => {
  const c = codec.toLowerCase();
  if (c.startsWith("mp4a.40.")) return part(aacProfiles[c.split(".")[2]] ?? "AAC", "audio");
  if (c === "ac-3") return part("Dolby Digital (AC-3)", "audio");
  if (c === "ec-3") return part("Dolby Digital Plus", "audio");
  if (c.startsWith("hvc1") || c.startsWith("hev1")) return part("HEVC", "video");
  if (c.startsWith("dvh1") || c.startsWith("dvhe")) return part("Dolby Vision", "video");
  if (c.startsWith("av01")) return part("AV1", "video");
  if (c.startsWith("vp09")) return part("VP9", "video");
  if (c.startsWith("avc1") || c.startsWith("avc3")) return part("H.264", "video");
  return part(codec, "format");
};

// unsupportedPart is what of these codecs this browser cannot decode.
const unsupportedPart = (codecs) => {
  const refused = codecs.find((c) => !window.MediaSource?.isTypeSupported(`video/mp4; codecs="${c}"`));
  return refused ? describeCodec(refused) : anyFormat();
};

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
  notifyUnplayable("");

  if (url.includes(".m3u8") && Hls.isSupported()) {
    const hls = new Hls({ enableWorker: true, lowLatencyMode: true, startPosition: resumeAt > 0 ? resumeAt : -1 });
    currentHls = hls;
    let codecs = [];
    hls.on(Hls.Events.MANIFEST_LOADED, (_, data) => {
      codecs = [...new Set(data.levels.flatMap((l) => [l.videoCodec, l.audioCodec]).filter(Boolean))];
    });
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
      if (
        data.details === Hls.ErrorDetails.MANIFEST_INCOMPATIBLE_CODECS_ERROR ||
        data.details === Hls.ErrorDetails.BUFFER_INCOMPATIBLE_CODECS_ERROR
      ) {
        // Retrying cannot help: this browser does not decode the stream.
        if (hls === currentHls) notifyUnplayable(unsupportedPart(codecs));
        destroyHls();
        return;
      }
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad();
      else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
      else destroyHls();
    });
  } else {
    // Native HLS (Safari) or progressive stream.
    if (resumeAt > 0) {
      video.addEventListener("loadedmetadata", () => (video.currentTime = resumeAt), { once: true });
    }
    video.addEventListener(
      "error",
      () => {
        if (currentUrl === url && video.error?.code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED) {
          notifyUnplayable(anyFormat());
        }
      },
      { once: true },
    );
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
  notifyUnplayable("");
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

// Launches an external player via its URL scheme. Resolves to whether it
// opened: browsers give no answer, but a player that opens (or the browser's
// "Open VLC?" prompt) takes the focus from this page, and a link nothing
// handles leaves it where it was.
window.openInPlayer = (player, streamPath, origin) => {
  const fullUrl = absoluteUrl(streamPath, origin);
  const targets = {
    vlc: `vlc://${fullUrl}`,
    iina: `iina://weblink?url=${encodeURIComponent(fullUrl)}`,
    potplayer: `potplayer://${fullUrl}`,
  };
  if (!targets[player]) return Promise.resolve(false);
  return new Promise((resolve) => {
    let left = false;
    const away = () => (left = true);
    window.addEventListener("blur", away);
    document.addEventListener("visibilitychange", away);
    window.location.href = targets[player];
    setTimeout(() => {
      window.removeEventListener("blur", away);
      document.removeEventListener("visibilitychange", away);
      resolve(left || !document.hasFocus());
    }, 1500);
  });
};
