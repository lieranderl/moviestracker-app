package handlers_test

import (
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadinessRefreshChecksCapabilitiesWithoutStartingMedia(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	fake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.147.2"))
		case "/gst/echo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"gstreamer":{"available":true,"works":true}}`))
		default:
			t.Errorf("readiness requested media endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, fake)
	defer ts.Close()
	t.Cleanup(srv.Close)
	t.Cleanup(sessions.Close)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/readiness", nil)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"torrConnected":true`) || !strings.Contains(w.Body.String(), `"gst":true,"gstKnown":true`) {
		t.Errorf("ready refresh = %d: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(paths, []string{"/echo", "/gst/echo"}) {
		t.Errorf("refresh requests = %v; want Echo capabilities only", paths)
	}
	if got := getSignedOutReadiness(srv); got.Code != http.StatusUnauthorized {
		t.Errorf("signed-out readiness = %d", got.Code)
	}
}

func getSignedOutReadiness(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/torrserver/readiness", nil))
	return w
}

func TestReadinessDistinguishesDisconnectedAndUnknownConversion(t *testing.T) {
	for _, tc := range []struct {
		name              string
		echoCode, gstCode int
		gstBody, want     string
	}{
		{"disconnected", 503, 200, `{}`, `"gst":false,"gstKnown":false,"torrConnected":false`},
		{"conversion unknown", 200, 503, `{}`, `"gst":false,"gstKnown":false,"torrConnected":true`},
		{"conversion unsupported", 200, 404, `{}`, `"gst":false,"gstKnown":true,"torrConnected":true`},
		{"conversion metadata missing", 200, 200, `{}`, `"gst":false,"gstKnown":false,"torrConnected":true`},
		{"conversion health missing", 200, 200, `{"gstreamer":{}}`, `"gst":false,"gstKnown":false,"torrConnected":true`},
		{"conversion metadata null", 200, 200, `{"gstreamer":null}`, `"gst":false,"gstKnown":false,"torrConnected":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, sessions, ts := setupTestServerWithTorrServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/echo":
					w.WriteHeader(tc.echoCode)
					_, _ = w.Write([]byte("MatriX.147.2"))
				case "/gst/echo":
					w.WriteHeader(tc.gstCode)
					_, _ = w.Write([]byte(tc.gstBody))
				default:
					t.Errorf("unexpected media request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()
			t.Cleanup(srv.Close)
			t.Cleanup(sessions.Close)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/readiness", nil)))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("readiness = %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAReadinessReplyFromAReplacedServerCannotOverwriteTheCurrentConnection(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			once.Do(func() { close(started) })
			<-release
			_, _ = w.Write([]byte("MatriX.147.2"))
			return
		}
		http.NotFound(w, r)
	}))
	defer fake.Close()
	manager := torrserver.NewManager(fake.URL)
	l := newLocal(t, withAdmin(t), func(cfg *handlers.Config) { cfg.TorrServer = manager })
	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/readiness", nil)
	req.AddCookie(l.admin(t))
	w := httptest.NewRecorder()
	go func() { defer close(done); l.server.ServeHTTP(w, req) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("readiness never reached the external server")
	}
	if err := manager.SetURL("http://127.0.0.1:1"); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	<-done
	if w.Code != http.StatusNoContent || strings.Contains(w.Body.String(), "torrConnected") {
		t.Errorf("replaced server patched readiness: %d %s", w.Code, w.Body.String())
	}
}
