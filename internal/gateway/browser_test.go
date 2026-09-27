package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// lampa is where a browser app such as Lampa is loaded from: not the
// gateway's own address, so the browser checks with the gateway first.
const lampa = "http://192.168.1.50:8080"

// preflight sends the check a browser makes before an app's POST with a
// login and a JSON body, from a device at remote.
func (s *setup) preflight(t *testing.T, target, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, target, nil)
	req.RemoteAddr = remote
	req.Header.Set("Origin", lampa)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func TestBrowserAppsPassTheBrowsersCheckWithoutALogin(t *testing.T) {
	s := newSetup(t)

	rec := s.preflight(t, "/torrents", tv)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /torrents = %d %q, want 204", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := strings.ToLower(h.Get("Access-Control-Allow-Headers")); !strings.Contains(got, "authorization") || !strings.Contains(got, "content-type") {
		t.Errorf("Access-Control-Allow-Headers = %q, want authorization and content-type", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("Access-Control-Allow-Methods = %q, want POST", got)
	}
	if got := s.engine.asked(); len(got) != 0 {
		t.Errorf("TorrServer was asked %q", got)
	}
}

func TestBrowsersChecksDoNotCountAsWrongLogins(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")

	for range 30 {
		s.preflight(t, "/torrents", tv)
	}
	if rec := s.ask(t, http.MethodPost, "/torrents", `{"action":"list"}`, user, password, tv); rec.Code != http.StatusOK {
		t.Errorf("POST /torrents after the browser's checks = %d %q, want 200", rec.Code, rec.Body.String())
	}
}

func TestBrowsersChecksFromTheInternetAreRefusedUnlessItIsAllowed(t *testing.T) {
	s := newSetup(t)

	if rec := s.preflight(t, "/torrents", "203.0.113.9:40000"); rec.Code != http.StatusForbidden {
		t.Errorf("OPTIONS /torrents from the internet = %d, want 403", rec.Code)
	}
}

func TestPagesOnTheInternetMayReachTheGatewayOnTheHomeNetwork(t *testing.T) {
	s := newSetup(t)
	req := httptest.NewRequest(http.MethodOptions, "/torrents", nil)
	req.RemoteAddr = tv
	req.Header.Set("Origin", "https://lampa.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Errorf("Access-Control-Allow-Private-Network = %q, want true", got)
	}
}

// fromLampa sends a request as a browser app loaded from lampa does.
func (s *setup) fromLampa(t *testing.T, method, target, body, user, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = tv
	req.Header.Set("Origin", lampa)
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func TestBrowserAppsCanReadTheAnswers(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")

	for _, tc := range []struct {
		name           string
		user, password string
		wantCode       int
	}{
		{"TorrServer's answer", user, password, http.StatusOK},
		{"a wrong login", user, "wrong", http.StatusUnauthorized},
	} {
		rec := s.fromLampa(t, http.MethodPost, "/torrents", `{"action":"list"}`, tc.user, tc.password)
		if rec.Code != tc.wantCode {
			t.Errorf("%s: POST /torrents = %d, want %d", tc.name, rec.Code, tc.wantCode)
		}
		if got := rec.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "*" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want exactly *", tc.name, got)
		}
	}
}
