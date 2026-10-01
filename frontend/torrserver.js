// The cloud web app's TorrServer client (internal/web): the visitor's own
// TorrServer is reached from their browser, never from the server, so these
// calls are the one part Datastar cannot make itself (Basic auth, TorrServer's
// JSON API). Results go back to Datastar as events on the element that asked:
// data-on:ts-status="$tsStatus = evt.detail".
//
// A TorrServer's login stays in this browser (localStorage, by address).

const loginKey = (url) => `mt-ts-login:${url}`;
const timeoutMs = 6000;

const readLogin = (url) => {
  try {
    return JSON.parse(localStorage.getItem(loginKey(url)) || "null");
  } catch {
    return null;
  }
};

// tsSaveLogin keeps a TorrServer's login in this browser; an empty user
// forgets it.
window.tsSaveLogin = (url, user, pass) => {
  try {
    if (user) localStorage.setItem(loginKey(url), JSON.stringify({ user, pass: pass || "" }));
    else localStorage.removeItem(loginKey(url));
  } catch {
    // Storage blocked: the login lasts until the page closes.
  }
};

// tsForgetLogin forgets a TorrServer's login in this browser.
window.tsForgetLogin = (url) => window.tsSaveLogin(url, "", "");

// base64 encodes text as UTF-8, so logins beyond Latin-1 (Cyrillic) work;
// btoa alone takes Latin-1 only.
const base64 = (text) => btoa(String.fromCharCode(...new TextEncoder().encode(text)));

const headers = (url) => {
  const login = readLogin(url);
  return login ? { Authorization: `Basic ${base64(`${login.user}:${login.pass}`)}` } : {};
};

// call is a request to the TorrServer at url whose body read reads, answered
// with { value } or a problem the page explains: "mixed" (an http address on
// an https page, not this computer), "unreachable" (not running, blocked, or
// its certificate not trusted), "login" (it wants a login), "timeout",
// "status", or "aborted" (signal: a newer request replaced this one). The
// timeout covers reading the body too.
const call = async (url, path, { init = {}, read = (res) => res.text(), signal, timeout = timeoutMs } = {}) => {
  const target = new URL(path, `${url}/`);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname);
  if (window.location.protocol === "https:" && target.protocol === "http:" && !local) {
    return { problem: "mixed" };
  }
  const ctl = new AbortController();
  const abort = () => ctl.abort();
  signal?.addEventListener("abort", abort);
  const timer = setTimeout(abort, timeout);
  try {
    const res = await fetch(target, { ...init, headers: { ...headers(url), ...init.headers }, signal: ctl.signal });
    if (res.status === 401) return { problem: "login", status: 401 };
    if (!res.ok) return { problem: "status", status: res.status };
    return { value: await read(res) };
  } catch (err) {
    if (signal?.aborted) return { problem: "aborted" };
    return { problem: err.name === "AbortError" ? "timeout" : "unreachable" };
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", abort);
  }
};

const tell = (el, name, detail) => el?.dispatchEvent(new CustomEvent(name, { detail }));

// latest is each element's request in flight: a newer one aborts it, so a
// slow answer from a server picked before never overwrites the current one.
const latest = new WeakMap();

const begin = (el) => {
  latest.get(el)?.abort();
  const ctl = new AbortController();
  latest.set(el, ctl);
  return ctl.signal;
};

// status is a full ts-status detail: Datastar merges an object assigned to a
// signal, so every field is always sent.
const status = (fields) => ({ checking: false, ok: false, version: "", hls: false, problem: "", status: 0, ...fields });

// canConvert says whether a -gst build's GStreamer works: its /gst/echo
// answers 200 with a health report ({"gstreamer": {"works": true}, …});
// other builds have no /gst/echo.
const canConvert = async (url, signal) => {
  const gst = await call(url, "gst/echo", { read: (res) => res.json(), signal });
  return gst.value?.gstreamer?.works === true;
};

