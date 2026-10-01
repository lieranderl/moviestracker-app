import { afterEach, beforeEach, expect, mock, test } from "bun:test";

const originalFetch = globalThis.fetch;
const originalWindow = globalThis.window;
const originalStorage = globalThis.localStorage;
globalThis.window = { location: { protocol: "https:" } };
await import("./torrserver.js");
const client = globalThis.window;
const server = "https://nas.example:8091";
const added = '{"hash":"08ada5a7a6183aae1e09d831df6748d566095a10"}';
let requests;
let storage;

beforeEach(() => {
  globalThis.window = client;
  client.location.protocol = "https:";
  storage = new Map();
  globalThis.localStorage = {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  };
  requests = mock(async () => new Response(added));
  globalThis.fetch = requests;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.window = originalWindow;
  globalThis.localStorage = originalStorage;
});

const form = () => {
  const el = new EventTarget();
  const events = [];
  el.addEventListener("ts-added", (evt) => events.push(evt.detail));
  return { el, events };
};

test("a user adds a magnet directly to their TorrServer with title and poster", async () => {
  const { el, events } = form();
  await client.tsAddTorrent(el, server, {
    link: " magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10 ",
    title: " Sintel ",
    poster: " https://image.tmdb.org/t/p/w500/sintel.jpg ",
  });
  expect(requests).toHaveBeenCalledTimes(1);
  const [target, init] = requests.mock.calls[0];
  expect(target.href).toBe("https://nas.example:8091/torrents");
  expect(init.method).toBe("POST");
  expect(init.headers["Content-Type"]).toBe("application/json");
  expect(JSON.parse(init.body)).toEqual({
    action: "add",
    link: "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
    title: "Sintel",
    poster: "https://image.tmdb.org/t/p/w500/sintel.jpg",
    save_to_db: true,
  });
  expect(events).toEqual([
    { adding: true, ok: false, problem: "", status: 0 },
    { adding: false, ok: true, problem: "", status: 0 },
  ]);
});

test("a user uploads a torrent file with metadata and their browser-only login", async () => {
  client.tsSaveLogin(server, "евгений", "пароль");
  const { el, events } = form();
  await client.tsAddTorrent(el, server, {
    file: new File(["d4:infod4:name6:Sintelee"], "Sintel.torrent"),
    title: "Sintel",
    poster: "https://image.tmdb.org/t/p/w500/sintel.jpg",
  });
  const [target, init] = requests.mock.calls[0];
  expect(target.href).toBe("https://nas.example:8091/torrent/upload");
  expect(init.headers.Authorization).toBe("Basic 0LXQstCz0LXQvdC40Lk60L/QsNGA0L7Qu9GM");
  expect(init.headers["Content-Type"]).toBeUndefined();
  expect(init.body).toBeInstanceOf(FormData);
  expect(init.body.get("file").name).toBe("Sintel.torrent");
  expect(await init.body.get("file").text()).toBe("d4:infod4:name6:Sintelee");
  expect(init.body.get("save")).toBe("true");
  expect(init.body.get("title")).toBe("Sintel");
  expect(init.body.get("poster")).toBe("https://image.tmdb.org/t/p/w500/sintel.jpg");
  expect(events.at(-1).ok).toBe(true);
});

test("invalid links and files are refused before contacting TorrServer", async () => {
  for (const input of [
    {},
    { link: "javascript:alert(1)" },
    { link: "ftp://example.com/movie.torrent" },
    { link: "magnet:?dn=Sintel" },
    { link: "https://user:password@example.com/movie.torrent" },
    { link: "https://example.com/movie.torrent", file: new File(["de"], "movie.torrent") },
    { file: new File(["not a torrent"], "movie.torrent") },
    { file: new File(["de"], "movie.txt") },
    { file: new File([], "movie.torrent") },
    { file: new File([new Uint8Array((4 << 20) + 1)], "movie.torrent") },
    { link: "https://example.com/movie.torrent", poster: "javascript:alert(1)" },
  ]) {
    const { el, events } = form();
    await client.tsAddTorrent(el, server, input);
    expect(events.at(-1)).toMatchObject({ adding: false, ok: false, problem: "input" });
  }
  expect(requests).not.toHaveBeenCalled();
});

