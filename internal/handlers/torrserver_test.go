package handlers_test

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func sessionCookie(sessionID string) *http.Cookie {
	return &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

func setupTestServerWithTorrServer(t *testing.T, torrHandler http.Handler) (*handlers.Server, *auth.SessionManager, *httptest.Server) {
	t.Helper()
	var ts *httptest.Server
	if torrHandler != nil {
		ts = httptest.NewServer(torrHandler)
	}

	sessions := auth.NewSessionManager(1024, rand.Reader)
	var torrURL string
	if ts != nil {
		torrURL = ts.URL
	} else {
		torrURL = "http://localhost:8090"
	}
	torrMgr := torrserver.NewManager(torrURL)
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("config.Open(): %v", err)
	}
	addAccount(t, store, "admin", config.RoleAdmin)
	accounts := auth.NewAccounts(store)

	cfg := handlers.Config{
		Sessions:          sessions,
		Accounts:          accounts,
		Store:             store,
		SecureCookies:     false,
		MaxSSEStreams:     10,
		LoginAttempts:     5,
		LoginWindow:       time.Minute,
		LoginClientKeys:   100,
		TrustedProxyCIDRs: nil,
		CatalogTimeout:    2 * time.Second,
		TorrServer:        torrMgr,
	}

	srv, err := handlers.NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return srv, sessions, ts
}

func TestTorrServerPage_AuthProtection(t *testing.T) {
	srv, _, _ := setupTestServerWithTorrServer(t, nil)

	// Unauthenticated request should redirect to /login
	req := httptest.NewRequest(http.MethodGet, "/torrserver", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected redirect to /login, got %q", loc)
	}
}

func TestTorrServerPage_Authenticated(t *testing.T) {
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/gst/echo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"gstreamer":{"available":true,"works":true,"version":"1.28.7"}}`))
		case "/torrents":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"hash":"testhash1","title":"Test Movie","stat_string":"Torrent in db","torrent_size":1048576}]`))
		default:
			http.NotFound(w, r)
		}
	})

	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()

	// Create valid user session
	sessionID, err := sessions.CreateSession(&auth.User{
		Username: "test",
		Name:     "Test User",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/torrserver", nil)
	req.AddCookie(sessionCookie(sessionID))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "TorrServer") {
		t.Errorf("expected page to contain TorrServer, got %s", body)
	}
	if !strings.Contains(body, "Test Movie") {
		t.Errorf("expected page to contain Test Movie, got %s", body)
	}
}

