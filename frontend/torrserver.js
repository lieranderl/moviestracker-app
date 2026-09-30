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

const headers = (url) => {
  const login = readLogin(url);
  return login ? { Authorization: `Basic ${btoa(`${login.user}:${login.pass}`)}` } : {};
};

// call is a request to the TorrServer at url, answered with its response or
// a problem the page explains: "mixed" (an http address on an https page, not
// this computer), "unreachable" (not running, blocked, or its certificate not
// trusted), "login" (it wants a login), "timeout", or "status".
const call = async (url, path, init = {}) => {
  const target = new URL(path, `${url}/`);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname);
  if (window.location.protocol === "https:" && target.protocol === "http:" && !local) {
    return { problem: "mixed" };
  }
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  try {
    const res = await fetch(target, { ...init, headers: { ...headers(url), ...init.headers }, signal: ctl.signal });
    if (res.status === 401) return { problem: "login" };
    if (!res.ok) return { problem: "status", status: res.status };
    return { res };
  } catch (err) {
    return { problem: err.name === "AbortError" ? "timeout" : "unreachable" };
  } finally {
    clearTimeout(timer);
  }
};

const tell = (el, name, detail) => el?.dispatchEvent(new CustomEvent(name, { detail }));

// status is a full ts-status detail: Datastar merges an object assigned to a
// signal, so every field is always sent.
const status = (fields) => ({ checking: false, ok: false, version: "", hls: false, problem: "", status: 0, ...fields });

// canConvert says whether a -gst build's GStreamer works: its /gst/echo
// answers 200 with a health report ({"gstreamer": {"works": true}, …});
// other builds have no /gst/echo.
const canConvert = async (url) => {
  const gst = await call(url, "gst/echo");
  if (gst.problem) return false;
  try {
    return (await gst.res.json())?.gstreamer?.works === true;
  } catch {
    return false;
  }
};

// tsCheck asks the TorrServer at url for its version and whether it can
// convert to HLS, then fires ts-status on el: { ok, version, hls } or
// { ok: false, problem, status }.
window.tsCheck = async (el, url) => {
  if (!url) return;
  tell(el, "ts-status", status({ checking: true }));
  const echo = await call(url, "echo");
  if (echo.problem) {
    tell(el, "ts-status", status({ problem: echo.problem, status: echo.status || 0 }));
    return;
  }
  const version = (await echo.res.text()).trim();
  tell(el, "ts-status", status({ ok: true, version, hls: await canConvert(url) }));
};
