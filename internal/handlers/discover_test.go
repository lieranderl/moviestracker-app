package handlers

import (
	"errors"
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestHomeRendersListsThatLoadLazily(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movies", true).Body.String())

	for _, want := range []string{
		`data-init="@get('/api/discover')"`,
		`id="discover-now-playing"`, "In theaters",
		`id="discover-top-rated-movies"`,
		`id="discover-popular-series"`, `id="discover-top-rated-series"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home missing %q", want)
		}
	}
}

func TestDiscoverStreamsEachLazyListOfTheHomeRails(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{lists: map[tmdb.List][]tmdb.MediaItem{
		tmdb.NowPlayingMovies: {{ID: 1, Title: "In Theaters", MediaType: "movie"}},
		tmdb.PopularMovies:    {{ID: 2, Title: "Crowd Pleaser", MediaType: "movie"}},
		tmdb.TopRatedMovies:   {{ID: 3, Title: "Classic", MediaType: "movie"}},
		tmdb.PopularSeries:    {{ID: 4, Title: "Binge Show", MediaType: "tv"}},
		tmdb.TopRatedSeries:   {{ID: 5, Title: "Acclaimed Show", MediaType: "tv"}},
	}}, &fakeJacRed{}, "")

	body := get(t, server, "/api/discover", true).Body.String()

	if n := strings.Count(body, "event: datastar-patch-elements"); n != 4 {
		t.Errorf("patches = %d, want one per lazy list (4)", n)
	}
	for _, want := range []string{`href="/movie/1"`, `href="/movie/3"`, `href="/tv/4"`, `href="/tv/5"`} {
		if !strings.Contains(body, want) {
			t.Errorf("discover stream missing %q", want)
		}
	}
	if strings.Contains(body, `href="/movie/2"`) {
		t.Error("discover streams Popular movies, which home does not show")
	}
}

func TestDiscoverHidesTheTabOfAListThatFails(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{err: errors.New("tmdb down")}, &fakeJacRed{}, "")

	body := get(t, server, "/api/discover", true).Body.String()

	if !strings.Contains(body, `id="tab-discover-top-rated-movies" class="hidden"`) {
		t.Errorf("a failed list should hide its tab, got %q", body)
	}
	if !strings.Contains(body, `id="discover-top-rated-movies"`) || !strings.Contains(body, "Nothing here right now.") {
		t.Errorf("a failed list should stop loading and say it is empty, got %q", body)
	}
}

func TestDiscoverRequiresSignIn(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	if rec := get(t, server, "/api/discover", false); rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
