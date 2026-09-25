package handlers_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

const duneHash = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"

// fakeEngine is a TorrServer with one torrent, recording what it was asked for.
type fakeEngine struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
	gst  bool
}

func newFakeEngine(t *testing.T, gst bool) *fakeEngine {
	t.Helper()
	e := &fakeEngine{gst: gst}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.seen = append(e.seen, r.URL.RequestURI())
		e.mu.Unlock()
		switch {
		case r.URL.Path == "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case r.URL.Path == "/gst/echo" && e.gst:
			_, _ = w.Write([]byte(`{"gstreamer":{"available":true,"works":true,"version":"1.28.7"}}`))
		case r.URL.Path == "/torrents":
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Dune","stat":3,` +
				`"file_stats":[{"id":1,"path":"Dune (2021)/Dune.2021.mkv","length":1000}]}]`))
		case strings.HasPrefix(r.URL.Path, "/stream/"):
			_, _ = w.Write([]byte("video bytes"))
		case r.URL.Path == "/gst/"+duneHash+"/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="English",URI="/gst/` + duneHash + `/subs/0.m3u8"` + "\n" +
				`#EXT-X-STREAM-INF:BANDWIDTH=1000,CODECS="avc1.640028,mp4a.40.2"` + "\n" +
				"/gst/" + duneHash + "/video.m3u8?audio=0\n"))
		case r.URL.Path == "/gst/"+duneHash+"/video.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6,\n/gst/" + duneHash + "/seg/0.m4s?audio=0\n"))
		case r.URL.Path == "/gst/"+duneHash+"/seg/0.m4s":
			_, _ = w.Write([]byte("segment bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(e.Close)
	return e
}

func (e *fakeEngine) asked(prefix string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, uri := range e.seen {
		if strings.HasPrefix(uri, prefix) {
			out = append(out, uri)
		}
	}
	return out
}

func withEngineAt(url string) localOption {
	return func(c *handlers.Config) { c.TorrServer = torrserver.NewManager(url) }
}

var shareLink = regexp.MustCompile(`/s/[0-9a-f]{40}\.\d+\.\d+\.[A-Za-z0-9_-]+/[^"'\s<]*`)

func TestTheTorrServerPageGivesSignedLinksAndNeverTheEngineAddress(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), l.admin(t)).Body.String()
	if strings.Contains(page, engine.URL) || strings.Contains(page, strings.TrimPrefix(engine.URL, "http://")) {
		t.Fatalf("the TorrServer page contains the engine address %s", engine.URL)
	}
	link := shareLink.FindString(page)
	if link == "" {
		t.Fatalf("no signed /s/ link on the page")
	}

	// VLC or a TV opens it without any Moviestracker session.
	rr := l.do(t, httptest.NewRequest(http.MethodGet, link, nil), nil)
	if rr.Code != http.StatusOK || rr.Body.String() != "video bytes" {
		t.Fatalf("GET %s = %d %q, want the stream", link, rr.Code, rr.Body.String())
	}
	asked := engine.asked("/stream/")
	if len(asked) != 1 || !strings.Contains(asked[0], "link="+duneHash) || !strings.Contains(asked[0], "index=1") || !strings.Contains(asked[0], "play") {
		t.Errorf("engine was asked %q, want a play request for file 1 of %s", asked, duneHash)
	}
}

