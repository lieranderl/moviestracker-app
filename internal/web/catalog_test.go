package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// withTMDB is cfg with the catalog served by a stand-in for TMDB (the
// external boundary), whose search finds Dune.
func withTMDB(t *testing.T, cfg web.Config) web.Config {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/3/search/multi" {
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"media_type":"movie","id":438631,"title":"Dune","original_title":"Dune","release_date":"2021-09-15"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[]}`))
	}))
	t.Cleanup(fake.Close)
	cfg.Sources = sources.Connector{TMDBBaseURL: fake.URL}.Connect(config.Sources{TMDBKey: "tmdb-key"})
	return cfg
}

func TestASignedInUserSearchesTheCatalog(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withTMDB(t, g.config()))
	page := getWith(t, h, "/search?q=dune", signIn(t, h))
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, `href="/movie/438631"`) {
		t.Fatalf("GET /search?q=dune = %d, want a page linking to Dune", page.Code)
	}
	for _, local := range []string{`href="/dashboard"`, `href="/torrserver"`, `href="/movies"`, `href="/settings`} {
		if strings.Contains(body, local) {
			t.Errorf("the web app's page links to the local app's %s", local)
		}
	}
}

func TestSignedOutVisitorsAreSentToSignIn(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withTMDB(t, g.config()))
	for _, path := range []string{"/search?q=dune", "/movie/438631", "/tv/95396", "/browse/trending-movies"} {
		res := get(t, h, path)
		if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/" {
			t.Errorf("GET %s signed out = %d to %q, want 303 to /", path, res.Code, res.Header().Get("Location"))
		}
	}
}
