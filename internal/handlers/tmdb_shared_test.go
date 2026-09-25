package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
)

const sharedKey = "0123456789abcdef0123456789abcdef" // #nosec G101 -- a test fixture shaped like a v3 key

func TestTheSharedTMDBKeyWorksOutOfTheBoxAndIsNeverShown(t *testing.T) {
	var mu sync.Mutex
	var used []string // the key each search went out with
	fakeTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("api_key")
		if key == "" {
			key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if r.URL.Path == "/3/search/multi" {
			mu.Lock()
			used = append(used, key)
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"page":1,"results":[]}`))
	}))
	defer fakeTMDB.Close()
	l := newLocal(t, withAdmin(t), func(c *handlers.Config) {
		c.Connector = sources.Connector{TMDBBaseURL: fakeTMDB.URL}
		c.Env = config.Env{SharedTMDBKey: sharedKey}
		// As the server starts: clients from the sources in effect.
		clients := c.Connector.Connect(config.Sources{TMDBKey: c.Env.Apply(c.Store.State()).Sources.TMDBKey})
		c.TMDB, c.Details = clients.Catalog, clients.Details
	})
	admin := l.admin(t)
	lastKey := func() string {
		l.do(t, httptest.NewRequest(http.MethodGet, "/search?q=dune", nil), admin)
		mu.Lock()
		defer mu.Unlock()
		if len(used) == 0 {
			return ""
		}
		return used[len(used)-1]
	}

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if strings.Contains(page, sharedKey) {
		t.Fatal("the shared key is sent to the browser")
	}
	for _, want := range []string{"shared TMDB key", "your own key"} {
		if !strings.Contains(page, want) {
			t.Errorf("Sources page lacks %q", want)
		}
	}
	if got := lastKey(); got != sharedKey {
		t.Errorf("search with no key of one's own used %q, want the shared key", got)
	}

	l.action(t, "/api/settings/sources/tmdb", `{"tmdbKey":"good-token"}`, admin)
	if got := lastKey(); got != "good-token" {
		t.Errorf("search after saving an own key used %q", got)
	}
	page = l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if !strings.Contains(page, "/api/settings/sources/tmdb/shared") {
		t.Error("with an own key saved, the page should offer the shared key again")
	}

	rr := l.action(t, "/api/settings/sources/tmdb/shared", `{}`, admin)
	if got := l.store.State().Sources.TMDBKey; got != "" || strings.Contains(rr.Body.String(), sharedKey) {
		t.Errorf("switching back: stored key %q; response shows the shared key: %v", got, strings.Contains(rr.Body.String(), sharedKey))
	}
	if got := lastKey(); got != sharedKey {
		t.Errorf("search after switching back used %q, want the shared key", got)
	}
}
