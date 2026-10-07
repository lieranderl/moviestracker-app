package handlers

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
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
