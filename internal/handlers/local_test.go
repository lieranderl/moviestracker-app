package handlers_test

import (
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

const adminPassword = "correct horse battery"

// local is one MoviesTracker install: a data directory, its accounts and the server.
type local struct {
	server   *handlers.Server
	store    *config.Store
	accounts *auth.Accounts
	sessions *auth.SessionManager
}

type localOption func(*handlers.Config)

// testHashes caches low-cost bcrypt hashes, so test servers start quickly
// under the race detector.
var testHashes sync.Map // password -> []byte

// addAccount writes an account straight into store.
func addAccount(t *testing.T, store *config.Store, username, role string) {
	t.Helper()
	hash, ok := testHashes.Load(adminPassword)
	if !ok {
		h, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
		hash, _ = testHashes.LoadOrStore(adminPassword, h)
	}
	err := store.Update(func(st *config.State) error {
		st.Users = append(st.Users, config.User{Username: username, Name: username, Role: role, PasswordHash: string(hash.([]byte))})
		return nil
	})
	if err != nil {
		t.Fatalf("add account: %v", err)
	}
}

// withSetupCode is the code the server printed for setting up from another device.
func withSetupCode(code string) localOption {
	return func(c *handlers.Config) { c.SetupCode = code }
}

// withAdmin completes setup before the server starts.
func withAdmin(t *testing.T) func(*config.Store) {
	return func(store *config.Store) { addAccount(t, store, "admin", config.RoleAdmin) }
}

func newLocal(t *testing.T, prepare func(*config.Store), opts ...localOption) *local {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("config.Open(): %v", err)
	}
	if prepare != nil {
		prepare(store)
	}
	accounts := auth.NewAccounts(store)
	sessions := auth.NewSessionManager(64, rand.Reader, auth.WithStore(store, accounts.Lookup))
	t.Cleanup(sessions.Close)
	cfg := handlers.Config{
		Sessions:        sessions,
		Accounts:        accounts,
		Store:           store,
		MaxSSEStreams:   8,
		LoginAttempts:   5,
		LoginWindow:     time.Minute,
		LoginClientKeys: 16,
		CatalogTimeout:  2 * time.Second,
		TorrServer:      torrserver.NewManager(store.State().TorrServer.URL),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	server, err := handlers.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	t.Cleanup(server.Close)
	return &local{server: server, store: store, accounts: accounts, sessions: sessions}
}

func (l *local) do(t *testing.T, req *http.Request, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	l.server.ServeHTTP(rr, req)
	return rr
}

// action posts Datastar signals, as a button or form on the page does.
func (l *local) action(t *testing.T, path, signals string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(signals))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")
	return l.do(t, req, cookie)
}

