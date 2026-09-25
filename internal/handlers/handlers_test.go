package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithConfig(t, validTestConfig(t))
}

func newTestServerWithConfig(t *testing.T, cfg Config) *Server {
	t.Helper()
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

// signedIn returns a session cookie for the test admin Alex.
func signedIn(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	token, err := server.sessions.CreateSession(server.accounts.Lookup(testUsername))
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

type handlerErrorReader struct {
	err error
}

func (r handlerErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type handlerCountingReader struct {
	next byte
}

func (r *handlerCountingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

func TestSignedOutVisitorsStartAtSignIn(t *testing.T) {
	server := newTestServer(t)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("GET / = %d → %q, want 303 → /login", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	body := rec.Body.String()
	for _, want := range []string{
		"Moviestracker",
		"This product uses the TMDB API but is not endorsed or certified by TMDB.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sign-in page missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"cdn.jsdelivr.net",
		"unpkg.com",
		"<style",
		"<script>",
		" style=",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("sign-in page contains CSP-incompatible markup %q", forbidden)
		}
	}
	for _, asset := range []string{"/static/app.css", "/static/theme.js", "/static/datastar.js"} {
		if !strings.Contains(body, asset) {
			t.Errorf("sign-in page does not reference local asset %q", asset)
		}
	}
}

func TestSignedInVisitorLandsOnTrending(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/movies" {
		t.Fatalf("Location = %q, want /movies", got)
	}
}

func TestStaticAssets(t *testing.T) {
	server := newTestServer(t)
	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/static/app.css", contentType: "text/css"},
		{path: "/static/theme.js", contentType: "javascript"},
		{path: "/static/player.js", contentType: "javascript"},
		{path: "/static/datastar.js", contentType: "javascript"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tt.contentType) {
				t.Fatalf("Content-Type = %q, want it to contain %q", got, tt.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("asset response is empty")
			}
		})
	}
}

func TestLoginPage(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Before you sign in") {
		t.Error("expected the disclaimer step heading in response body")
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	server := newTestServer(t)

	payload, _ := json.Marshal(map[string]any{
		"username": "bad",
		"password": "wrong",
	})
	req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBuffer(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(strings.ToLower(body), "invalid username or password") {
		t.Errorf("expected error message in response, got: %s", body)
	}
}

func TestJSONLoginWithoutDatastarHeadersUsesHTTPRedirect(t *testing.T) {
	server := newTestServer(t)
	body := strings.NewReader(`{"username":"alex","password":"correct horse battery"}`)
	req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/movies" {
		t.Fatalf("Location = %q, want /movies", got)
	}
}

func TestLoginRequestValidation(t *testing.T) {
	const maxBodyBytes = 1 << 20
	validPrefix := "username=alex&password="
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{
			name:        "body at limit is decoded",
			body:        validPrefix + strings.Repeat("x", maxBodyBytes-len(validPrefix)),
			contentType: "application/x-www-form-urlencoded",
			wantStatus:  http.StatusSeeOther,
		},
		{
			name:        "body one byte over limit",
			body:        strings.Repeat("x", maxBodyBytes+1),
			contentType: "application/json",
			wantStatus:  http.StatusRequestEntityTooLarge,
		},
		{
			name:        "malformed JSON",
			body:        "{",
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "unsupported content type",
			body:        "username=alex&password=correct horse battery",
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t)
			req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(tt.body)))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()

			server.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestLoginRateAndSessionCapacity(t *testing.T) {
	t.Run("rate limit", func(t *testing.T) {
		cfg := validTestConfig(t)
		cfg.LoginAttempts = 2
		server := newTestServerWithConfig(t, cfg)

		for attempt := 1; attempt <= 3; attempt++ {
			req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=nobody&password=wrong")))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "192.0.2.1:1234"
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if attempt < 3 && rec.Code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d was rate limited early", attempt)
			}
			if attempt == 3 && rec.Code != http.StatusTooManyRequests {
				t.Fatalf("attempt 3 status = %d, want 429", rec.Code)
			}
		}
	})

	t.Run("session capacity", func(t *testing.T) {
		cfg := validTestConfig(t)
		cfg.Sessions.Close()
		cfg.Sessions = auth.NewSessionManager(1, &handlerCountingReader{})
		server := newTestServerWithConfig(t, cfg)

		for attempt, want := range []int{http.StatusSeeOther, http.StatusServiceUnavailable} {
			req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=alex&password=correct+horse+battery")))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "192.0.2.2:1234"
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("attempt %d status = %d, want %d", attempt+1, rec.Code, want)
			}
		}
	})

	t.Run("entropy failure is generic", func(t *testing.T) {
		sourceErr := errors.New("sensitive entropy source failure")
		cfg := validTestConfig(t)
		cfg.Sessions.Close()
		cfg.Sessions = auth.NewSessionManager(1, handlerErrorReader{err: sourceErr})
		server := newTestServerWithConfig(t, cfg)
		req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=alex&password=correct+horse+battery")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if strings.Contains(rec.Body.String(), sourceErr.Error()) {
			t.Fatalf("response leaks internal error: %q", rec.Body.String())
		}
	})
}