test("switching TorrServers aborts an add and suppresses its late success", async () => {
  let finish;
  requests.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  const { el, events } = form();
  const pending = client.tsAddTorrent(el, server, { link: "https://example.com/movie.torrent" });
  await Promise.resolve();
  const requestSignal = requests.mock.calls[0][1].signal;
  await client.tsResetAdd(el, "https://other.example");
  expect(requestSignal.aborted).toBe(true);
  finish(new Response(added));
  await pending;
  expect(events.at(-1)).toEqual({ adding: false, ok: false, problem: "", status: 0 });
  expect(events.filter((event) => event.ok)).toHaveLength(0);
});

test("submitting twice while an add is pending sends only one request", async () => {
  let finish;
  requests.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  const { el, events } = form();
  const input = { link: "https://example.com/movie.torrent" };
  const pending = client.tsAddTorrent(el, server, input);
  await Promise.resolve();
  await client.tsAddTorrent(el, server, input);
  expect(requests).toHaveBeenCalledTimes(1);
  finish(new Response(added));
  await pending;
  expect(events.at(-1).ok).toBe(true);
});

test("TorrServer failures are reported without success and the form can retry", async () => {
  for (const [reply, problem, status] of [
    [() => new Response("Unauthorized", { status: 401 }), "login", 401],
    [() => new Response("failed", { status: 500 }), "status", 500],
    [() => { throw new TypeError("Failed to fetch"); }, "unreachable", 0],
    [() => { throw new DOMException("Timed out", "AbortError"); }, "timeout", 0],
  ]) {
    requests.mockImplementation(reply);
    const { el, events } = form();
    await client.tsAddTorrent(el, server, { link: "http://example.com/movie.torrent" });
    expect(events.at(-1)).toEqual({ adding: false, ok: false, problem, status });
    requests.mockImplementation(() => new Response(added));
    await client.tsAddTorrent(el, server, { link: "http://example.com/movie.torrent" });
    expect(events.at(-1).ok).toBe(true);
  }
});

test("an HTTPS page explains a remote HTTP TorrServer without sending a request", async () => {
  const { el, events } = form();
  await client.tsAddTorrent(el, "http://nas.example:8090", { link: "https://example.com/movie.torrent" });
  expect(requests).not.toHaveBeenCalled();
  expect(events.at(-1)).toMatchObject({ adding: false, ok: false, problem: "mixed" });
});

test("an HTTPS page can add to localhost with its saved login", async () => {
  client.tsSaveLogin("http://localhost:8090", "user", "pass");
  const { el, events } = form();
  await client.tsAddTorrent(el, "http://localhost:8090", { link: "https://example.com/movie.torrent" });
  expect(requests.mock.calls[0][0].href).toBe("http://localhost:8090/torrents");
  expect(requests.mock.calls[0][1].headers.Authorization).toBe("Basic dXNlcjpwYXNz");
  expect(events.at(-1).ok).toBe(true);
});

test("an upload rejected inside TorrServer is a failure even with HTTP 200", async () => {
  for (const body of ["null", "[]", "{}", "not JSON"]) {
    requests.mockImplementation(() => new Response(body));
    const { el, events } = form();
    await client.tsAddTorrent(el, server, { file: new File(["definitely invalid torrent"], "bad.torrent") });
    expect(events.at(-1)).toEqual({ adding: false, ok: false, problem: "status", status: 200 });
  }
});

test("a title page adds a release with its TMDB identity and category", async () => {
  const { el, events } = form();
  await client.tsAddTorrent(el, server, {
    link: "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
    title: "Dune (2021)", poster: "https://image.tmdb.org/t/p/w500/d.jpg",
    mediaId: 438631, mediaType: "movie",
  });
  const body = JSON.parse(requests.mock.calls[0][1].body);
  expect(body.category).toBe("movie");
  expect(JSON.parse(body.data)).toEqual({ tmdb: { id: 438631, type: "movie" } });
  expect(events.at(-1).ok).toBe(true);
});