func TestTorrServerAPI_StreamProxy(t *testing.T) {
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "master.m3u8") {
			rawPlaylist := "#EXTM3U\n/gst/testhash/video.m3u8\n"
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Content-Length", strconv.Itoa(len(rawPlaylist)))
			_, _ = w.Write([]byte(rawPlaylist))
			return
		}
		http.NotFound(w, r)
	})

	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()

	sessionID, _ := sessions.CreateSession(&auth.User{
		Username: "test",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/gst/testhash/master.m3u8?index=1", nil)
	req.AddCookie(sessionCookie(sessionID))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	body := rr.Body.String()
	// Check that /gst/ links in playlist were rewritten to proxy path
	if !strings.Contains(body, "/api/torrserver/stream/gst/testhash/video.m3u8") {
		t.Errorf("expected playlist to rewrite /gst/ to proxy path, got %s", body)
	}
	// Check Content-Length matches rewritten body length, avoiding ERR_CONTENT_LENGTH_MISMATCH
	expectedLen := strconv.Itoa(len(body))
	if cl := rr.Header().Get("Content-Length"); cl != expectedLen {
		t.Errorf("expected Content-Length %s, got %s", expectedLen, cl)
	}

	// Also verify over a real TCP HTTP connection that http.Client reads to completion with no Content-Length error
	realServer := httptest.NewServer(srv)
	defer realServer.Close()

	clientReq, err := http.NewRequest(http.MethodGet, realServer.URL+"/api/torrserver/stream/gst/testhash/master.m3u8?index=1", nil)
	if err != nil {
		t.Fatalf("failed to create client request: %v", err)
	}
	clientReq.AddCookie(sessionCookie(sessionID))
	clientResp, err := http.DefaultClient.Do(clientReq)
	if err != nil {
		t.Fatalf("client request failed: %v", err)
	}
	defer func() { _ = clientResp.Body.Close() }()

	clientBody, err := io.ReadAll(clientResp.Body)
	if err != nil {
		t.Fatalf("failed reading stream body (content length mismatch): %v", err)
	}
	if string(clientBody) != body {
		t.Errorf("expected body %q, got %q", body, string(clientBody))
	}
}

func TestTorrServerAPI_ActionsAndFiles(t *testing.T) {
	var dropCalled, remCalled bool
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			action, _ := payload["action"].(string)
			switch action {
			case "drop":
				dropCalled = true
				w.WriteHeader(http.StatusOK)
			case "rem":
				remCalled = true
				w.WriteHeader(http.StatusOK)
			case "get":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"hash":"hash1","title":"Movie 1","torrent_size":1000,"file_stats":[{"id":1,"path":"vid.mp4","length":1000}]}`))
			case "list":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}
		case "/gst/hash1/probe":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"DurationNS":1000000000,"Container":"QuickTime / MOV","Tracks":[{"Index":0,"Type":"video","Width":1920,"Height":1080}]}`))
		}
	})

	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()

	sessionID, _ := sessions.CreateSession(&auth.User{
		Username: "test",
		Role:     config.RoleAdmin,
	})

	// 1. Test Drop action
	reqDrop := httptest.NewRequest(http.MethodPost, "/api/torrserver/action?op=drop&hash=hash1", nil)
	reqDrop.AddCookie(sessionCookie(sessionID))
	rrDrop := httptest.NewRecorder()
	srv.ServeHTTP(rrDrop, reqDrop)
	if rrDrop.Code != http.StatusOK || !dropCalled {
		t.Fatalf("expected drop OK, got code %d, dropCalled %v", rrDrop.Code, dropCalled)
	}

	// 2. Test Rem action
	reqRem := httptest.NewRequest(http.MethodPost, "/api/torrserver/action?op=rem&hash=hash1", nil)
	reqRem.AddCookie(sessionCookie(sessionID))
	rrRem := httptest.NewRecorder()
	srv.ServeHTTP(rrRem, reqRem)
	if rrRem.Code != http.StatusOK || !remCalled {
		t.Fatalf("expected rem OK, got code %d, remCalled %v", rrRem.Code, remCalled)
	}

	// 3. Test Files inspection
	reqFiles := httptest.NewRequest(http.MethodGet, "/api/torrserver/files?hash=hash1", nil)
	reqFiles.AddCookie(sessionCookie(sessionID))
	rrFiles := httptest.NewRecorder()
	srv.ServeHTTP(rrFiles, reqFiles)
	if rrFiles.Code != http.StatusOK || !strings.Contains(rrFiles.Body.String(), "Loaded 1 file(s) for Movie 1") {
		t.Fatalf("expected files OK with loaded-files alert, got code %d, body %s", rrFiles.Code, rrFiles.Body.String())
	}

	// 4. Test Probe inspection
	reqProbe := httptest.NewRequest(http.MethodGet, "/api/torrserver/probe?hash=hash1&index=1", nil)
	reqProbe.AddCookie(sessionCookie(sessionID))
	rrProbe := httptest.NewRecorder()
	srv.ServeHTTP(rrProbe, reqProbe)
	if rrProbe.Code != http.StatusOK || !strings.Contains(rrProbe.Body.String(), "1920x1080") {
		t.Fatalf("expected probe OK with 1920x1080, got code %d, body %s", rrProbe.Code, rrProbe.Body.String())
	}
}

func TestStreamProxyCannotPlantCookiesOrRunPagesOnTheAppOrigin(t *testing.T) {
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", auth.SessionCookieName+"=planted; Path=/")
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-3/100")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("mp4!"))
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	sessionID, _ := sessions.CreateSession(&auth.User{Username: "user-1"})

	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/stream/movie.mkv?link="+duneHash+"&index=1&play", nil)
	req.Header.Set("Range", "bytes=0-3")
	req.AddCookie(sessionCookie(sessionID))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusPartialContent || rr.Body.String() != "mp4!" {
		t.Fatalf("proxied range = %d %q, want 206 %q", rr.Code, rr.Body.String(), "mp4!")
	}
	for header, want := range map[string]string{"Content-Type": "video/mp4", "Content-Range": "bytes 0-3/100", "Accept-Ranges": "bytes"} {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if got := rr.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %q, want none: TorrServer must not set cookies on the app origin", got)
	}
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want a sandbox so proxied documents cannot run as the app", csp)
	}
}