func TestLoginRateLimitUsesConfiguredRetryAfter(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.LoginAttempts = 1
	cfg.LoginWindow = 90 * time.Second
	server := newTestServerWithConfig(t, cfg)

	for attempt := 1; attempt <= 2; attempt++ {
		req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=nobody&password=wrong")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if attempt == 2 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
			}
			if got := rec.Header().Get("Retry-After"); got != "90" {
				t.Fatalf("Retry-After = %q, want 90", got)
			}
		}
	}
}

func TestUntrustedForwardedForCannotBypassLoginLimit(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.LoginAttempts = 1
	cfg.TrustedProxyCIDRs = nil
	server := newTestServerWithConfig(t, cfg)

	for attempt, forwardedFor := range []string{"198.51.100.10", "198.51.100.11"} {
		req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=nobody&password=wrong")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", forwardedFor)
		req.RemoteAddr = "192.0.2.20:1234"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if attempt == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second request status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
	}
}

func TestTrustedProxyUsesFirstUntrustedAddress(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.LoginAttempts = 1
	cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	server := newTestServerWithConfig(t, cfg)

	chains := []string{
		"198.51.100.10, 127.0.0.2",
		"198.51.100.11, 127.0.0.2",
	}
	for _, chain := range chains {
		req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=nobody&password=wrong")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", chain)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("chain %q status = %d, want %d", chain, rec.Code, http.StatusSeeOther)
		}
	}
}

func TestMalformedForwardedForFallsBackToPeer(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.LoginAttempts = 1
	cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	server := newTestServerWithConfig(t, cfg)

	for attempt, chain := range []string{"not-an-ip", "also-not-an-ip"} {
		req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=nobody&password=wrong")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", chain)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if attempt == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second malformed chain status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
	}
}

func TestLoginAndAuthenticatedFlow(t *testing.T) {
	server := newTestServer(t)

	// 1. Submit valid login via SSE (including client signals payload from Datastar)
	payload, _ := json.Marshal(map[string]any{
		"redirectUrl":  "",
		"username":     "alex",
		"password":     "correct horse battery",
		"submitting":   true,
		"errorMessage": "",
	})
	req := withConsent(httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBuffer(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "redirectUrl") || strings.Contains(body, "<script") {
		t.Fatalf("login redirect patch is not CSP-safe: %q", body)
	}

	// Verify cookie was set
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// 2. Access the authenticated home with session cookie
	reqAuthed := httptest.NewRequest(http.MethodGet, "/movies", nil)
	reqAuthed.AddCookie(sessionCookie)
	recAuthed := httptest.NewRecorder()
	server.ServeHTTP(recAuthed, reqAuthed)

	if recAuthed.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recAuthed.Code)
	}
	if !strings.Contains(recAuthed.Body.String(), "Alex Rivers") {
		t.Error("expected user name 'Alex Rivers' in the signed-in navbar")
	}

	// 3. Logout
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	reqLogout.AddCookie(sessionCookie)
	reqLogout.Header.Set("Accept", "text/event-stream")
	recLogout := httptest.NewRecorder()
	server.ServeHTTP(recLogout, reqLogout)

	if recLogout.Code != http.StatusOK {
		t.Fatalf("expected status 200 on logout, got %d", recLogout.Code)
	}
	if body := recLogout.Body.String(); !strings.Contains(body, "redirectUrl") || strings.Contains(body, "<script") {
		t.Fatalf("logout redirect patch is not CSP-safe: %q", body)
	}
}

func TestStreamsAreBoundedAndReleaseTheirSlot(t *testing.T) {
	server := newTestServer(t)
	cookie := signedIn(t, server)
	if !server.acquireSSE() {
		t.Fatal("failed to reserve test SSE slot")
	}

	rejectedCtx, rejectedCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer rejectedCancel()
	rejectedReq := httptest.NewRequest(http.MethodGet, "/api/torrserver/torrents?stream=true", nil).WithContext(rejectedCtx)
	rejectedReq.AddCookie(cookie)
	rejectedRec := httptest.NewRecorder()
	server.ServeHTTP(rejectedRec, rejectedReq)
	if rejectedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("capacity status = %d, want 429", rejectedRec.Code)
	}
	if got := rejectedRec.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
	server.releaseSSE()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/torrserver/torrents?stream=true", nil).WithContext(ctx)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if !server.acquireSSE() {
		t.Fatal("stream permit was not released after request cancellation")
	}
	server.releaseSSE()
}

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	const wantCSP = "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self'; img-src 'self' data: https: http:; media-src 'self' blob: data: http: https:; frame-src 'self' https://www.youtube-nocookie.com https://www.youtube.com; connect-src 'self' http: https:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"

	for _, tt := range []struct {
		name     string
		secure   bool
		wantHSTS string
	}{
		{name: "HTTP", secure: false},
		{name: "HTTPS", secure: true, wantHSTS: "max-age=31536000; includeSubDomains"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := SecurityHeadersMiddleware(tt.secure, inner)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			h := rec.Header()
			if got := h.Get("Content-Security-Policy"); got != wantCSP {
				t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
			}
			if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := h.Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if got := h.Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
				t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
			}
			if got := h.Get("Strict-Transport-Security"); got != tt.wantHSTS {
				t.Errorf("Strict-Transport-Security = %q, want %q", got, tt.wantHSTS)
			}
		})
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("before response commitment", func(t *testing.T) {
		panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("unexpected runtime failure")
		})
		handler := RecoveryMiddleware(panickingHandler)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), http.StatusText(http.StatusInternalServerError)) {
			t.Errorf("body = %q, want generic server error", rec.Body.String())
		}
	})

	t.Run("after response commitment", func(t *testing.T) {
		panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("partial response"))
			panic("unexpected runtime failure")
		})
		handler := RecoveryMiddleware(panickingHandler)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

		if rec.Code != http.StatusAccepted {
			t.Errorf("status = %d, want 202", rec.Code)
		}
		if got := rec.Body.String(); got != "partial response" {
			t.Errorf("body = %q, want only the committed response", got)
		}
	})
}