test("a viewer loads files and HLS tracks using only their browser's TorrServer login", async () => {
  client.tsSaveLogin(server, "viewer", "browser-secret");
  const el = new EventTarget();
  const events = [];
  el.addEventListener("ts-player", (evt) => events.push(evt.detail));
  requests.mockImplementation(async (target) => new Response(JSON.stringify(target.pathname.endsWith('/probe')
    ? { Tracks: [{Index:0,Type:"audio",Language:"en"}] }
    : { hash: "08ada5a7a6183aae1e09d831df6748d566095a10", file_stats: [{id:1,path:"Sintel.mp4"}] })));
  await client.tsPlayerLoad(el, server, "08ada5a7a6183aae1e09d831df6748d566095a10", 1, "hls", 1);
  expect(requests.mock.calls.map(([target]) => target.href)).toEqual([
    "https://nas.example:8091/torrents",
    "https://nas.example:8091/gst/08ada5a7a6183aae1e09d831df6748d566095a10/probe?index=1",
  ]);
  expect(requests.mock.calls.every(([, init]) => init.headers.Authorization === `Basic ${btoa('viewer:browser-secret')}`)).toBe(true);
  expect(events[0].probe.Tracks[0].Language).toBe("en");
  expect(JSON.stringify(events)).not.toContain("browser-secret");
});

test("an HLS viewer sends heartbeats and stops them when closing", async () => {
 const el=new EventTarget();
 client.tsSaveLogin(server,"viewer","browser-secret");
 const links=await client.tsPlayerLease(el,server,"08ada5a7a6183aae1e09d831df6748d566095a10",1,"hls",0);
 expect(links.stream).toBe("https://nas.example:8091/gst/08ada5a7a6183aae1e09d831df6748d566095a10/master.m3u8?index=1&audio=0");
 await client.tsPlayerTick(el);
 await client.tsPlayerRelease(el);
 await client.tsPlayerRelease(el);
 expect(requests.mock.calls.map(([target])=>target.pathname)).toEqual(["/torrents","/settings","/gst/08ada5a7a6183aae1e09d831df6748d566095a10/heartbeat","/gst/08ada5a7a6183aae1e09d831df6748d566095a10/master.m3u8"]);
 await client.tsPlayerTick(el);
 expect(requests).toHaveBeenCalledTimes(4);
});

test("changing an HLS audio track does not remove another viewer's shared task", async () => {
  const el = new EventTarget();
  const hash = "08ada5a7a6183aae1e09d831df6748d566095a10";
  await client.tsPlayerLease(el, server, hash, 1, "hls", 0);
  const next = await client.tsPlayerLease(el, server, hash, 1, "hls", 1);
  expect(next.stream).toEndWith("index=1&audio=1");
  expect(requests).not.toHaveBeenCalled();
  await client.tsPlayerRelease(el);
});

test("closing during a slow probe prevents a late result from reopening playback",async()=>{
 const el=new EventTarget();const events=[];
 el.addEventListener('ts-player',evt=>events.push(evt.detail));
 let finish;
 requests.mockImplementation(async target=> target.pathname.endsWith('/probe') ? new Promise(resolve=>{finish=resolve}) : new Response(JSON.stringify({hash:'08ada5a7a6183aae1e09d831df6748d566095a10',file_stats:[]})));
 const loading=client.tsPlayerLoad(el,server,'08ada5a7a6183aae1e09d831df6748d566095a10',1,'hls',1);
 while(!finish) await new Promise(resolve=>setTimeout(resolve,1));
 await client.tsPlayerCancel(el);
 finish(new Response('{"Tracks":[]}'));
 await loading;
 expect(events).toEqual([]);
});

test("invalid playback metadata reports a failure instead of leaving the player loading", async () => {
  const hash = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111";
  for (const value of [null, [], {hash:"bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111"}]) {
    globalThis.fetch = async () => Response.json(value);
    const el = new EventTarget();
    const events = [];
    el.addEventListener("ts-player", evt => events.push(evt.detail));
    await client.tsPlayerLoad(el, server, hash, 1, "direct", 1);
    expect(events[0].problem).toBe("unreachable");
    expect(events[0].torrent).toBeNull();
  }
});

test("closing one viewer does not remove the HLS task used by another viewer", async () => {
  const first = new EventTarget();
  const second = new EventTarget();
  const hash = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111";
  await client.tsPlayerLease(first, server, hash, 1, "hls", 0);
  await client.tsPlayerLease(second, server, hash, 1, "hls", 0);
  await client.tsPlayerRelease(first);
  expect(requests).not.toHaveBeenCalled();
  requests.mockImplementation(async target => Response.json(target.pathname === "/torrents" ? {hash} : {}));
  await client.tsPlayerTick(second);
  expect(requests.mock.calls.map(([target]) => target.pathname)).toEqual(["/torrents", "/settings", `/gst/${hash}/heartbeat`, `/gst/${hash}/master.m3u8`]);
  await client.tsPlayerRelease(second);
});

