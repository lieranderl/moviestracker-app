package handlers

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestTonightCanPickFromMyWatchlist(t *testing.T) {
	details := discoverDetails()
	details.movies = map[int]*tmdb.MovieDetails{42: {MediaItem: tmdb.MediaItem{ID: 42, Title: "Saved film", MediaType: "movie", ReleaseDate: "2020-01-01", VoteAverage: 8}, Runtime: 88, Genres: []tmdb.Genre{{ID: 878}}}}
	s := newMediaServer(t, details, nil, "")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=42&state=want")
	page := html.UnescapeString(get(t, s, "/discover?source=watchlist&pick=1", true).Body.String())
	got := picks(page)
	if len(got) != 1 || !strings.Contains(got[0], "/movie/42") {
		t.Fatalf("watchlist picks = %v; want only saved film 42", got)
	}
}

func TestPinnedTonightCandidatesSurviveRerolls(t *testing.T) {
	_, _, getPage := tonightServer(t, 8)
	for _, draw := range []string{"1", "2", "3"} {
		page := getPage("/discover?pick=" + draw + "&pin=2&pin=2&pin=1&pin=99999&pin=-3&pin=oops")
		got := picks(page)
		if len(got) != 5 || !strings.Contains(got[0], "/movie/2") {
			t.Fatalf("draw %s = %v; want pinned film 2 first in five unique eligible picks", draw, got)
		}
		joined := strings.Join(got, " ")
		if strings.Contains(joined, `href="/movie/1"`) || strings.Contains(joined, "99999") {
			t.Fatal("ineligible pinned film entered shortlist")
		}
	}
}

func TestWatchlistPicksRespectFiltersAndNeverRelaxUnknownMetadata(t *testing.T) {
	details := discoverDetails()
	details.movies = map[int]*tmdb.MovieDetails{42: {MediaItem: tmdb.MediaItem{ID: 42, Title: "Saved film", MediaType: "movie", ReleaseDate: "2020-01-01", VoteAverage: 8}, Runtime: 88, OriginalLanguage: "ko", Genres: []tmdb.Genre{{ID: 878}}}}
	s := newMediaServer(t, details, nil, "")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=42&state=want")
	matching := "/discover?source=watchlist&pick=1&type=movie&genre=878&year=2020&rating=8&runtime=90&lang=ko"
	if got := picks(html.UnescapeString(get(t, s, matching, true).Body.String())); len(got) != 1 {
		t.Fatalf("matching saved film excluded: %v", got)
	}
	for _, query := range []string{"genre=28", "year=2021", "lang=fr", "type=tv"} {
		if got := picks(html.UnescapeString(get(t, s, "/discover?source=watchlist&pick=1&"+query, true).Body.String())); len(got) != 0 {
			t.Errorf("%s ignored: %v", query, got)
		}
	}
	details.movies[42].Runtime = 0
	if got := picks(html.UnescapeString(get(t, s, "/discover?source=watchlist&pick=1&runtime=90", true).Body.String())); len(got) != 0 {
		t.Error("unknown runtime treated as under 90 minutes")
	}
	delete(details.movies, 42)
	page := html.UnescapeString(get(t, s, "/discover?source=watchlist&pick=1", true).Body.String())
	if len(picks(page)) != 0 || !strings.Contains(page, "Your watchlist could not be loaded") {
		t.Fatal("failed metadata did not show failure")
	}
}

func TestTonightKeepsAtMostFiveEligiblePins(t *testing.T) {
	_, _, getPage := tonightServer(t, 8)
	page := getPage("/discover?pick=1&pin=2&pin=3&pin=4&pin=5&pin=6&pin=7")
	got := picks(page)
	if len(got) != 5 {
		t.Fatalf("pinned shortlist = %v; want exactly five", got)
	}
	for _, id := range []string{"2", "3", "4", "5", "6"} {
		if !strings.Contains(strings.Join(got, " "), `href="/movie/`+id+`"`) {
			t.Errorf("pin %s was lost", id)
		}
	}
	if strings.Contains(page, "pin=7") {
		t.Error("sixth pin retained in controls")
	}
}

func TestWatchlistPicksLimitMetadataWorkAndExplainTheWindow(t *testing.T) {
	details := discoverDetails()
	details.movies = map[int]*tmdb.MovieDetails{}
	clock := time.Date(2026, time.November, 10, 20, 0, 0, 0, time.UTC)
	saved := store.NewMemory(store.WithClock(func() time.Time { return clock }))
	for id := 51; id >= 1; id-- {
		if _, err := saved.ChangeTitleState(t.Context(), "viewer", store.TitleState{Kind: "movie", TMDBID: id, Title: fmt.Sprintf("Movie %d", id), State: store.WantToWatch, UpdatedAt: clock.Add(-time.Duration(id) * time.Minute)}, store.TitleState{}); err != nil {
			t.Fatal(err)
		}
		if id <= 50 {
			details.movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprintf("Movie %d", id), MediaType: "movie"}}
		}
	}
	catalog := NewCatalog(func() *sources.Clients { return &sources.Clients{Details: details} }, func(*http.Request) *auth.User { return &auth.User{Username: "viewer"} }, "/login", time.Second)
	catalog.PickFromWatchlist(saved, func(*http.Request) (string, bool) { return "viewer", true })
	mux := http.NewServeMux()
	catalog.Register(mux, "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discover?source=watchlist&pick=1&pin=51", nil))
	page := html.UnescapeString(rec.Body.String())
	if len(picks(page)) != 5 || strings.Contains(page, `href="/movie/51"`) || !strings.Contains(page, "Picks use your 50 most recently saved titles.") {
		t.Fatal("watchlist budget did not omit oldest metadata lookup and explain candidate window")
	}
}

func TestMoviePinsDoNotBecomeSeriesPinsWhenChangingType(t *testing.T) {
	details := discoverDetails()
	details.discovered = tmdb.Page{TotalPages: 1, Items: []tmdb.MediaItem{{ID: 42, Title: "Series 42", MediaType: "tv"}}}
	details.series = map[int]*tmdb.TVDetails{42: {MediaItem: tmdb.MediaItem{ID: 42, Title: "Series 42", MediaType: "tv"}}}
	server := newMediaServer(t, details, nil, "")
	page := html.UnescapeString(get(t, server, "/discover?type=tv&pick=1&pin=42&pinType=movie", true).Body.String())
	if !strings.Contains(page, `aria-label="Pin Series 42"`) || strings.Contains(page, `aria-label="Unpin Series 42"`) {
		t.Fatal("movie pin 42 was reinterpreted as series pin 42 after changing type")
	}
}
