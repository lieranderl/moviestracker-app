package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withConsent attaches the accepted-disclaimer cookie to a request.
func withConsent(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: consentCookieName, Value: consentVersion, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	return req
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestVisitorSeesDisclaimerBeforeSignInOptions(t *testing.T) {
	server := newTestServer(t)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"free, non-commercial personal catalog",
		"This product uses the TMDB API but is not endorsed or certified by TMDB.",
		"does not host, upload, seed, or distribute media",
		`href="https://www.themoviedb.org/"`,
		`href="https://jacred.su/"`,
		`action="/api/consent"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("consent step missing %q", want)
		}
	}
	if strings.Contains(body, `action="/api/login"`) {
		t.Error("sign-in options must stay hidden until the disclaimer is accepted")
	}
}

func TestVisitorWhoAcceptedSeesSignInOptions(t *testing.T) {
	server := newTestServer(t)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, withConsent(httptest.NewRequest(http.MethodGet, "/login", nil)))

	body := rec.Body.String()
	if !strings.Contains(body, `action="/api/login"`) {
		t.Error("expected sign-in options once the disclaimer is accepted")
	}
	if strings.Contains(body, `action="/api/consent"`) {
		t.Error("accepted visitors should not be asked again")
	}
}

func TestAcceptingDisclaimerWithDatastarPatchesInSignIn(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/consent", strings.NewReader(`{"accepted":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	cookie := findCookie(rec, consentCookieName)
	if cookie == nil || cookie.Value != consentVersion || !cookie.HttpOnly {
		t.Fatalf("consent cookie = %+v, want HttpOnly %q", cookie, consentVersion)
	}
	if cookie.MaxAge < 365*24*60*60 {
		t.Errorf("consent cookie MaxAge = %d, want at least one year", cookie.MaxAge)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "datastar-patch-elements") || !strings.Contains(body, `action="/api/login"`) {
		t.Errorf("expected sign-in step patch, got %q", body)
	}
}

func TestAcceptingDisclaimerWithoutJavaScriptRedirectsToSignIn(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/consent", strings.NewReader("accept=yes"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
	if findCookie(rec, consentCookieName) == nil {
		t.Error("expected consent cookie")
	}
}

func TestDisclaimerMustBeExplicitlyAccepted(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/consent", strings.NewReader("accept="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if findCookie(rec, consentCookieName) != nil {
		t.Fatal("consent cookie must not be set without explicit acceptance")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSignInWithoutConsentIsRefused(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=alex&password=correct+horse+battery"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if findCookie(rec, "datastar_session") != nil {
		t.Fatal("session must not be created before the disclaimer is accepted")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSignInLandsOnTrending(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=alex&password=correct+horse+battery"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, withConsent(req))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/movies" {
		t.Fatalf("got %d %q, want 303 /movies", rec.Code, rec.Header().Get("Location"))
	}
}
