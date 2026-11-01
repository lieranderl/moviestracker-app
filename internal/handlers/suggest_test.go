package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestTheLocalNavbarSuggestsMoviesSeriesAndPeople(t *testing.T) {
	details := &fakeTMDBDetails{search: map[string]*tmdb.SearchResults{"villeneuve": {
		Query:  "villeneuve",
		Movies: []tmdb.MediaItem{{ID: 438631, Title: "Dune", MediaType: "movie"}, {ID: 329865, Title: "Arrival", MediaType: "movie"}, {ID: 335984, Title: "Blade Runner 2049", MediaType: "movie"}, {ID: 27205, Title: "Inception", MediaType: "movie"}},
		Series: []tmdb.MediaItem{{ID: 95396, Title: "Severance", MediaType: "tv"}},
		People: []tmdb.PersonSummary{{ID: 137427, Name: "Denis Villeneuve", KnownForDepartment: "Directing"}},
	}}}
	s := newMediaServer(t, details, nil, "")
	body := sendAs(t, s, http.MethodGet, "/api/search/suggest?datastar="+url.QueryEscape(`{"navQ":"villeneuve"}`))
	for _, want := range []string{`href="/movie/438631"`, `href="/tv/95396"`, `href="/person/137427"`, "Denis Villeneuve", `href="/search?q=villeneuve"`} {
		if !strings.Contains(body, want) {
			t.Errorf("suggestions lack %q", want)
		}
	}
	if got := strings.Count(body, `class="suggestion`); got != 6 {
		t.Errorf("%d suggestions, want 6: three movies, a series and a person, then the next movie", got)
	}
}
