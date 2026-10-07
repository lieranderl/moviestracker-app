package handlers

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestTheNavbarShowsWhetherTorrServerAnswers(t *testing.T) {
	ts, _ := fakeTorrServer(t)
	online := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, ts.URL)

	page := html.UnescapeString(get(t, online, "/movie/27205", true).Body.String())
	for _, want := range []string{`id="ts-nav-status"`, `@get('/api/torrserver/state')`} {
		if !strings.Contains(page, want) {
			t.Errorf("the navbar lacks %q", want)
		}
	}
	if body := get(t, online, "/api/torrserver/state", true).Body.String(); !strings.Contains(body, `"_tsNav":"online"`) {
		t.Errorf("state of an answering TorrServer = %q, want online", body)
	}

	offline := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "http://127.0.0.1:1")
	if body := get(t, offline, "/api/torrserver/state", true).Body.String(); !strings.Contains(body, `"_tsNav":"offline"`) {
		t.Errorf("state of a silent TorrServer = %q, want offline", body)
	}
	if rec := get(t, offline, "/api/torrserver/state", false); rec.Code == 200 && strings.Contains(rec.Body.String(), "_tsNav") {
		t.Error("a signed-out visitor should not learn the TorrServer's state")
	}
}

func TestTheAppsHomeListsWhatWasAddedToTorrServerLast(t *testing.T) {
	// TorrServer, the external boundary, lists two torrents.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.136"))
		case "/torrents":
			_, _ = w.Write([]byte(`[{"hash":"a","title":"Older","timestamp":1,"stat":3},{"hash":"b","title":"Newer","timestamp":2,"stat":3}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	cfg := validTestConfig(t)
	cfg.TorrServer = torrserver.NewManager(ts.URL)
	server := newTestServerWithConfig(t, cfg)

	page := html.UnescapeString(get(t, server, "/movies", true).Body.String())
	if !strings.Contains(page, `id="trending-movies"`) {
		t.Fatal("the app's home did not render its rows")
	}
	if !strings.Contains(page, `id="recent-torrents"`) || !strings.Contains(page, "@get('/api/torrserver/recent')") {
		t.Error("the app's home has no row of what was added to TorrServer last, loaded from its server")
	}
	row := get(t, server, "/api/torrserver/recent", true).Body.String()
	if !strings.Contains(row, "Recently added to TorrServer") || strings.Index(row, "Newer") > strings.Index(row, "Older") {
		t.Errorf("the row does not list TorrServer's torrents newest first:\n%s", row)
	}
	if rec := get(t, server, "/api/torrserver/recent", false); strings.Contains(rec.Body.String(), "Newer") {
		t.Error("a signed-out visitor sees the TorrServer's torrents")
	}
}
