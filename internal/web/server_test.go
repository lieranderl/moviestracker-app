package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestCloudRunCanProbeTheWebApp(t *testing.T) {
	h := web.New(web.Config{})
	for path, want := range map[string]string{"/healthz": "ok\n", "/readyz": "ready\n"} {
		res := get(t, h, path)
		if res.Code != http.StatusOK || res.Body.String() != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, res.Code, res.Body, want)
		}
	}
}

func TestPagesAreServedWithTheStrictSecurityHeaders(t *testing.T) {
	res := get(t, web.New(web.Config{}), "/healthz")
	csp := res.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self'") || !strings.Contains(csp, "connect-src 'self' http: https:") {
		t.Errorf("Content-Security-Policy = %q, want the app's strict policy that lets the browser reach a visitor's TorrServer", csp)
	}
	if got := res.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("Strict-Transport-Security = %q, want HSTS: Cloud Run serves HTTPS only", got)
	}
}

func TestTheAppsStylesAndScriptsAreServed(t *testing.T) {
	h := web.New(web.Config{})
	for _, path := range []string{"/static/app.css", "/static/datastar.js", "/static/player.js"} {
		if res := get(t, h, path); res.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, res.Code)
		}
	}
}

func TestASignedOutVisitorIsOfferedGoogleSignIn(t *testing.T) {
	res := get(t, web.New(web.Config{}), "/")
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.Code)
	}
	for _, want := range []string{"Sign in with Google", `href="/auth/google"`, `action="/api/language"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sign-in page lacks %q", want)
		}
	}
}

func TestTheSignInPageSpeaksTheVisitorsLanguage(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	web.New(web.Config{}).ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "Войти через Google") {
		t.Errorf("sign-in page for a Russian browser lacks %q", "Войти через Google")
	}
}