test("routine list polling cannot cancel the viewer's file refresh", async () => {
  const el = new EventTarget();
  const hash = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111";
  const events = [];
  el.addEventListener("ts-files", evt => events.push(evt.detail));
  let finish;
  requests.mockImplementation(async (target, init) => JSON.parse(init.body).action === "get"
    ? new Promise(resolve => {finish = resolve}) : Response.json([]));
  const files = client.tsPlayerFiles(el, server, hash);
  await client.tsList(el, server, true);
  finish(Response.json({hash, file_stats:[{id:1,path:"Sintel.mp4"}]}));
  await files;
  expect(events).toHaveLength(1);
  expect(events[0].torrent.file_stats[0].path).toBe("Sintel.mp4");
});

test("settings read relays only editable fields and keeps the login in the browser", async () => {
 client.tsSaveLogin(server, "viewer", "secret");
 requests.mockImplementation(async () => Response.json({CacheSize:268435456, TMDBApiKey:"private", NewOption:7}));
 const el = new EventTarget();
 const events = [];
 el.addEventListener("ts-settings", evt => events.push(evt.detail));
 await client.tsSettingsRead(el, server, "streaming", ["CacheSize"], 1);
 expect(requests.mock.calls[0][0].href).toBe(server + "/settings");
 expect(JSON.parse(requests.mock.calls[0][1].body)).toEqual({action:"get"});
 expect(requests.mock.calls[0][1].headers.Authorization).toBe("Basic dmlld2VyOnNlY3JldA==");
 expect(events.at(-1)).toEqual({url:server, section:"streaming", nonce:1, values:{CacheSize:268435456}, problem:"", status:0});
});

test("saving merges validated changes into fresh settings preserving future fields", async () => {
 const el = new EventTarget(); const events = [];
 el.addEventListener("ts-settings-saved", evt => events.push(evt.detail));
 requests.mockImplementation(async (_target, init) => {
  if (JSON.parse(init.body).action === "get") return Response.json({CacheSize:67108864, NewOption:{enabled:true}, TMDBApiKey:"private"});
  return new Response("");
 });
 await client.tsSettingsWrite(el,server,"streaming",{CacheSize:268435456},3);
 expect(JSON.parse(requests.mock.calls[1][1].body)).toEqual({action:"set",sets:{CacheSize:268435456,NewOption:{enabled:true},TMDBApiKey:"private"}});
 expect(events.at(-1)).toEqual({url:server,nonce:3,busy:false,ok:true,problem:"",status:0});
});

test("GStreamer saves preserve new fields and report rejected credentials", async () => {
 const el = new EventTarget(); const saved = [];
 el.addEventListener("ts-settings-saved",evt => saved.push(evt.detail));
 requests.mockImplementation(async (_target,init) => init.method === "POST" ? new Response("",{status:401}) : Response.json({built_in:true,config:{MaxTasks:4,NewCodec:true}}));
 await client.tsSettingsWrite(el,server,"gstreamer",{MaxTasks:2},4);
 expect(JSON.parse(requests.mock.calls[1][1].body)).toEqual({action:"set",config:{MaxTasks:2,NewCodec:true}});
 expect(saved.at(-1).problem).toBe("login");
 expect(saved.at(-1).ok).toBe(false);
});

test("switching servers during a settings read suppresses the old reply", async () => {
 const el = new EventTarget(); const events = []; let oldReply;
 el.addEventListener("ts-settings",evt => events.push(evt.detail));
 requests.mockImplementationOnce(() => new Promise(resolve => {oldReply = resolve}));
 requests.mockImplementationOnce(async () => Response.json({CacheSize:134217728}));
 const old = client.tsSettingsRead(el,server,"streaming",["CacheSize"],5);
 await Promise.resolve();
 await client.tsSettingsRead(el,"https://other.example","streaming",["CacheSize"],6);
 oldReply(Response.json({CacheSize:67108864})); await old;
 expect(events).toHaveLength(1);
 expect(events[0].url).toBe("https://other.example");
});

