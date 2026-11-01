package handlers

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func searchDetails() *fakeTMDBDetails {
	return &fakeTMDBDetails{search: map[string]*tmdb.SearchResults{
		"nolan": {
			Query:  "nolan",
			Movies: []tmdb.MediaItem{{ID: 27205, Title: "Inception", MediaType: "movie"}},
			Series: []tmdb.MediaItem{{ID: 1396, Title: "Breaking Bad", MediaType: "tv"}},
			People: []tmdb.PersonSummary{{ID: 525, Name: "Christopher Nolan", KnownForDepartment: "Directing", KnownFor: []string{"Inception"}}},
		},
	}}
}

func TestSearchPageGroupsMoviesSeriesAndPeople(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")

	rec := get(t, server, "/search?q=nolan", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		`value="nolan"`,
		"Movies", `href="/movie/27205"`,
		"TV series", `href="/tv/1396"`,
		"People", `href="/person/525"`, "Christopher Nolan",
		"data-on:input__debounce.300ms",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("search page missing %q", want)
		}
	}
}

// The rendered effect is the browser boundary: it must synchronize live input
// without growing history or discarding browser-owned state. Back/Forward and
// scroll restoration are also exercised in the browser.
func TestLiveSearchKeepsAShareableURLWithoutExtraHistoryEntries(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")
	body := html.UnescapeString(get(t, server, "/search?q=nolan", true).Body.String())
	for _, want := range []string{`data-effect=`, `history.replaceState(history.state,`, `url.searchParams.set('q', $q)`, `url.searchParams.delete('q')`} {
		if !strings.Contains(body, want) {
			t.Errorf("live search URL synchronization missing %q", want)
		}
	}
	if strings.Contains(body, "history.pushState") {
		t.Error("typing must replace the current history entry")
	}
}

func TestReturningToSearchDoesNotScrollBackToTheInput(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")
	body := html.UnescapeString(get(t, server, "/search?q=nolan", true).Body.String())
	if strings.Contains(body, " autofocus") {
		t.Error("native autofocus overrides restored search scroll")
	}
	if !strings.Contains(body, "el.focus({preventScroll: true})") {
		t.Error("initial search focus must preserve the browser scroll position")
	}
}

func TestLiveSearchPatchesResults(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")

	rec := get(t, server, "/api/search?datastar="+url.QueryEscape(`{"q":"nolan"}`), true)

	body := rec.Body.String()
	if !strings.Contains(body, "datastar-patch-elements") || !strings.Contains(body, `id="search-results"`) || !strings.Contains(body, "Christopher Nolan") {
		t.Errorf("expected search-results patch, got %q", body)
	}
}

func TestSearchWithNoMatchesSaysSo(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")

	body := get(t, server, "/search?q=zzzz", true).Body.String()

	if !strings.Contains(body, "No matches for") {
		t.Errorf("expected empty state")
	}
}

func TestBlankSearchOffersTrendingDiscovery(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.Details = searchDetails()
	cfg.TMDB = &mockTrendingProvider{
		movies: []tmdb.MediaItem{{ID: 603, Title: "The Matrix", MediaType: "movie"}},
		series: []tmdb.MediaItem{{ID: 1399, Title: "Game of Thrones", MediaType: "tv"}},
	}
	server := newTestServerWithConfig(t, cfg)

	body := get(t, server, "/search", true).Body.String()

	for _, want := range []string{"Trending movies", `href="/movie/603"`, "Trending series", `href="/tv/1399"`} {
		if !strings.Contains(body, want) {
			t.Errorf("discovery missing %q", want)
		}
	}
}

func TestSearchRequiresSignIn(t *testing.T) {
	server := newMediaServer(t, searchDetails(), &fakeJacRed{}, "")

	if rec := get(t, server, "/search?q=nolan", false); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if rec := get(t, server, "/api/search", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api status = %d, want 401", rec.Code)
	}
}