// tsCheck asks the TorrServer at url for its version and whether it can
// convert to HLS, then fires ts-status on el: { ok, version, hls } or
// { ok: false, problem, status }. A newer check on el discards this one.
//
// It fires nothing before its first await: called from a data-effect, an
// event handled synchronously would make the effect depend on the signals
// the handler sets ($tsStatus), and every answer would start another check.
window.tsCheck = async (el, url) => {
  if (!url) return;
  const signal = begin(el);
  await Promise.resolve();
  if (signal.aborted) return;
  tell(el, "ts-status", status({ checking: true }));
  const echo = await call(url, "echo", { signal });
  if (signal.aborted) return;
  if (echo.problem) {
    tell(el, "ts-status", status({ problem: echo.problem, status: echo.status || 0 }));
    return;
  }
  const hls = await canConvert(url, signal);
  if (signal.aborted) return;
  tell(el, "ts-status", status({ ok: true, version: echo.value.trim(), hls }));
};

// shown is the list each element last reported, so an unchanged list is not
// posted again.
const shown = new WeakMap();

// tsList reads the TorrServer's torrents and, when they changed since el
// last reported them (or force), fires ts-torrents on el with them; the page
// posts them to the server, which renders them. Like tsCheck, it fires
// nothing before its first await.
window.tsList = async (el, url, force = false) => {
  if (!url) return;
  const signal = begin(el);
  await Promise.resolve();
  if (signal.aborted) return;
  const list = await call(url, "torrents", {
    init: { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action: "list" }) },
    read: async (res) => { const value = await res.json(); return Array.isArray(value) ? value.map(withFiles) : value; },
    signal,
  });
  if (signal.aborted || list.problem || !Array.isArray(list.value)) return;
  const key = `${url} ${JSON.stringify(list.value)}`;
  if (!force && shown.get(el) === key) return;
  shown.set(el, key);
  tell(el, "ts-torrents", list.value);
};

// tsTorrentAction asks the TorrServer to drop a torrent's cache ("drop") or
// remove it ("rem"), then lists its torrents again, unless another
// TorrServer was picked meanwhile (the page keeps the pick in
// localStorage): that one's list is already being read.
window.tsTorrentAction = async (el, url, action, hash) => {
  if (!url || !["drop", "rem"].includes(action)) return;
  await call(url, "torrents", {
    init: { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, hash }) },
  });
  let picked = url;
  try {
    picked = localStorage.getItem("mt-ts-selected") ?? url;
  } catch {
    // Storage blocked: assume the pick did not change.
  }
  if (picked === url) await window.tsList(el, url, true);
};

// tsAddTorrent sends the form's torrent straight to TorrServer. Datastar
// owns the progress, errors and list refresh through ts-added events.
const webAddress = (value) => {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password;
  } catch {
    return false;
  }
};

const torrentLink = (value) => {
  if (webAddress(value)) return true;
  try {
    const url = new URL(value);
    return url.protocol === "magnet:" && url.searchParams.getAll("xt").some((xt) => /^urn:btih:(?:[a-f0-9]{40}|[a-z2-7]{32})$/i.test(xt));
  } catch {
    return false;
  }
};

// A server selection change cancels the old form operation. Like tsCheck,
// this reports after an await so a data-effect never tracks its own result.
const adding = new WeakMap();

window.tsResetAdd = async (el) => {
  adding.delete(el);
  const signal = begin(el);
  await Promise.resolve();
  if (!signal.aborted) tell(el, "ts-added", { adding: false, ok: false, problem: "", status: 0 });
};

