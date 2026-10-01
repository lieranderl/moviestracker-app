package web_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

const (
	testClientID = "client-123.apps.googleusercontent.com"
	testBaseURL  = "https://web.example"
)

// now is the tests' clock.
var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// google is a stand-in for Google's OAuth endpoints, the external boundary
// of signing in. Its token endpoint trades the code "good-code" for an ID
// token with claims.
type google struct {
	server   *httptest.Server
	claims   map[string]any
	verifier string // the PKCE verifier the app sent with the code
}

func newGoogle(t *testing.T) *google {
	t.Helper()
	g := &google{claims: map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"sub":            "1098765",
		"email":          "ann@example.com",
		"email_verified": true,
		"name":           "Ann Lee",
		"exp":            now.Add(time.Hour).Unix(),
	}}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.PostFormValue("code") != "good-code" || r.PostFormValue("client_id") != testClientID {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		g.verifier = r.PostFormValue("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "at", "token_type": "Bearer", "id_token": idToken(t, g.claims),
		})
	}))
	t.Cleanup(g.server.Close)
	return g
}

// idToken is a JWT carrying claims, shaped as Google's token endpoint
// returns it.
func idToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`)) + "." + enc.EncodeToString(payload) + ".c2ln"
}

// signIn goes through Google's sign-in as a browser would and returns the
// callback's response.
func signIn(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	_, back := signInFrom(t, h)
	return back
}

// signInFrom is signIn that also returns the redirect to Google.
func signInFrom(t *testing.T, h http.Handler) (start, back *httptest.ResponseRecorder) {
	t.Helper()
	start = get(t, h, "/auth/google")
	to, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=good-code&state="+url.QueryEscape(to.Query().Get("state")), nil)
	for _, c := range start.Result().Cookies() {
		req.AddCookie(c)
	}
	back = httptest.NewRecorder()
	h.ServeHTTP(back, req)
	return start, back
}

// getWith is a GET sent with the cookies a response set.
func getWith(t *testing.T, h http.Handler, path string, from *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range from.Result().Cookies() {
		if c.MaxAge >= 0 {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func (g *google) config() web.Config {
	return web.Config{
		Now:        func() time.Time { return now },
		BaseURL:    testBaseURL,
		SessionKey: []byte("0123456789abcdef0123456789abcdef"),
		Google: web.Google{
			ClientID:     testClientID,
			ClientSecret: "shh",
			AuthURL:      g.server.URL + "/o/oauth2/v2/auth",
			TokenURL:     g.server.URL + "/token",
		},
	}
}

func TestSignInSendsTheVisitorToGoogle(t *testing.T) {
	g := newGoogle(t)
	res := get(t, web.New(g.config()), "/auth/google")
	if res.Code != http.StatusFound {
		t.Fatalf("GET /auth/google = %d, want 302", res.Code)
	}
	to, err := url.Parse(res.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := to.Scheme + "://" + to.Host + to.Path; got != g.server.URL+"/o/oauth2/v2/auth" {
		t.Errorf("redirect goes to %q, want Google's sign-in page", got)
	}
	q := to.Query()
	for key, want := range map[string]string{
		"client_id":             testClientID,
		"redirect_uri":          testBaseURL + "/auth/google/callback",
		"response_type":         "code",
		"scope":                 "openid email profile",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if q.Get("state") == "" || q.Get("code_challenge") == "" {
		t.Errorf("state = %q, code_challenge = %q, want both set", q.Get("state"), q.Get("code_challenge"))
	}
	if len(res.Result().Cookies()) == 0 {
		t.Error("no cookie remembers the sign-in attempt, so the callback could not check it")
	}
}

func TestAVerifiedGoogleAccountSignsIn(t *testing.T) {
	g := newGoogle(t)
	h := web.New(g.config())
	start, back := signInFrom(t, h)
	if back.Code != http.StatusSeeOther || back.Header().Get("Location") != "/" {
		t.Fatalf("callback = %d to %q, want 303 to /", back.Code, back.Header().Get("Location"))
	}
	home := getWith(t, h, "/", back)
	body := home.Body.String()
	for _, want := range []string{"Ann Lee", `action="/api/logout"`} {
		if !strings.Contains(body, want) {
			t.Errorf("home page after signing in lacks %q", want)
		}
	}
	if strings.Contains(body, "Sign in with Google") {
		t.Error("home page after signing in still offers to sign in")
	}
	// PKCE (RFC 7636, S256): the verifier sent with the code is the one the
	// redirect's challenge was made from.
	to, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(g.verifier))
	if challenge := to.Query().Get("code_challenge"); base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Errorf("code_verifier %q does not answer code_challenge %q", g.verifier, challenge)
	}
}

// The navbar shows the Google account's picture when it has one, and the
// initials when it has none or it is not an HTTPS image.
func TestTheAvatarShowsTheGoogleAccountsPicture(t *testing.T) {
	const picture = "https://lh3.googleusercontent.com/a/ann=s96-c"
	for _, tc := range []struct{ picture, want, not string }{
		{picture, `<img src="` + picture + `"`, ""},
		{picture, `data-init="el.complete && !el.naturalWidth && el.remove()" data-on:error="el.remove()"`, ""},
		{"", "AL", "<img src="},
		{"javascript:alert(1)", "AL", "javascript:"},
	} {
		g := newGoogle(t)
		if tc.picture != "" {
			g.claims["picture"] = tc.picture
		}
		h := web.New(g.config())
		_, back := signInFrom(t, h)
		body := getWith(t, h, "/", back).Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("picture %q: the navbar lacks %q", tc.picture, tc.want)
		}
		if tc.not != "" && strings.Contains(body, tc.not) {
			t.Errorf("picture %q: the navbar shows %q", tc.picture, tc.not)
		}
	}
}

// sessionCookie is the session a response set, if any.
func sessionCookie(res *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range res.Result().Cookies() {
		if c.Name == "mt_session" && c.MaxAge > 0 {
			return c
		}
	}
	return nil
}

func TestGoogleAccountsThatCannotSignIn(t *testing.T) {
	for name, change := range map[string]func(claims map[string]any){
		"unverified email":      func(c map[string]any) { c["email_verified"] = false },
		"token for another app": func(c map[string]any) { c["aud"] = "someone-else.apps.googleusercontent.com" },
		"token not from Google": func(c map[string]any) { c["iss"] = "https://evil.example" },
		"token already expired": func(c map[string]any) { c["exp"] = now.Add(-time.Minute).Unix() },
		"token without account": func(c map[string]any) { delete(c, "sub") },
	} {
		t.Run(name, func(t *testing.T) {
			g := newGoogle(t)
			change(g.claims)
			back := signIn(t, web.New(g.config()))
			if back.Code != http.StatusForbidden || sessionCookie(back) != nil {
				t.Errorf("callback = %d with session %v, want 403 and no session", back.Code, sessionCookie(back) != nil)
			}
		})
	}
}

func TestACallbackThisBrowserDidNotStartIsRefused(t *testing.T) {
	g := newGoogle(t)
	h := web.New(g.config())
	start := get(t, h, "/auth/google")
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=good-code&state=forged", nil)
	for _, c := range start.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || sessionCookie(rec) != nil {
		t.Errorf("forged callback = %d with session %v, want 400 and no session", rec.Code, sessionCookie(rec) != nil)
	}
}

func TestASessionLastsThirtyDays(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	back := signIn(t, web.New(cfg))
	later := func(d time.Duration) string {
		cfg.Now = func() time.Time { return now.Add(d) }
		return getWith(t, web.New(cfg), "/", back).Body.String()
	}
	if body := later(29 * 24 * time.Hour); !strings.Contains(body, "Ann Lee") {
		t.Error("session ended before 30 days")
	}
	if body := later(30 * 24 * time.Hour); !strings.Contains(body, "Sign in with Google") {
		t.Error("session still valid after 30 days")
	}
}

func TestAChangedSessionCookieIsNotTrusted(t *testing.T) {
	g := newGoogle(t)
	h := web.New(g.config())
	c := sessionCookie(signIn(t, h))
	if c == nil {
		t.Fatal("no session after signing in")
	}
	payload, mac, _ := strings.Cut(c.Value, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	forged := strings.Replace(string(raw), "Ann Lee", "Mallory", 1)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name: c.Name, Value: base64.RawURLEncoding.EncodeToString([]byte(forged)) + "." + mac,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if body := rec.Body.String(); strings.Contains(body, "Mallory") || !strings.Contains(body, "Sign in with Google") {
		t.Error("a session cookie changed in the browser was trusted")
	}
}

func TestSigningOutEndsTheSession(t *testing.T) {
	g := newGoogle(t)
	h := web.New(g.config())
	back := signIn(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(sessionCookie(back))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/logout = %d, want 303", out.Code)
	}
	var cleared bool
	for _, c := range out.Result().Cookies() {
		cleared = cleared || (c.Name == "mt_session" && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("signing out did not delete the session cookie")
	}
}

func TestSignInIsUnavailableUntilItIsSetUp(t *testing.T) {
	if res := get(t, web.New(web.Config{}), "/auth/google"); res.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /auth/google without a Google client = %d, want 503", res.Code)
	}
}

func TestASignedInUsersLanguageFollowsThemToOtherBrowsers(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	cfg.Store = store.NewMemory()
	h := web.New(cfg)

	// In one browser, Ann picks Russian.
	req := httptest.NewRequest(http.MethodPost, "/api/language", strings.NewReader("lang=ru"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie(signIn(t, h)))
	h.ServeHTTP(httptest.NewRecorder(), req)

	// In another, she signs in and her pages are in Russian.
	home := getWith(t, h, "/", signIn(t, h))
	if body := home.Body.String(); !strings.Contains(body, `<html lang="ru"`) || !strings.Contains(body, "Главная") {
		t.Error("after signing in elsewhere, the home page is not in the language Ann picked")
	}
}

func TestSigningOutFromThePageMenuTakesTheBrowserHome(t *testing.T) {
	g := newGoogle(t)
	h := web.New(g.config())
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.Header.Set("Datastar-Request", "true") // the navbar's @post('/api/logout')
	req.AddCookie(sessionCookie(signIn(t, h)))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK || !strings.Contains(out.Body.String(), `"redirectUrl":"/"`) {
		t.Errorf("Datastar sign-out = %d %q, want a redirectUrl signal to /", out.Code, out.Body.String())
	}
	var cleared bool
	for _, c := range out.Result().Cookies() {
		cleared = cleared || (c.Name == "mt_session" && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("signing out from the page menu did not delete the session cookie")
	}
}