func TestStreamProxyOnlyForwardsTorrServerMediaPaths(t *testing.T) {
	var forwarded []string
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, r.URL.Path)
		_, _ = w.Write([]byte("ok"))
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	sessionID, _ := sessions.CreateSession(&auth.User{Username: "user-1"})

	get := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/"+path, nil)
		req.AddCookie(sessionCookie(sessionID))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr.Code
	}

	for _, media := range []string{"stream?link=" + duneHash + "&index=1&play", "stream/movie.mkv?link=" + duneHash + "&index=1", "gst/hash1/master.m3u8?index=1", "gst/hash1/seg0.ts"} {
		if code := get(media); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", media, code)
		}
	}
	forwarded = nil
	for _, api := range []string{"settings", "torrents", "echo", "shutdown", "streams/x", "gstx/y", "viewed"} {
		if code := get(api); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", api, code)
		}
	}
	if len(forwarded) != 0 {
		t.Errorf("non-media paths reached TorrServer: %q", forwarded)
	}
}

// A link on another site opens the proxy with the visitor's cookie, so it
// must not be able to make TorrServer add a torrent or fetch an address.
func TestTheStreamProxyOnlyPlaysAFileOfATorrentByItsHash(t *testing.T) {
	var forwarded []string
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, r.URL.RawQuery)
		_, _ = w.Write([]byte("ok"))
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	sessionID, _ := sessions.CreateSession(&auth.User{Username: "user-1"})
	get := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/"+path, nil)
		req.AddCookie(sessionCookie(sessionID))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr.Code
	}

	for _, hostile := range []string{
		"stream?link=" + url.QueryEscape("magnet:?xt=urn:btih:"+duneHash) + "&index=1&play&save",
		"stream/x.mkv?link=" + url.QueryEscape("http://10.0.0.1/evil.torrent") + "&index=1&play",
		"stream/x.mkv?link=" + duneHash + "&play",
		"stream/x.mkv?link=" + duneHash + "&index=zero&play",
	} {
		if code := get(hostile); code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", hostile, code)
		}
	}
	if len(forwarded) != 0 {
		t.Fatalf("TorrServer was asked %q", forwarded)
	}

	if code := get("stream/x.mkv?link=" + strings.ToUpper(duneHash) + "&index=2&play&save&title=x&poster=y"); code != http.StatusOK {
		t.Fatalf("playing a file = %d, want 200", code)
	}
	if want := "index=2&link=" + duneHash + "&play"; len(forwarded) != 1 || forwarded[0] != want {
		t.Errorf("TorrServer was asked %q, want only %q", forwarded, want)
	}
}

// Viewers add and stop torrents; removing one (and its saved state) is for
// administrators.
func TestOnlyAnAdministratorRemovesATorrent(t *testing.T) {
	var asked []string
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/torrents" {
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			action, _ := payload["action"].(string)
			asked = append(asked, action)
			if action == "list" {
				_, _ = w.Write([]byte(`[]`))
			}
		}
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	viewer, _ := sessions.CreateSession(&auth.User{Username: "anna", Role: config.RoleViewer})
	admin, _ := sessions.CreateSession(&auth.User{Username: "admin", Role: config.RoleAdmin})
	do := func(method, path, session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(sessionCookie(session))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr
	}

	if rr := do(http.MethodPost, "/api/torrserver/action?op=rem&hash=hash1", viewer); rr.Code != http.StatusForbidden {
		t.Errorf("a viewer removing a torrent = %d, want 403", rr.Code)
	}
	if slices.Contains(asked, "rem") {
		t.Fatal("a viewer's remove reached TorrServer")
	}
	if rr := do(http.MethodPost, "/api/torrserver/action?op=drop&hash=hash1", viewer); rr.Code != http.StatusOK || !slices.Contains(asked, "drop") {
		t.Errorf("a viewer stopping a torrent = %d (TorrServer asked %q), want 200", rr.Code, asked)
	}
	if rr := do(http.MethodPost, "/api/torrserver/action?op=rem&hash=hash1", admin); rr.Code != http.StatusOK || !slices.Contains(asked, "rem") {
		t.Errorf("an administrator removing a torrent = %d (TorrServer asked %q), want 200", rr.Code, asked)
	}

	for session, want := range map[string]string{viewer: "canRemove: false", admin: "canRemove: true"} {
		if page := do(http.MethodGet, "/torrserver", session).Body.String(); !strings.Contains(page, want) {
			t.Errorf("TorrServer page lacks %q", want)
		}
	}
}