func TestAChangedLinkReachesNothing(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	link := shareLink.FindString(l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), l.admin(t)).Body.String())
	if link == "" {
		t.Fatal("no signed link on the page")
	}
	parts := strings.SplitN(strings.TrimPrefix(link, "/s/"), "/", 2)
	token := strings.Split(parts[0], ".")
	for name, bad := range map[string]string{
		"other file":   "/s/" + strings.Join([]string{token[0], "2", token[2], token[3]}, ".") + "/x.mkv",
		"bad mac":      "/s/" + strings.Join([]string{token[0], token[1], token[2], "AAAA"}, ".") + "/x.mkv",
		"engine paths": "/s/" + parts[0] + "/../../settings",
		"no token":     "/s/",
	} {
		rr := l.do(t, httptest.NewRequest(http.MethodGet, bad, nil), nil)
		if rr.Code == http.StatusOK {
			t.Errorf("%s: GET %s = 200, want refused", name, bad)
		}
	}
	if asked := engine.asked("/stream/"); len(asked) != 0 {
		t.Errorf("changed links reached the engine: %q", asked)
	}
	if asked := engine.asked("/settings"); len(asked) != 0 {
		t.Errorf("a link reached the engine's settings: %q", asked)
	}
}

func TestPlaylistsListOnlySignedLinksOfTheChosenKind(t *testing.T) {
	for _, tc := range []struct {
		gst        bool
		kind, want string // want: how the one entry ends
	}{
		{false, "", "/Dune.2021.mkv"},
		{true, "", "/Dune.2021.mkv"}, // Direct unless HLS is asked for
		{true, "direct", "/Dune.2021.mkv"},
		{true, "hls", "/hls/master.m3u8"},
	} {
		engine := newFakeEngine(t, tc.gst)
		l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
		req := httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash+"&kind="+tc.kind, nil)
		req.Host = "192.168.1.20:8095"
		rr := l.do(t, req, l.admin(t))
		body := rr.Body.String()
		name := "Dune.m3u8"
		if tc.kind == "hls" {
			name = "Dune (HLS).m3u8"
		}
		if !strings.Contains(rr.Header().Get("Content-Disposition"), `attachment; filename="`+name+`"`) {
			t.Errorf("gst=%t kind=%q: playlist is not a %s download: %q", tc.gst, tc.kind, name, rr.Header().Get("Content-Disposition"))
		}
		if strings.Contains(body, engine.URL) {
			t.Errorf("gst=%t kind=%q: playlist contains the engine address:\n%s", tc.gst, tc.kind, body)
		}
		var entries []string
		for line := range strings.Lines(body) {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				entries = append(entries, line)
			}
		}
		if len(entries) != 1 || !strings.HasPrefix(entries[0], "http://192.168.1.20:8095/s/") || !strings.HasSuffix(entries[0], tc.want) {
			t.Errorf("gst=%t kind=%q: entries %q, want one signed link ending in %s", tc.gst, tc.kind, entries, tc.want)
		}
	}
}

func TestAnHLSPlaylistNeedsGStreamer(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash+"&kind=hls", nil), l.admin(t))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "GStreamer") {
		t.Errorf("HLS playlist without GStreamer = %d %q, want 409 naming GStreamer", rr.Code, rr.Body.String())
	}
}

func TestASignedLinkPlaysGStreamerHLSEndToEnd(t *testing.T) {
	engine := newFakeEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash+"&kind=hls", nil)
	req.Host = "tv.localhost"
	master := shareLink.FindString(l.do(t, req, l.admin(t)).Body.String())
	if master == "" {
		t.Fatal("no signed HLS link in the playlist")
	}
	prefix := master[:strings.Index(master, "/hls/")]

	body := l.do(t, httptest.NewRequest(http.MethodGet, master, nil), nil).Body.String()
	if strings.Contains(body, "/gst/") || !strings.Contains(body, prefix+"/hls/video.m3u8?audio=0") || !strings.Contains(body, `URI="`+prefix+`/hls/subs/0.m3u8"`) {
		t.Fatalf("master playlist not rewritten to signed paths:\n%s", body)
	}
	variant := l.do(t, httptest.NewRequest(http.MethodGet, prefix+"/hls/video.m3u8?audio=0", nil), nil).Body.String()
	if !strings.Contains(variant, prefix+"/hls/seg/0.m4s?audio=0") {
		t.Fatalf("variant playlist not rewritten:\n%s", variant)
	}
	seg := l.do(t, httptest.NewRequest(http.MethodGet, prefix+"/hls/seg/0.m4s?audio=0", nil), nil)
	if seg.Code != http.StatusOK || seg.Body.String() != "segment bytes" {
		t.Errorf("segment through the signed link = %d %q", seg.Code, seg.Body.String())
	}
	if got := engine.asked("/gst/" + duneHash + "/master.m3u8"); len(got) != 1 || !strings.Contains(got[0], "index=1") {
		t.Errorf("engine master requests %q, want file 1", got)
	}
}