func (l *local) signIn(t *testing.T, user *auth.User) *http.Cookie {
	t.Helper()
	token, err := l.sessions.CreateSession(user)
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

func (l *local) admin(t *testing.T) *http.Cookie {
	t.Helper()
	return l.signIn(t, l.accounts.Lookup("admin"))
}

func sessionCookieFrom(rr *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestAFreshInstallSendsEveryPageToSetup(t *testing.T) {
	l := newLocal(t, nil)
	for _, path := range []string{"/", "/movies", "/login", "/torrserver", "/settings/sources"} {
		rr := l.do(t, httptest.NewRequest(http.MethodGet, path, nil), nil)
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/setup" {
			t.Errorf("GET %s = %d → %q, want 303 → /setup", path, rr.Code, rr.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/setup", "/static/app.css", "/healthz"} {
		if rr := l.do(t, httptest.NewRequest(http.MethodGet, path, nil), nil); rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rr.Code)
		}
	}
}

func TestSetupCreatesTheAdminSignsInAndClosesItself(t *testing.T) {
	l := newLocal(t, nil, withSetupCode("7KQ4-M2XD"))

	rr := l.action(t, "/api/setup", `{"setupCode":"7KQ4-M2XD","accepted":false,"username":"evgenii","name":"Evgenii","password":"correct horse battery"}`, nil)
	if !l.accounts.NeedsSetup() || sessionCookieFrom(rr) != nil {
		t.Fatal("setup created an account without the disclaimer being accepted")
	}
	if !strings.Contains(rr.Body.String(), "disclaimer") {
		t.Errorf("refusal does not mention the disclaimer:\n%s", rr.Body.String())
	}

	rr = l.action(t, "/api/setup", `{"setupCode":"7KQ4-M2XD","accepted":true,"username":"evgenii","name":"Evgenii","password":"short"}`, nil)
	if !l.accounts.NeedsSetup() || !strings.Contains(rr.Body.String(), "at least 10 characters") {
		t.Fatalf("a short password was accepted or not explained:\n%s", rr.Body.String())
	}

	rr = l.action(t, "/api/setup", `{"setupCode":"7KQ4-M2XD","accepted":true,"username":"evgenii","name":"Evgenii","password":"correct horse battery"}`, nil)
	cookie := sessionCookieFrom(rr)
	if l.accounts.NeedsSetup() || cookie == nil {
		t.Fatalf("setup did not create the admin and sign in:\n%s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "/settings/sources?welcome=1") {
		t.Errorf("setup does not continue to the Sources step:\n%s", rr.Body.String())
	}
	if page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources?welcome=1", nil), cookie); page.Code != http.StatusOK ||
		!strings.Contains(page.Body.String(), "themoviedb.org") {
		t.Errorf("Sources step = %d, want 200 with the TMDB key guide", page.Code)
	}

	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/setup", nil), nil); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("GET /setup after setup = %d → %q, want 303 → /login", rr.Code, rr.Header().Get("Location"))
	}
	l.action(t, "/api/setup", `{"setupCode":"7KQ4-M2XD","accepted":true,"username":"intruder","name":"X","password":"another long password"}`, nil)
	if l.accounts.Lookup("intruder") != nil {
		t.Error("a second setup created another admin")
	}
}

// Until the owner sets it up, a fresh install listens on the whole network:
// another device may only create the admin with the setup code the server
// printed to its log.
func TestOnlyThisMachineOrTheSetupCodeCreatesTheFirstAdmin(t *testing.T) {
	const code = "7KQ4-M2XD"
	from := func(addr string, headers ...string) func(*http.Request) {
		return func(r *http.Request) {
			r.RemoteAddr = addr
			for i := 0; i+1 < len(headers); i += 2 {
				r.Header.Set(headers[i], headers[i+1])
			}
		}
	}
	setup := func(l *local, signals string, at func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(signals))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Datastar-Request", "true")
		at(req)
		return l.do(t, req, nil)
	}
	const account = `"accepted":true,"username":"owner","password":"correct horse battery"`
	lan := from("192.168.1.66:50000")

	l := newLocal(t, nil, withSetupCode(code))
	page := httptest.NewRequest(http.MethodGet, "/setup", nil)
	lan(page)
	if body := l.do(t, page, nil).Body.String(); !strings.Contains(body, "Setup code") || !strings.Contains(body, "docker compose logs moviestracker") {
		t.Error("the setup page seen from another device does not ask for the setup code or say where to find it")
	}
	for _, try := range []struct {
		name    string
		signals string
		at      func(*http.Request)
	}{
		{"another device without the code", `{` + account + `}`, lan},
		{"another device with a wrong code", `{` + account + `,"setupCode":"AAAA-BBBB"}`, lan},
		{"a proxy on this machine forwarding another device", `{` + account + `}`, from("127.0.0.1:50000", "X-Forwarded-For", "192.168.1.66")},
	} {
		rr := setup(l, try.signals, try.at)
		if !l.accounts.NeedsSetup() || sessionCookieFrom(rr) != nil {
			t.Fatalf("%s created the admin", try.name)
		}
		if !strings.Contains(rr.Body.String(), "setup code") {
			t.Errorf("%s: the refusal does not mention the setup code:\n%s", try.name, rr.Body.String())
		}
	}
	if rr := setup(l, `{`+account+`,"setupCode":" 7kq4m2xd "}`, lan); l.accounts.NeedsSetup() || sessionCookieFrom(rr) == nil {
		t.Fatalf("the setup code (typed loosely) did not create the admin:\n%s", rr.Body.String())
	}

	l = newLocal(t, nil) // no code at all: only this machine can set up
	page = httptest.NewRequest(http.MethodGet, "/setup", nil)
	from("127.0.0.1:50000")(page)
	if strings.Contains(l.do(t, page, nil).Body.String(), "Setup code") {
		t.Error("the setup page asks this machine for a setup code")
	}
	if setup(l, `{`+account+`}`, lan); !l.accounts.NeedsSetup() {
		t.Fatal("another device set up an install that has no setup code")
	}
	if rr := setup(l, `{`+account+`}`, from("[::1]:50000")); l.accounts.NeedsSetup() || sessionCookieFrom(rr) == nil {
		t.Fatalf("this machine could not set up without a code:\n%s", rr.Body.String())
	}
}

