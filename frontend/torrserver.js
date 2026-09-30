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
const call = async (url, path, { init = {}, read = (res) => res.text(), signal } = {}) => {
  const target = new URL(path, `${url}/`);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname);
  if (window.location.protocol === "https:" && target.protocol === "http:" && !local) {
    return { problem: "mixed" };
  }
  const ctl = new AbortController();
  const abort = () => ctl.abort();
  signal?.addEventListener("abort", abort);
  const timer = setTimeout(abort, timeoutMs);
  try {
    const res = await fetch(target, { ...init, headers: { ...headers(url), ...init.headers }, signal: ctl.signal });
    if (res.status === 401) return { problem: "login" };
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
    read: (res) => res.json(),
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