window.tsAddTorrent = async (el, url, { link = "", title = "", poster = "", file, mediaId, mediaType } = {}) => {
  if (adding.has(el)) return;
  const signal = begin(el);
  adding.set(el, signal);
  try {
    await Promise.resolve();
    if (signal.aborted) return;
    tell(el, "ts-added", { adding: true, ok: false, problem: "", status: 0 });
    link = link.trim();
    title = title.trim();
    poster = poster.trim();
    let valid = webAddress(url) && (!poster || webAddress(poster));
    if (file) {
      valid = valid && !link && /\.torrent$/i.test(file.name) && file.size > 0 && file.size <= (4 << 20);
      if (valid) valid = (await file.slice(0, 1).text().catch(() => "")) === "d";
    } else {
      valid = valid && torrentLink(link);
    }
    if (signal.aborted) return;
    if (!valid) {
      tell(el, "ts-added", { adding: false, ok: false, problem: "input", status: 0 });
      return;
    }
    let path = "torrents";
    const metadata = Number.isSafeInteger(mediaId) && mediaId > 0 && ["movie", "tv"].includes(mediaType)
      ? { category: mediaType, data: JSON.stringify({ tmdb: { id: mediaId, type: mediaType } }) }
      : {};
    let init = {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "add", link, title, poster, save_to_db: true, ...metadata }),
    };
    if (file) {
      const body = new FormData();
      body.append("file", file, file.name);
      body.append("save", "true");
      body.append("title", title);
      body.append("poster", poster);
      path = "torrent/upload";
      init = { method: "POST", body };
    }
    // Adding may wait for torrent metadata, unlike connection checks.
    const result = await call(url, path, {
      init, signal, timeout: 30000,
      read: (res) => res.json().catch(() => null),
    });
    if (signal.aborted) return;
    // The upload endpoint can answer HTTP 200 with null when it rejected
    // the file. Only a returned torrent confirms it was actually added.
    if (!result.problem && !/^[a-f0-9]{40}$/i.test(result.value?.hash || "")) {
      result.problem = "status";
      result.status = 200;
    }
    tell(el, "ts-added", { adding: false, ok: !result.problem, problem: result.problem || "", status: result.status || 0 });
  } finally {
    if (adding.get(el) === signal) adding.delete(el);
  }
};

// Player reads are browser-to-TorrServer requests; their events contain media
// data only. Datastar posts those data to the cloud for rendering.
const playerLoads = new WeakMap();
const playerMedia = new WeakMap();
const hashOK = (hash) => /^[a-f0-9]{40}$/i.test(hash);
const playerProblem = (result) => ({ problem: result.problem || "", status: result.status || 0 });
const withFiles = (torrent) => {
  if (!torrent || typeof torrent !== "object") return null;
  if (!torrent.file_stats?.length && torrent.data) {
    try { torrent.file_stats = JSON.parse(torrent.data).TorrServer?.Files || []; } catch {}
  }
  return torrent;
};
const getTorrent = (url, hash, signal) => call(url, "torrents", {
  init: {method: "POST", headers: {"Content-Type":"application/json"}, body:JSON.stringify({action:"get",hash})},
  read: async (res) => {
    const torrent = withFiles(await res.json());
    if (!torrent || torrent.hash !== hash) throw new TypeError("Invalid torrent metadata");
    return torrent;
  }, signal,
});

window.tsPlayerLoad = async (el, url, hash, index, kind, nonce) => {
  if (!webAddress(url) || !hashOK(hash) || !Number.isSafeInteger(index) || index < 1 || !["direct","hls"].includes(kind)) return;
  playerLoads.get(el)?.abort();
  const ctl = new AbortController();
  playerLoads.set(el, ctl);
  await Promise.resolve();
  if (ctl.signal.aborted) return;
  const result = await getTorrent(url, hash, ctl.signal);
  if (ctl.signal.aborted) return;
  let probe = {value: null};
  if (!result.problem && kind === "hls") {
    probe = await call(url, `gst/${hash}/probe?index=${index}`, {signal:ctl.signal, read:res=>res.json(),timeout:30000});
  }
  if (ctl.signal.aborted) return;
  playerMedia.set(el,{url,hash,index,probe:probe.value || null});
  tell(el,"ts-player",{hash,index,kind,nonce,torrent:result.value || null,probe:probe.value || null,...playerProblem(result),probeProblem:probe.problem || ""});
};

const playerSessions = new WeakMap();
const playerLeases = new WeakMap();