// The IMDb rating service is Moviestracker's own: Sources offers only an
// on/off switch and never shows its address.
func TestIMDbRatingsAreSwitchedOnAndOffWithoutShowingTheServiceAddress(t *testing.T) {
	l := newLocal(t, withAdmin(t))
	admin := l.admin(t)
	for _, path := range []string{"/settings/sources?welcome=1", "/settings/sources"} {
		page := l.do(t, httptest.NewRequest(http.MethodGet, path, nil), admin).Body.String()
		if strings.Contains(page, config.DefaultIMDbURL) || strings.Contains(page, "imdbUrl") {
			t.Errorf("%s shows the rating service address", path)
		}
		if !strings.Contains(page, "Show IMDb ratings") {
			t.Errorf("%s lacks the IMDb switch", path)
		}
	}

	for _, on := range []bool{false, true} {
		rr := l.action(t, "/api/settings/sources/imdb", fmt.Sprintf(`{"imdbOn":%t}`, on), admin)
		if strings.Contains(rr.Body.String(), config.DefaultIMDbURL) {
			t.Error("saving the switch shows the rating service address")
		}
		if st := l.store.State().Sources; st.IMDbOff == on || st.IMDbURL != config.DefaultIMDbURL {
			t.Errorf("after switching ratings on=%t: off=%t, service %q; want the default service kept", on, st.IMDbOff, st.IMDbURL)
		}
	}
}

func TestOnlyAnAdminOpensSources(t *testing.T) {
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	})
	get := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		return l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), cookie)
	}
	if rr := get(nil); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("signed out: %d → %q, want 303 → /login", rr.Code, rr.Header().Get("Location"))
	}
	viewer := l.signIn(t, l.accounts.Lookup("kid"))
	if rr := get(viewer); rr.Code != http.StatusForbidden {
		t.Errorf("viewer: %d, want 403", rr.Code)
	}
	if rr := l.action(t, "/api/settings/sources/tmdb", `{"tmdbKey":"x"}`, viewer); rr.Code != http.StatusForbidden {
		t.Errorf("viewer saving a TMDB key: %d, want 403", rr.Code)
	}
	if rr := get(l.admin(t)); rr.Code != http.StatusOK {
		t.Errorf("admin: %d, want 200", rr.Code)
	}
}

func TestANewTMDBKeyIsCheckedSavedAndUsedAtOnce(t *testing.T) {
	var mu sync.Mutex
	var searchedWith []string
	fakeTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/configuration":
			if r.Header.Get("Authorization") != "Bearer good-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case "/3/search/multi":
			mu.Lock()
			searchedWith = append(searchedWith, r.Header.Get("Authorization"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"page":1,"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"page":1,"results":[]}`))
		}
	}))
	defer fakeTMDB.Close()
	l := newLocal(t, withAdmin(t), func(c *handlers.Config) { c.Connector = sources.Connector{TMDBBaseURL: fakeTMDB.URL} })
	admin := l.admin(t)

	rr := l.action(t, "/api/settings/sources/tmdb", `{"tmdbKey":"bad-token"}`, admin)
	if got := l.store.State().Sources.TMDBKey; got != "" || !strings.Contains(rr.Body.String(), "rejected") {
		t.Fatalf("rejected key: stored %q, response:\n%s", got, rr.Body.String())
	}

	rr = l.action(t, "/api/settings/sources/tmdb", `{"tmdbKey":" good-token "}`, admin)
	if got := l.store.State().Sources.TMDBKey; got != "good-token" {
		t.Fatalf("working key not stored (got %q), response:\n%s", got, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "good-token") {
		t.Error("the saved key is sent back to the browser")
	}

	l.do(t, httptest.NewRequest(http.MethodGet, "/search?q=dune", nil), admin)
	mu.Lock()
	defer mu.Unlock()
	if len(searchedWith) == 0 || searchedWith[len(searchedWith)-1] != "Bearer good-token" {
		t.Errorf("search after saving used %q, want the new key without a restart", searchedWith)
	}
}

