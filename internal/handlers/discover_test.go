package handlers

import (
	"errors"
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestHomeRendersDiscoveryRailsThatLoadLazily(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movies", true).Body.String())

	for _, want := range []string{
		`data-init="@get('/api/discover')"`,
		`id="discover-now-playing"`, "Now playing in theaters",
		`id="discover-popular-movies"`, `id="discover-top-rated-movies"`,
		`id="discover-popular-series"`, `id="discover-top-rated-series"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home missing %q", want)
		}
	}
}

func TestDiscoverStreamsEachRail(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{lists: map[tmdb.List][]tmdb.MediaItem{
		tmdb.NowPlayingMovies: {{ID: 1, Title: "In Theaters", MediaType: "movie"}},
		tmdb.PopularMovies:    {{ID: 2, Title: "Crowd Pleaser", MediaType: "movie"}},
		tmdb.TopRatedMovies:   {{ID: 3, Title: "Classic", MediaType: "movie"}},
		tmdb.PopularSeries:    {{ID: 4, Title: "Binge Show", MediaType: "tv"}},
		tmdb.TopRatedSeries:   {{ID: 5, Title: "Acclaimed Show", MediaType: "tv"}},
	}}, &fakeJacRed{}, "")

	body := get(t, server, "/api/discover", true).Body.String()

	if n := strings.Count(body, "event: datastar-patch-elements"); n != 5 {
		t.Errorf("patches = %d, want one per rail (5)", n)
	}
	for _, want := range []string{`href="/movie/1"`, `href="/movie/2"`, `href="/movie/3"`, `href="/tv/4"`, `href="/tv/5"`} {
		if !strings.Contains(body, want) {
			t.Errorf("discover stream missing %q", want)
		}
	}
}

func TestDiscoverHidesRailsThatFail(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{err: errors.New("tmdb down")}, &fakeJacRed{}, "")

	body := get(t, server, "/api/discover", true).Body.String()

	if !strings.Contains(body, `id="discover-popular-movies" class="hidden"`) {
		t.Errorf("failed rails should collapse, got %q", body)
	}
}

func TestDiscoverRequiresSignIn(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	if rec := get(t, server, "/api/discover", false); rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
