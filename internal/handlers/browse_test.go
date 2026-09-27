package handlers

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestEveryHomeRowLinksToAllOfIt(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	body := get(t, server, "/movies", true).Body.String()

	for _, slug := range []string{"trending-movies", "trending-series", "now-playing", "popular-movies", "top-rated-movies", "popular-series", "top-rated-series"} {
		if !strings.Contains(body, `href="/browse/`+slug+`"`) {
			t.Errorf("home has no link to all of %s", slug)
		}
	}
}

func TestABrowsePageShowsTwentyTitlesAndLoadsMoreOnScroll(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{pages: map[tmdb.List]int{tmdb.PopularMovies: 3}}, &fakeJacRed{}, "")

	rec := get(t, server, "/browse/popular-movies", true)
	body := html.UnescapeString(rec.Body.String())

	if rec.Code != http.StatusOK || !strings.Contains(body, "Popular movies") {
		t.Fatalf("GET /browse/popular-movies = %d, want the Popular movies page", rec.Code)
	}
	for n := range 20 {
		if !strings.Contains(body, fmt.Sprintf(`href="/movie/%d"`, 100+n)) {
			t.Errorf("page lacks title %d of the first page", 100+n)
		}
	}
	if strings.Contains(body, `href="/movie/200"`) {
		t.Error("page shows the second page before scrolling")
	}
	if !strings.Contains(body, `data-on-intersect="@get('/api/browse/popular-movies?page=2')"`) {
		t.Error("page does not load page 2 on scroll")
	}
}

func TestScrollingAppendsTheNextTwentyTitlesUntilTheLastPage(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{pages: map[tmdb.List]int{tmdb.TopRatedSeries: 3}}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/api/browse/top-rated-series?page=2", true).Body.String())
	for _, want := range []string{
		"selector #browse-grid", "mode append", `href="/tv/200"`, `href="/tv/219"`,
		`data-on-intersect="@get('/api/browse/top-rated-series?page=3')"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page 2 lacks %q", want)
		}
	}

	last := html.UnescapeString(get(t, server, "/api/browse/top-rated-series?page=3", true).Body.String())
	if !strings.Contains(last, `href="/tv/300"`) || strings.Contains(last, "data-on-intersect") {
		t.Errorf("the last page should append its titles and stop loading:\n%s", last)
	}
}

// TMDB's pages shift while someone scrolls: a title already shown on the
// page before is not shown twice.
func TestATitleFromThePageBeforeIsNotShownAgain(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{
		pages: map[tmdb.List]int{tmdb.PopularMovies: 2},
		pageItems: map[tmdb.List]map[int][]tmdb.MediaItem{tmdb.PopularMovies: {
			1: {{ID: 1, Title: "Shifted", MediaType: "movie"}, {ID: 2, Title: "Stays", MediaType: "movie"}},
			2: {{ID: 1, Title: "Shifted", MediaType: "movie"}, {ID: 3, Title: "New", MediaType: "movie"}},
		}},
	}, &fakeJacRed{}, "")

	body := get(t, server, "/api/browse/popular-movies?page=2", true).Body.String()
	if strings.Contains(body, `href="/movie/1"`) || !strings.Contains(body, `href="/movie/3"`) {
		t.Errorf("page 2 should show only what page 1 did not:\n%s", body)
	}
}

func TestTrendingPagesSwitchBetweenThisWeekAndToday(t *testing.T) {
	fake := &fakeTMDBDetails{pages: map[tmdb.List]int{tmdb.TrendingSeriesWeek: 5, tmdb.TrendingSeriesDay: 5}}
	server := newMediaServer(t, fake, &fakeJacRed{}, "")

	week := html.UnescapeString(get(t, server, "/browse/trending-series", true).Body.String())
	day := html.UnescapeString(get(t, server, "/browse/trending-series?window=day", true).Body.String())
	get(t, server, "/api/browse/trending-series?window=day&page=2", true)

	for _, want := range []string{`href="/browse/trending-series?window=day"`, `@get('/api/browse/trending-series?page=2')`} {
		if !strings.Contains(week, want) {
			t.Errorf("this week's page lacks %q", want)
		}
	}
	for _, want := range []string{`href="/browse/trending-series"`, `@get('/api/browse/trending-series?window=day&page=2')`} {
		if !strings.Contains(day, want) {
			t.Errorf("today's page lacks %q", want)
		}
	}
	got := fake.listsAsked()
	for _, want := range []string{"trending/tv/week 1", "trending/tv/day 1", "trending/tv/day 2"} {
		if !slices.Contains(got, want) {
			t.Errorf("TMDB was asked %q, not %q", got, want)
		}
	}

	if other := html.UnescapeString(get(t, server, "/browse/popular-series?window=day", true).Body.String()); strings.Contains(other, "window=day") {
		t.Error("a list without trending windows offers the switch")
	}
}

func TestBrowsingNeedsSigningInAndAKnownListAndPage(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{pages: map[tmdb.List]int{tmdb.PopularMovies: 3}}, &fakeJacRed{}, "")

	for _, c := range []struct {
		path   string
		signed bool
		want   int
	}{
		{"/browse/popular-movies", false, http.StatusSeeOther},
		{"/api/browse/popular-movies?page=2", false, http.StatusUnauthorized},
		{"/browse/nope", true, http.StatusNotFound},
		{"/api/browse/nope?page=2", true, http.StatusNotFound},
		{"/api/browse/popular-movies?page=1", true, http.StatusBadRequest},
		{"/api/browse/popular-movies?page=501", true, http.StatusBadRequest},
		{"/api/browse/popular-movies?page=x", true, http.StatusBadRequest},
	} {
		if rec := get(t, server, c.path, c.signed); rec.Code != c.want {
			t.Errorf("GET %s (signed in %v) = %d, want %d", c.path, c.signed, rec.Code, c.want)
		}
	}
}

func TestABrowsePageSaysWhenTMDBFails(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{err: errors.New("tmdb down")}, &fakeJacRed{}, "")

	page := get(t, server, "/browse/popular-movies", true).Body.String()
	if !strings.Contains(page, "TMDB did not answer") || strings.Contains(page, "data-on-intersect") {
		t.Errorf("a failed first page should say so and not load more:\n%s", page)
	}
	more := html.UnescapeString(get(t, server, "/api/browse/popular-movies?page=2", true).Body.String())
	if !strings.Contains(more, "TMDB did not answer") || !strings.Contains(more, `@get('/api/browse/popular-movies?page=2')`) {
		t.Errorf("a failed next page should say so and offer to try again:\n%s", more)
	}
}