func TestJacRedIsTestedBeforeItIsSaved(t *testing.T) {
	var apiKey string
	fakeJacRed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.URL.Query().Get("apikey")
		_, _ = w.Write([]byte(`[{"tracker":"rutor","url":"https://rutor.info/torrent/1","title":"Inception 2010","sid":12,"sizeName":"10 GB",` +
			`"magnet":"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=a","relased":2010,"quality":1080}]`))
	}))
	defer fakeJacRed.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	l := newLocal(t, withAdmin(t))
	admin := l.admin(t)

	rr := l.action(t, "/api/settings/sources/jacred", `{"jacredUrl":"`+gone.URL+`","jacredApiKey":""}`, admin)
	if got := l.store.State().Sources.JacRedURL; got != config.DefaultJacRedURL {
		t.Fatalf("an unreachable JacRed was saved (%q):\n%s", got, rr.Body.String())
	}

	rr = l.action(t, "/api/settings/sources/jacred", `{"jacredUrl":"`+fakeJacRed.URL+`/","jacredApiKey":"private"}`, admin)
	got := l.store.State().Sources
	if got.JacRedURL != fakeJacRed.URL || got.JacRedAPIKey != "private" || apiKey != "private" {
		t.Fatalf("working JacRed not saved: %+v (test search sent apikey %q):\n%s", got, apiKey, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "1 release") {
		t.Errorf("response does not report the test search:\n%s", rr.Body.String())
	}
}

func TestTheTorrServerAddressIsCheckedBeforeItIsUsed(t *testing.T) {
	fakeTorrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			_, _ = w.Write([]byte("MatriX.145"))
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeTorrServer.Close()
	l := newLocal(t, withAdmin(t))
	admin := l.admin(t)

	rr := l.action(t, "/api/settings/sources/torrserver", `{"torrserverUrl":"ftp://nas"}`, admin)
	if got := l.store.State().TorrServer.URL; got != config.DefaultTorrServerURL {
		t.Fatalf("an ftp address was saved (%q):\n%s", got, rr.Body.String())
	}

	rr = l.action(t, "/api/settings/sources/torrserver", `{"torrserverUrl":"`+fakeTorrServer.URL+`"}`, admin)
	if got := l.store.State().TorrServer.URL; got != fakeTorrServer.URL || !strings.Contains(rr.Body.String(), "MatriX.145") {
		t.Fatalf("working TorrServer not saved (%q):\n%s", got, rr.Body.String())
	}
	status := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/status", nil), admin)
	if !strings.Contains(status.Body.String(), "MatriX.145") {
		t.Errorf("TorrServer status after switching does not use the new address:\n%s", status.Body.String())
	}
}

func TestEnvironmentOverridesAreReadOnlyInSources(t *testing.T) {
	l := newLocal(t, withAdmin(t), func(c *handlers.Config) { c.Env = config.Env{TMDBKey: "from-env"} })
	admin := l.admin(t)
	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if !strings.Contains(page, "TMDB_API_KEY") {
		t.Errorf("Sources page does not say the TMDB key comes from TMDB_API_KEY")
	}
	if strings.Contains(page, "from-env") {
		t.Error("the TMDB key from the environment is rendered into the page")
	}
	if rr := l.action(t, "/api/settings/sources/tmdb", `{"tmdbKey":"other"}`, admin); rr.Code != http.StatusConflict {
		t.Errorf("saving an overridden key: %d, want 409", rr.Code)
	}
}

func TestRequestsForForeignHostnamesAreRefused(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := handlers.HostAllowlistMiddleware([]string{"media.home", "MacBook.local"}, ok)
	for host, want := range map[string]int{
		"192.168.1.20:8095":  http.StatusNoContent,
		"[fe80::1]:8095":     http.StatusNoContent,
		"localhost:8095":     http.StatusNoContent,
		"tv.localhost":       http.StatusNoContent,
		"media.home:8095":    http.StatusNoContent,
		"macbook.local":      http.StatusNoContent,
		"evil.example":       http.StatusMisdirectedRequest,
		"media.home.evil.io": http.StatusMisdirectedRequest,
	} {
		req := httptest.NewRequest(http.MethodGet, "/movies", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != want {
			t.Errorf("Host %q: %d, want %d", host, rr.Code, want)
		}
	}
}

func TestTheStreamProxyReachesAnEngineThatRequiresCredentials(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "moviestracker" || pass != "engine-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("video bytes"))
	}))
	defer engine.Close()
	mgr := torrserver.NewManager(engine.URL)
	if err := mgr.SetEndpoint(engine.URL, "moviestracker", "engine-secret"); err != nil {
		t.Fatalf("SetEndpoint(): %v", err)
	}
	l := newLocal(t, withAdmin(t), func(c *handlers.Config) { c.TorrServer = mgr })

	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/stream/movie.mkv?link="+duneHash+"&index=1&play", nil), l.admin(t))
	if rr.Code != http.StatusOK || rr.Body.String() != "video bytes" {
		t.Fatalf("proxied stream = %d %q, want 200 with the engine's bytes", rr.Code, rr.Body.String())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}