// TorrServer shares one HLS task per hash across tabs and devices. Stop this
// viewer's heartbeat and let TorrServer expire an idle task; hash-wide removal
// would interrupt another viewer that still uses it.
window.tsPlayerRelease = (el) => {
  playerLeases.delete(el);
  const session = playerSessions.get(el);
  playerSessions.delete(el);
  session?.tick?.abort();
  return Promise.resolve();
};

window.tsPlayerCancel = (el) => {
  playerLoads.get(el)?.abort();
  return window.tsPlayerRelease(el);
};

window.tsPlayerLease = async (el, url, hash, index, kind, audio) => {
  if (!webAddress(url) || !hashOK(hash) || !Number.isSafeInteger(index) || index < 1 || !["direct","hls"].includes(kind) || !Number.isSafeInteger(audio) || audio < 0) return null;
  const key=JSON.stringify([url,hash,index,kind,audio]);
  const current=playerSessions.get(el);
  if (current?.key===key) return null;
  const cleanup=window.tsPlayerRelease(el);
  const lease={};
  playerLeases.set(el,lease);
  await cleanup;
  if (playerLeases.get(el)!==lease) return null;
  playerSessions.set(el,{key,url,hash,index,kind,audio});
  const direct=new URL(`stream?link=${hash}&index=${index}&play`,`${url}/`).href;
  const hls=new URL(`gst/${hash}/master.m3u8?index=${index}&audio=${audio}`,`${url}/`).href;
  return {direct,hls,file:`${hash}:${index}`,stream:kind==="hls"?hls:direct};
};

window.tsPlayerTick = async (el) => {
  const session=playerSessions.get(el);
  if (!session || session.tick) return;
  const ctl=new AbortController();
  session.tick=ctl;
  try {
    const torrent=await getTorrent(session.url,session.hash,ctl.signal);
    if (session.cacheSize === undefined && !ctl.signal.aborted) {
      const settings = await settingsGet(session.url,"streaming",ctl.signal);
      const capacity = settings.value?.CacheSize;
      session.cacheSize = Number.isSafeInteger(capacity) && capacity > 0 ? capacity : 0;
    }
    let heartbeat={};
    if (session.kind==="hls" && !ctl.signal.aborted) {
      heartbeat=await call(session.url,`gst/${session.hash}/heartbeat`,{signal:ctl.signal,read:res=>res.json()});
    }
    if (session.kind === "hls" && !session.master && !heartbeat.problem && !ctl.signal.aborted) {
      const master = await call(session.url,`gst/${session.hash}/master.m3u8?index=${session.index}&audio=${session.audio}`,{signal:ctl.signal,read:res=>res.text()});
      if (master.value?.includes("#EXT-X-STREAM-INF:") && master.value.length <= 65536) session.master = master.value;
    }
    if (ctl.signal.aborted || playerSessions.get(el)!==session) return;
    const media = playerMedia.get(el);
    const probe = media?.url === session.url && media.hash === session.hash && media.index === session.index ? media.probe : null;
    tell(el,"ts-player-stats",{hash:session.hash,index:session.index,kind:session.kind,audio:session.audio,probe,master:session.master || "",torrent:torrent.value || null,...playerProblem(torrent),heartbeatProblem:heartbeat.problem || "",cacheSize:session.cacheSize || 0});
  } finally { if (session.tick===ctl) session.tick=null; }
};

const playerFileLoads = new WeakMap();

window.tsPlayerFiles = async (el, url, hash) => {
  if (!webAddress(url) || !hashOK(hash)) return;
  playerFileLoads.get(el)?.abort();
  const ctl = new AbortController();
  playerFileLoads.set(el, ctl);
  const signal = ctl.signal;
  const result=await getTorrent(url,hash,signal);
  if (signal.aborted) return;
  tell(el,"ts-files",{url,hash,index:1,kind:"direct",torrent:result.value || null,...playerProblem(result)});
};