func TestFailedTorrServerActionsAreReportedToTheUser(t *testing.T) {
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["action"] == "list" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError) // drop, rem and get all fail
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	sessionID, _ := sessions.CreateSession(&auth.User{Username: "user-1", Role: config.RoleAdmin})

	for target, want := range map[string]string{
		"/api/torrserver/action?op=drop&hash=hash1": "could not stop",
		"/api/torrserver/action?op=rem&hash=hash1":  "could not remove",
		"/api/torrserver/files?hash=hash1":          "could not load the files",
	} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.Header.Set("Datastar-Request", "true")
		req.AddCookie(sessionCookie(sessionID))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		body := rr.Body.String()
		if rr.Code != http.StatusOK || !strings.Contains(body, "alert-error") || !strings.Contains(body, want) {
			t.Errorf("POST %s = %d, want an error alert containing %q, got:\n%s", target, rr.Code, want, body)
		}
	}
}

func TestFetchingFilesOfATorrentStillGettingInfoKeepsItAndSaysWhy(t *testing.T) {
	var asked []string
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		action, _ := payload["action"].(string)
		asked = append(asked, action)
		switch action {
		case "get":
			_, _ = w.Write([]byte(`{"hash":"` + duneHash + `","title":"Extraction 2","stat":1,"stat_string":"Torrent getting info"}`))
		case "list":
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Extraction 2","stat":1,"stat_string":"Torrent getting info"}]`))
		}
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	sessionID, _ := sessions.CreateSession(&auth.User{Username: "anna", Role: config.RoleViewer})

	req := httptest.NewRequest(http.MethodPost, "/api/torrserver/files?hash="+duneHash, nil)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(sessionCookie(sessionID))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if slices.Contains(asked, "drop") || slices.Contains(asked, "rem") {
		t.Errorf("fetching files asked TorrServer %q: a torrent still getting info would be removed", asked)
	}
	if body := rr.Body.String(); !strings.Contains(body, "still getting") || strings.Contains(body, "Loaded 0") {
		t.Errorf("the answer does not say the torrent is still getting its info:\n%s", body)
	}
}

// TorrServer saves a torrent once it has its info: dropping one still
// getting info would remove it, which only an administrator's remove may do.
func TestATorrentStillGettingInfoCannotBeDropped(t *testing.T) {
	var asked []string
	torrHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		action, _ := payload["action"].(string)
		asked = append(asked, action)
		if action == "list" {
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Extraction 2","stat":1,"stat_string":"Torrent getting info"},` +
				`{"hash":"` + strings.Repeat("b", 40) + `","title":"Dune","stat":3,"stat_string":"Torrent working"}]`))
		}
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, torrHandler)
	defer ts.Close()
	for _, role := range []string{config.RoleViewer, config.RoleAdmin} {
		asked = nil
		session, _ := sessions.CreateSession(&auth.User{Username: role, Role: role})
		req := httptest.NewRequest(http.MethodPost, "/api/torrserver/action?op=drop&hash="+duneHash, nil)
		req.Header.Set("Datastar-Request", "true")
		req.AddCookie(sessionCookie(session))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if slices.Contains(asked, "drop") {
			t.Errorf("%s: a torrent still getting info was dropped (removed)", role)
		}
		if !strings.Contains(rr.Body.String(), "still getting its info") {
			t.Errorf("%s: the refusal does not say why:\n%s", role, rr.Body.String())
		}

		page := httptest.NewRequest(http.MethodGet, "/torrserver", nil)
		page.AddCookie(sessionCookie(session))
		prr := httptest.NewRecorder()
		srv.ServeHTTP(prr, page)
		drops := regexp.MustCompile(`data-action="drop"\s+data-hash="([0-9a-f]{40})"`).FindAllStringSubmatch(prr.Body.String(), -1)
		if len(drops) != 1 || drops[0][1] != strings.Repeat("b", 40) {
			t.Errorf("%s: drop buttons %q, want one, for the working torrent only", role, drops)
		}
	}
}