func TestResponseWriterRecorder(t *testing.T) {
	t.Run("implicit body write records OK", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriterRecorder{ResponseWriter: rec}

		if _, err := rw.Write([]byte("ok")); err != nil {
			t.Fatalf("Write(): %v", err)
		}
		if rw.statusCode != http.StatusOK || !rw.wroteHeader {
			t.Errorf("status = %d, wroteHeader = %t; want 200, true", rw.statusCode, rw.wroteHeader)
		}
	})

	t.Run("first explicit status wins", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriterRecorder{ResponseWriter: rec}

		rw.WriteHeader(http.StatusCreated)
		rw.WriteHeader(http.StatusTeapot)

		if rw.statusCode != http.StatusCreated {
			t.Errorf("recorded status = %d, want 201", rw.statusCode)
		}
		if rec.Code != http.StatusCreated {
			t.Errorf("underlying status = %d, want 201", rec.Code)
		}
		if rw.Unwrap() != rec {
			t.Error("expected Unwrap to return underlying recorder")
		}
	})

	t.Run("flush commits OK", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriterRecorder{ResponseWriter: rec}
		rw.Flush()
		if rw.statusCode != http.StatusOK || !rw.wroteHeader {
			t.Errorf("status = %d, wroteHeader = %t; want 200, true", rw.statusCode, rw.wroteHeader)
		}
	})
}

func TestMemoryLeakCheck(t *testing.T) {
	server := newTestServer(t)

	// Warmup
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/login", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Run 10,000 requests
	for i := 0; i < 10000; i++ {
		req := httptest.NewRequest(http.MethodGet, "/login", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	var diff uint64
	if m2.Alloc > m1.Alloc {
		diff = m2.Alloc - m1.Alloc
	}
	t.Logf("Before: %d KB, After: %d KB, Diff: %d KB", m1.Alloc/1024, m2.Alloc/1024, diff/1024)

	// If there's a leak, diff would be megabytes. Normal Go difference after GC is under 100 KB.
	if diff > 500*1024 {
		t.Fatalf("Potential memory leak detected: diff=%d KB", diff/1024)
	}
}

func TestHealthEndpoints(t *testing.T) {
	server := newTestServer(t)

	tests := []struct {
		path     string
		wantBody string
	}{
		{path: "/healthz", wantBody: "ok\n"},
		{path: "/readyz", wantBody: "ready\n"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestCrossSiteFormsCannotActForASignedInUser(t *testing.T) {
	server := newTestServer(t)
	cookie := signedIn(t, server)

	logout := func(fetchSite string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		req.Header.Set("Sec-Fetch-Site", fetchSite)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := logout("cross-site"); code != http.StatusForbidden {
		t.Fatalf("cross-site logout status = %d, want %d", code, http.StatusForbidden)
	}
	if server.sessions.GetUser(cookie.Value) == nil {
		t.Fatal("cross-site logout ended the session")
	}
	if code := logout("same-origin"); code != http.StatusSeeOther {
		t.Fatalf("same-origin logout status = %d, want %d", code, http.StatusSeeOther)
	}
}

func TestClosingTheServerEndsOpenStreams(t *testing.T) {
	server := newTestServer(t)
	cookie := signedIn(t, server)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The client never disconnects; only the server can end this stream.
		req := httptest.NewRequest(http.MethodGet, "/api/torrserver/torrents?stream=true", nil)
		req.AddCookie(cookie)
		server.ServeHTTP(httptest.NewRecorder(), req)
	}()
	time.Sleep(50 * time.Millisecond) // let the stream start

	server.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("torrent list stream still open after Close")
	}
	if !server.acquireSSE() {
		t.Fatal("stream permit was not released after Close")
	}
}