// Settings requests have their own lifetime, independent of list polling.
const settingsLoads = new WeakMap();
const settingsBegin = (el) => {
  settingsLoads.get(el)?.abort();
  const ctl = new AbortController();
  settingsLoads.set(el, ctl);
  return ctl.signal;
};
const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const settingsGet = async (url, section, signal) => {
  const gst = section === "gstreamer";
  const result = await call(url, gst ? "gst/settings" : "settings", {
    init: gst ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action: "get" }) },
    read: (res) => res.json(), signal,
  });
  if (result.problem) return result;
  if (gst && result.value?.built_in === false) return { problem: "no-gstreamer" };
  const value = gst ? result.value?.config : result.value;
  return object(value) ? { value } : { problem: "invalid" };
};

// Only the form's allowlisted fields leave the browser. Other settings may
// contain secrets, and are retained locally only for read/merge/write.
let settingsNonce = 0;
window.tsSettingsRead = async (el, url, section, keys, nonce = ++settingsNonce) => {
  const signal = settingsBegin(el);
  await Promise.resolve();
  if (!url || signal.aborted) return;
  tell(el, "ts-settings-loading", {url,section,nonce});
  const result = await settingsGet(url, section, signal);
  if (signal.aborted) return;
  const values = Object.fromEntries(keys.filter(key => Object.hasOwn(result.value || {}, key)).map(key => [key, result.value[key]]));
  tell(el, "ts-settings", { url, section, nonce, values, problem: result.problem || "", status: result.status || 0 });
};

// A validated patch is merged with a fresh object immediately before saving:
// TorrServer replaces the entire settings object, including unknown fields.
window.tsSettingsWrite = async (el, url, section, changes, nonce) => {
  const signal = settingsBegin(el);
  await Promise.resolve();
  if (!url || signal.aborted) return;
  const report = (fields) => tell(el, "ts-settings-saved", {url, nonce, busy:false, ok:false, problem:"", status:0, ...fields});
  report({busy:true});
  const current = await settingsGet(url, section, signal);
  if (signal.aborted) return;
  if (current.problem) { report({problem:current.problem,status:current.status || 0}); return; }
  // A version change can remove fields after the form was loaded. Do not
  // recreate unsupported settings in the newer object.
  if (Object.keys(changes).some(key => !Object.hasOwn(current.value,key))) { report({problem:"invalid"}); return; }
  const merged = {...current.value, ...changes};
  const gst = section === "gstreamer";
  const result = await call(url, gst ? "gst/settings" : "settings", {
    init: {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(gst ? {action:"set",config:merged} : {action:"set",sets:merged})}, signal,
  });
  if (signal.aborted) return;
  report({ok:!result.problem,problem:result.problem || "",status:result.status || 0});
};
window.tsSettingsCancel = (el) => settingsLoads.get(el)?.abort();

const probeLoads = new WeakMap();
window.tsProbe = async (el,url,hash,index) => {
  probeLoads.get(el)?.abort();
  const ctl = new AbortController(); probeLoads.set(el,ctl);
  await Promise.resolve();
  if (ctl.signal.aborted || !webAddress(url) || !hashOK(hash) || !Number.isSafeInteger(index) || index < 1) return;
  tell(el,"ts-probe-loading",{});
  const result = await call(url,`gst/${hash}/probe?index=${index}`,{signal:ctl.signal,read:res=>res.json(),timeout:30000});
  if (!ctl.signal.aborted) tell(el,"ts-probe",{url,hash,index,probe:result.value || null,...playerProblem(result)});
};
window.tsProbeCancel = el => probeLoads.get(el)?.abort();

// A download is a browser capability: construct the same M3U entries using
// direct TorrServer URLs rather than the local app's signed proxy URLs.
window.tsDownloadPlaylist = async (el,url,hash,kind) => {
  if (!webAddress(url) || !hashOK(hash) || !["direct","hls"].includes(kind)) return;
  const result = await getTorrent(url,hash);
  if (!result.value) return;
  const response = await fetch("/api/ts/playlist",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({url,kind,torrent:result.value})});
  if (!response.ok) return;
  const blob = await response.blob();
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a"); anchor.href = href; anchor.download = `${hash}-${kind}.m3u8`;
  anchor.click(); URL.revokeObjectURL(href);
};