test("unsupported GStreamer builds and malformed settings cannot trigger saves", async () => {
 for (const value of [null, [], {built_in:false}, {built_in:true,config:null}]) {
  const el = new EventTarget(); const saved = [];
  el.addEventListener("ts-settings-saved",evt => saved.push(evt.detail));
  requests.mockClear(); requests.mockImplementation(async () => Response.json(value));
  await client.tsSettingsWrite(el,server,"gstreamer",{MaxTasks:2},7);
  expect(requests).toHaveBeenCalledTimes(1);
  expect(saved.at(-1).ok).toBe(false);
 }
});

test("closing settings during the fresh read prevents a late write", async () => {
 const el = new EventTarget(); let reply;
 requests.mockImplementation(() => new Promise(resolve => {reply=resolve}));
 const save = client.tsSettingsWrite(el,server,"streaming",{CacheSize:134217728},10);
 await Promise.resolve();
 client.tsSettingsCancel(el);
 reply(Response.json({CacheSize:67108864})); await save;
 expect(requests).toHaveBeenCalledTimes(1);
});

test("shared player stats receive actual cache capacity without relaying settings secrets", async () => {
 const el = new EventTarget(); const events = []; const hash = "08ada5a7a6183aae1e09d831df6748d566095a10";
 el.addEventListener("ts-player-stats",evt=>events.push(evt.detail));
 requests.mockImplementation(async target => Response.json(target.pathname === "/torrents" ? {hash,preloaded_bytes:33554432} : target.pathname === "/settings" ? {CacheSize:67108864,TMDBApiKey:"private"} : {}));
 await client.tsPlayerLease(el,server,hash,1,"hls",0); await client.tsPlayerTick(el);
 expect(events[0].cacheSize).toBe(67108864);
 expect(JSON.stringify(events)).not.toContain("private");
 await client.tsPlayerRelease(el);
});

test("media comparison relays the selected audio and master playlist without login", async () => {
 const el = new EventTarget();
 const events=[]; el.addEventListener("ts-player-stats",evt=>events.push(evt.detail));
 const hash="08ada5a7a6183aae1e09d831df6748d566095a10";
 const probe={Tracks:[{Index:1,Type:"audio",Codec:"opus"}]};
 const master='#EXTM3U\n#EXT-X-STREAM-INF:CODECS="avc1.640028,mp4a.40.2"\nmain.m3u8\n';
 client.tsSaveLogin(server,"viewer","browser-secret");
 requests.mockImplementation(async target=> {
  if(target.pathname.endsWith("/probe")) return Response.json(probe);
  if(target.pathname.endsWith("master.m3u8")) return new Response(master);
  return Response.json(target.pathname === "/torrents" ? {hash} : {});
 });
 await client.tsPlayerLoad(el,server,hash,1,"hls",1);
 await client.tsPlayerLease(el,server,hash,1,"hls",1);
 await client.tsPlayerTick(el);
 expect(events[0].audio).toBe(1);
 expect(events[0].probe).toEqual(probe);
 expect(events[0].master).toBe(master);
 expect(JSON.stringify(events)).not.toContain("browser-secret");
 expect(requests.mock.calls.at(-1)[0].search).toBe("?index=1&audio=1");
 await client.tsPlayerCancel(el);
});

test("a failed settings read is retried on the next tick instead of reporting no cache", async () => {
 const el = new EventTarget(); const events = []; const hash = "08ada5a7a6183aae1e09d831df6748d566095a10";
 el.addEventListener("ts-player-stats",evt=>events.push(evt.detail));
 let settingsReads = 0;
 requests.mockImplementation(async target => {
  if (target.pathname === "/settings") return ++settingsReads === 1 ? new Response("", {status:503}) : Response.json({CacheSize:67108864});
  return Response.json(target.pathname === "/torrents" ? {hash} : {});
 });
 await client.tsPlayerLease(el,server,hash,1,"direct",0);
 await client.tsPlayerTick(el); await client.tsPlayerTick(el); await client.tsPlayerTick(el);
 expect(events.map(e => e.cacheSize)).toEqual([0, 67108864, 67108864]);
 expect(settingsReads).toBe(2);
 await client.tsPlayerRelease(el);
});
