package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// withTMDB is cfg with the catalog served by a stand-in for TMDB (the
// external boundary), which knows Dune and Severance and whose search finds
// Dune.
func withTMDB(t *testing.T, cfg web.Config) web.Config {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/movie/438631":
			_, _ = w.Write([]byte(`{"id":438631,"title":"Dune","original_title":"Dune","poster_path":"/d.jpg","release_date":"2021-09-15"}`))
			return
		case "/3/tv/95396":
			_, _ = w.Write([]byte(`{"id":95396,"name":"Severance","original_name":"Severance","poster_path":"/s.jpg","first_air_date":"2022-02-17","seasons":[{"season_number":1,"name":"Season 1","air_date":"2022-02-17","episode_count":9}]}`))
			return
		case "/3/movie/1":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status_code":34}`))
			return
		}
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
	for _, local := range []string{`href="/dashboard"`, `href="/movies"`, `href="/settings`} {
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

// withReleases is cfg with the backend's latest-releases feed holding Dune.
func withReleases(cfg web.Config) web.Config {
	cfg.Releases = releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.Latest: {{ID: 438631, Title: "Дюна", OriginalTitle: "Dune", PosterPath: "/d.jpg", FoundAt: now}},
	})
	return cfg
}

func TestTheHomeRowsShowTheLatestReleases(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withReleases(withTMDB(t, g.config())))
	session := signIn(t, h)
	rows := getWith(t, h, "/api/discover", session).Body.String()
	if !strings.Contains(rows, `id="discover-latest-releases"`) || !strings.Contains(rows, `href="/movie/438631"`) {
		t.Error("the home rows do not show the latest releases with Dune")
	}
	page := getWith(t, h, "/browse/latest-releases", session)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `href="/movie/438631"`) {
		t.Errorf("GET /browse/latest-releases = %d, want the whole list with Dune", page.Code)
	}
}

// countingFeeds is a stand-in for Firestore's feeds that counts its reads.
type countingFeeds struct {
	mu    sync.Mutex
	reads int
}

// Page counts the read and, like Firestore, takes a moment to answer.
func (c *countingFeeds) Page(context.Context, releases.Feed, int) (tmdb.Page, error) {
	c.mu.Lock()
	c.reads++
	c.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	return tmdb.Page{Page: 1, TotalPages: 1}, nil
}

func (c *countingFeeds) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func TestReleaseFeedsAreReadAtMostEveryFiveMinutes(t *testing.T) {
	g := newGoogle(t)
	feeds := &countingFeeds{}
	cfg := withTMDB(t, g.config())
	cfg.Releases = feeds
	clock := now
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)

	getWith(t, h, "/api/discover", session)
	first := feeds.count()
	clock = clock.Add(4 * time.Minute)
	getWith(t, h, "/api/discover", session)
	if got := feeds.count(); got != first {
		t.Errorf("feeds read %d times within five minutes, want %d", got, first)
	}
	clock = clock.Add(2 * time.Minute)
	getWith(t, h, "/api/discover", session)
	if got := feeds.count(); got != 2*first {
		t.Errorf("feeds read %d times after five minutes, want %d", got, 2*first)
	}
}

func TestManyHomePagesAtOnceReadEachFeedOnce(t *testing.T) {
	g := newGoogle(t)
	feeds := &countingFeeds{}
	cfg := withTMDB(t, g.config())
	cfg.Releases = feeds
	h := web.New(cfg)
	cookie := sessionCookie(signIn(t, h))

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/api/discover", nil)
			req.AddCookie(cookie)
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
	wg.Wait()
	if got := feeds.count(); got != 3 {
		t.Errorf("8 home pages at once read the feeds %d times, want 3 (once each)", got)
	}
}

func TestEverySignedInPageCanCheckTheVisitorsTorrServer(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withTMDB(t, g.config()))
	session := signIn(t, h)
	for _, path := range []string{"/", "/search?q=dune", "/favorites", "/movie/438631", "/torrserver"} {
		body := getWith(t, h, path, session).Body.String()
		if !strings.Contains(body, `id="ts-nav-status"`) {
			t.Errorf("%s has no TorrServer status in its navbar", path)
		}
		helpers, datastar := strings.Index(body, `src="/static/torrserver.js"`), strings.Index(body, `src="/static/datastar.js"`)
		if helpers < 0 || helpers > datastar {
			t.Errorf("%s loads torrserver.js at %d and datastar.js at %d: the navbar's check needs the helpers first", path, helpers, datastar)
		}
		if n := strings.Count(body, `src="/static/torrserver.js"`); n > 1 {
			t.Errorf("%s loads torrserver.js %d times", path, n)
		}
	}
}
