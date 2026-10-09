package gateway_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/streams"
)

// fakeTorrServer answers like a TorrServer started with --httpauth for the
// user "moviestracker", and remembers what it was asked.
type fakeTorrServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string // "METHOD /path body"
	saved    map[string]bool
}

func newFakeTorrServer(t *testing.T) *fakeTorrServer {
	t.Helper()
	f := &fakeTorrServer{saved: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		f.mu.Unlock()
		// TorrServer lets pages from any address read its answers.
		if r.Header.Get("Origin") != "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "moviestracker" || pass != "engine-secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Authorization Required"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := r.URL.Path
		if strings.HasPrefix(path, "/stream/") || strings.HasPrefix(path, "/play/") {
			path = "/stream"
		}
		switch path {
		case "/echo":
			_, _ = io.WriteString(w, "MatriX.145")
		case "/torrents":
			var req struct{ Action, Hash string }
			_ = json.Unmarshal(body, &req)
			f.mu.Lock()
			saved := f.saved[req.Hash]
			f.mu.Unlock()
			if req.Action == "get" && !saved {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, "ok")
		case "/stream":
			if r.Header.Get("Range") == "" {
				_, _ = io.WriteString(w, "ok")
				return
			}
			http.ServeContent(w, r, "movie.mkv", time.Time{}, bytes.NewReader(make([]byte, 1000)))
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTorrServer) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

type setup struct {
	engine  *fakeTorrServer
	store   *config.Store
	handler http.Handler
	plays   *streams.Tracker
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := newFakeTorrServer(t)
	plays := streams.New(time.Minute)
	h := gateway.New(gateway.Config{
		Store: store,
		Plays: plays,
		Upstream: func() gateway.Upstream {
			return gateway.Upstream{URL: engine.URL, User: "moviestracker", Password: "engine-secret"}
		},
	})
	return &setup{engine: engine, store: store, handler: h, plays: plays}
}

// addLogin creates a login for an app, as an admin would, and returns it.
func (s *setup) addLogin(t *testing.T, name string) (user, password string) {
	t.Helper()
	login, password, err := gateway.NewLogin(name, s.store.State().Gateway.Logins)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Update(func(st *config.State) error {
		st.Gateway.Logins = append(st.Gateway.Logins, login)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return login.User, password
}

// ask sends a request to the gateway from a device at remote (host:port).
func (s *setup) ask(t *testing.T, method, target, body, user, password, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = remote
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

const tv = "192.168.1.40:51000"

func TestAnAppReachesTorrServerWithItsOwnLogin(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Living room TV")

	rec := s.ask(t, http.MethodGet, "/echo", "", user, password, tv)
	if rec.Code != http.StatusOK || rec.Body.String() != "MatriX.145" {
		t.Fatalf("GET /echo = %d %q, want TorrServer's answer", rec.Code, rec.Body.String())
	}
	if got := s.engine.asked(); len(got) != 1 || got[0] != "GET /echo " {
		t.Errorf("TorrServer was asked %q", got)
	}
}

func TestTorrServerRefusesAppsWithoutAValidLogin(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")
	if err := s.store.Update(func(st *config.State) error { st.Gateway.Logins = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	other, otherPassword := s.addLogin(t, "Phone")

	for _, tc := range []struct{ name, user, password string }{
		{"no login", "", ""},
		{"a wrong password", other, "wrong-password"},
		{"a login that was revoked", user, password},
		{"TorrServer's own login", "moviestracker", "engine-secret"},
	} {
		rec := s.ask(t, http.MethodPost, "/torrents", `{"action":"list"}`, tc.user, tc.password, tv)
		if rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Basic") {
			t.Errorf("with %s: %d (WWW-Authenticate %q), want 401 asking for a login", tc.name, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	if got := s.engine.asked(); len(got) != 0 {
		t.Errorf("TorrServer was asked %q", got)
	}
	if rec := s.ask(t, http.MethodPost, "/torrents", `{"action":"list"}`, other, otherPassword, tv); rec.Code != http.StatusOK {
		t.Errorf("the other app's login no longer works: %d", rec.Code)
	}
}

func TestAppsPlayAddAndRemoveButCannotStopOrReconfigureTorrServer(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Living room TV")

	allowed := []struct{ method, target, body string }{
		{http.MethodPost, "/torrents", `{"action":"list"}`},
		{http.MethodPost, "/torrents", `{"action":"add","link":"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"}`},
		{http.MethodPost, "/torrents", `{"action":"rem","hash":"0123456789abcdef0123456789abcdef01234567"}`},
		{http.MethodPost, "/torrent/upload", "file"},
		{http.MethodPost, "/viewed", `{"action":"list"}`},
		{http.MethodPost, "/cache", `{"action":"get","hash":"0123456789abcdef0123456789abcdef01234567"}`},
		{http.MethodPost, "/settings", `{"action":"get"}`},
		{http.MethodGet, "/gst/settings", ""},
		{http.MethodGet, "/playlistall/all.m3u", ""},
		{http.MethodGet, "/ssl/status", ""},
		{http.MethodGet, "/ssl/cert", ""},
	}
	for _, tc := range allowed {
		if rec := s.ask(t, tc.method, tc.target, tc.body, user, password, tv); rec.Code != http.StatusOK {
			t.Errorf("%s %s %s = %d, want it passed to TorrServer", tc.method, tc.target, tc.body, rec.Code)
		}
	}
	refused := []struct{ method, target, body string }{
		{http.MethodGet, "/shutdown", ""},
		{http.MethodGet, "/shutdown/because", ""},
		{http.MethodPost, "/settings", `{"action":"set","sets":{"CacheSize":1}}`},
		{http.MethodPost, "/settings", `{"action":"def"}`},
		{http.MethodPost, "/settings", `{"action": "SET"}`},
		{http.MethodPost, "/torrents", `{"action":"wipe"}`},
		{http.MethodPost, "/storage/settings", `{}`},
		{http.MethodPost, "/gst/settings", `{}`},
		{http.MethodPost, "/waf", `{}`},
		{http.MethodPost, "/torznab/test", `{}`},
		// MatriX.146's certificate API: the certificate is Moviestracker's too.
		{http.MethodPost, "/ssl/upload", "cert"},
		{http.MethodPost, "/ssl/paths", `{"cert":"/etc/ssl/a.pem","key":"/etc/ssl/a.key"}`},
		{http.MethodPost, "/ssl/selfsigned", ""},
		{http.MethodPost, "/ssl/regenerate", ""},
		{http.MethodDelete, "/ssl/anything", ""},
		{http.MethodPost, "/torrents", `{"action":"list","pad":"` + strings.Repeat("x", 1<<20) + `"}`},
	}
	before := len(s.engine.asked())
	for _, tc := range refused {
		if rec := s.ask(t, tc.method, tc.target, tc.body, user, password, tv); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s %.60s = %d, want 403", tc.method, tc.target, tc.body, rec.Code)
		}
	}
	if got := s.engine.asked()[before:]; len(got) != 0 {
		t.Errorf("TorrServer was asked %q", got)
	}
	// What TorrServer receives is the body the app sent.
	s.ask(t, http.MethodPost, "/settings", `{"action":"get"}`, user, password, tv)
	if got := s.engine.asked(); got[len(got)-1] != `POST /settings {"action":"get"}` {
		t.Errorf("TorrServer got %q", got[len(got)-1])
	}
}

const savedHash = "0123456789abcdef0123456789abcdef01234567"

// Video players get stream links from the app and cannot sign in, so
// TorrServer serves streams without a login: the gateway does too, for the
// torrents already saved, and never adds one for a player without a login.
func TestPlayersStreamSavedTorrentsWithoutALogin(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	user, password := s.addLogin(t, "Living room TV")

	for _, target := range []string{
		"/stream/Movie.mkv?link=" + savedHash + "&index=1&play",
		"/stream?link=" + strings.ToUpper(savedHash) + "&index=1&play",
		"/stream?link=magnet:?xt=urn:btih:" + savedHash + "&index=1&play",
		"/play/" + savedHash + "/1",
		"/playlist?hash=" + savedHash,
		"/playlist/Movie.m3u?hash=" + savedHash,
	} {
		if rec := s.ask(t, http.MethodGet, target, "", "", "", tv); rec.Code != http.StatusOK {
			t.Errorf("a player without a login: GET %s = %d, want the stream", target, rec.Code)
		}
	}
	const unknown = "fedcba9876543210fedcba9876543210fedcba98"
	for _, target := range []string{
		"/stream?link=" + unknown + "&index=1&play",
		"/stream?link=magnet:?xt=urn:btih:" + unknown + "&save",
		"/stream?link=https://tracker.example/file.torrent&play",
		"/play/" + unknown + "/1",
		"/playlist?hash=" + unknown,
		"/stream?link=" + savedHash + "&link=" + unknown,
	} {
		if rec := s.ask(t, http.MethodGet, target, "", "", "", tv); rec.Code != http.StatusUnauthorized {
			t.Errorf("a player without a login: GET %s = %d, want 401", target, rec.Code)
		}
		if rec := s.ask(t, http.MethodGet, target, "", user, password, tv); rec.Code != http.StatusOK {
			t.Errorf("an app with its login: GET %s = %d, want it passed to TorrServer", target, rec.Code)
		}
	}
}

func TestStreamsSeekThroughTheGateway(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	req := httptest.NewRequest(http.MethodGet, "/stream?link="+savedHash+"&index=1&play", nil)
	req.RemoteAddr = tv
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 100 {
		t.Errorf("a range request = %d with %d bytes, want 206 with 100", rec.Code, rec.Body.Len())
	}
}

func TestOnlyTheHomeNetworkGetsInUnlessTheInternetIsAllowed(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")
	home := []string{"192.168.1.40:5000", "10.0.0.7:5000", "172.20.1.2:5000", "127.0.0.1:5000", "[::1]:5000",
		"[fd12:3456::7]:5000", "[fe80::1]:5000", "169.254.10.1:5000", "100.101.102.103:5000"} // …, Tailscale
	internet := []string{"203.0.113.9:5000", "[2001:db8::9]:5000", "8.8.8.8:5000"}
	for _, remote := range home {
		if rec := s.ask(t, http.MethodGet, "/echo", "", user, password, remote); rec.Code != http.StatusOK {
			t.Errorf("from %s at home: %d", remote, rec.Code)
		}
	}
	for _, remote := range internet {
		if rec := s.ask(t, http.MethodGet, "/echo", "", user, password, remote); rec.Code != http.StatusForbidden {
			t.Errorf("from %s on the internet: %d, want 403", remote, rec.Code)
		}
	}

	if err := s.store.Update(func(st *config.State) error { st.Gateway.Internet = true; return nil }); err != nil {
		t.Fatal(err)
	}
	// Logins never cross the internet unencrypted: HTTPS only from there.
	for _, remote := range internet {
		if rec := s.ask(t, http.MethodGet, "/echo", "", user, password, remote); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "HTTPS") {
			t.Errorf("from %s over plain HTTP with the internet allowed: %d %q, want 403 asking for HTTPS", remote, rec.Code, rec.Body.String())
		}
		if rec := s.ask(t, http.MethodGet, "https://nas.example:8091/echo", "", user, password, remote); rec.Code != http.StatusOK {
			t.Errorf("from %s over HTTPS with the internet allowed: %d", remote, rec.Code)
		}
	}
}

func TestGuessingLoginsIsCutShort(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")
	const guesser = "203.0.113.9:5000"
	if err := s.store.Update(func(st *config.State) error { st.Gateway.Internet = true; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		if rec := s.ask(t, http.MethodGet, "https://nas.example:8091/echo", "", user, fmt.Sprintf("guess-%d", i), guesser); rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d: %d, want 401", i, rec.Code)
		}
	}
	if rec := s.ask(t, http.MethodGet, "https://nas.example:8091/echo", "", user, password, guesser); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after 10 wrong logins the right one = %d, want 429 until the minute is over", rec.Code)
	}
	if rec := s.ask(t, http.MethodGet, "/echo", "", user, password, tv); rec.Code != http.StatusOK {
		t.Errorf("another device is locked out too: %d", rec.Code)
	}
}

// With "a TorrServer I already run" at the default 127.0.0.1:8090 and
// nothing there, the gateway on port 8090 would be its own upstream.
func TestTheGatewayNeverPassesRequestsToItself(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var self string
	g := gateway.New(gateway.Config{Store: store, Upstream: func() gateway.Upstream {
		return gateway.Upstream{URL: self, User: "moviestracker", Password: "engine-secret"}
	}})
	front := httptest.NewServer(g)
	t.Cleanup(front.Close)
	self = front.URL
	login, password, err := gateway.NewLogin("TV", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(st *config.State) error { st.Gateway.Logins = []config.AppLogin{login}; return nil })

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/echo", nil)
	req.SetBasicAuth(login.User, password)
	done := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			done <- 0
			return
		}
		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()
	select {
	case code := <-done:
		if code != http.StatusLoopDetected {
			t.Errorf("a gateway in front of itself answered %d, want 508", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway keeps passing the request to itself")
	}
}