func TestAnHLSLinkOnlyEverReachesTheFileItGrants(t *testing.T) {
	engine := newFakeEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash+"&kind=hls", nil)
	req.Host = "tv.localhost"
	master := shareLink.FindString(l.do(t, req, l.admin(t)).Body.String())
	if master == "" {
		t.Fatal("no signed HLS link in the playlist")
	}
	prefix := master[:strings.Index(master, "/hls/")]

	for _, path := range []string{"/hls/video.m3u8?audio=0&index=7", "/hls/seg/0.m4s?audio=0&index=7&index=8", "/hls/init.mp4?index=7"} {
		l.do(t, httptest.NewRequest(http.MethodGet, prefix+path, nil), nil)
	}
	asked := engine.asked("/gst/" + duneHash + "/")
	if len(asked) != 3 {
		t.Fatalf("engine was asked %q, want the 3 requests", asked)
	}
	for _, uri := range asked {
		if q, _ := url.ParseQuery(uri[strings.Index(uri, "?")+1:]); len(q["index"]) != 1 || q.Get("index") != "1" {
			t.Errorf("engine was asked %q, want index=1 only: the link grants file 1", uri)
		}
	}
}

func TestAPlayerThatHangsUpIsNotReportedAsAnEngineFailure(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the player closed before the proxy reached TorrServer
	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/stream/x.mkv?link="+duneHash+"&index=1&play", nil).WithContext(ctx), l.admin(t))
	if rr.Code == http.StatusBadGateway {
		t.Errorf("a cancelled request was answered 502 %q, as if TorrServer had failed", rr.Body.String())
	}
}

// withLAN makes this machine's network address ip.
func withLAN(ip string) localOption {
	return func(c *handlers.Config) { c.LANAddress = func() string { return ip } }
}

func TestLinksCopiedOnThisMachineUseItsNetworkAddressNotLocalhost(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withLAN("192.168.0.170"))
	cookie := l.admin(t)
	for host, want := range map[string]string{
		"localhost:8095":     "http://192.168.0.170:8095/s/",
		"127.0.0.1:8095":     "http://192.168.0.170:8095/s/",
		"[::1]:8095":         "http://192.168.0.170:8095/s/",
		"192.168.0.170:8095": "http://192.168.0.170:8095/s/",
		"media.local:8095":   "http://media.local:8095/s/", // a name devices already reach
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash, nil)
		req.Host = host
		rr := l.do(t, req, cookie)
		if body := rr.Body.String(); !strings.Contains(body, want) {
			t.Errorf("opened as %s: playlist links do not start with %s:\n%s", host, want, body)
		}
		// Its links are signed for a while: never a cached copy with old ones.
		if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("playlist Cache-Control = %q, want no-store", cc)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/torrserver", nil)
	req.Host = "localhost:8095"
	if page := html.UnescapeString(l.do(t, req, cookie).Body.String()); !strings.Contains(page, `linkOrigin: "http://192.168.0.170:8095"`) {
		t.Error("the TorrServer page does not give copy buttons and players the network address")
	}
}

func TestWithoutANetworkLinksKeepTheAddressInUse(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withLAN(""))
	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/playlist?hash="+duneHash, nil)
	req.Host = "localhost:8095"
	if body := l.do(t, req, l.admin(t)).Body.String(); !strings.Contains(body, "http://localhost:8095/s/") {
		t.Errorf("with no network, links should stay on localhost:\n%s", body)
	}
}
