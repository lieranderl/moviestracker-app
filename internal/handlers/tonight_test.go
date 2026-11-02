package handlers

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// tonightServer is a local app whose user watched movie 1; Discover finds
// movies 1 to 8, fewer than 2 hours long and in Korean.
func tonightServer(t *testing.T, found int) (*Server, *fakeTMDBDetails, func(path string) string) {
	t.Helper()
	details := &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{1: {MediaItem: tmdb.MediaItem{ID: 1, Title: "Movie 1", MediaType: "movie"}}}}
	for id := 1; id <= found; id++ {
		details.discovered.Items = append(details.discovered.Items, tmdb.MediaItem{ID: id, Title: fmt.Sprint("Movie ", id), MediaType: "movie"})
	}
	details.discovered.TotalPages = 1
	s := newMediaServer(t, details, nil, "")
	cookie := signedIn(t, s)
	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Datastar-Request", "true")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return html.UnescapeString(rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/titles/state?type=movie&id=1&state=watched", nil)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(cookie)
	s.ServeHTTP(httptest.NewRecorder(), req)
	return s, details, get
}

// picks are the titles of the page's shortlist.
func picks(page string) []string {
	section := regexp.MustCompile(`(?s)<section id="tonight-picks".*?</section>`).FindString(page)
	return regexp.MustCompile(`href="/movie/(\d+)"`).FindAllString(section, -1)
}

// Picking for tonight shortlists a few matching titles the user has not
// marked, keeps every chosen filter, and rerolls to another shortlist.
func TestPickingForTonightShortlistsUnwatchedMatches(t *testing.T) {
	_, details, get := tonightServer(t, 8)
	page := get("/discover?type=movie&runtime=120&lang=ko&pick=1")
	first := picks(page)
	if len(first) != 5 || strings.Contains(strings.Join(first, " "), `"/movie/1"`) {
		t.Errorf("shortlist = %v, want 5 titles and never the watched movie 1", first)
	}
	if asked := strings.Join(details.discover, "\n"); !strings.Contains(asked, "MaxRuntime:120") || !strings.Contains(asked, "Language:ko") {
		t.Errorf("Discover was not asked with the chosen filters: %s", asked)
	}
	reroll := regexp.MustCompile(`href="(/discover\?[^"]*pick=2[^"]*)"`).FindStringSubmatch(page)
	if reroll == nil || !strings.Contains(reroll[1], "runtime=120") || !strings.Contains(reroll[1], "lang=ko") {
		t.Fatalf("no reroll keeping the filters: %v", reroll)
	}
	if again := picks(get(reroll[1])); strings.Join(again, " ") == strings.Join(first, " ") {
		t.Error("rerolling gave the same shortlist")
	}
}

// With nothing matching, the page says so and offers to drop each filter
// in turn, instead of quietly ignoring one.
func TestNoPickForTonightOffersToRelaxEachFilter(t *testing.T) {
	_, _, get := tonightServer(t, 1) // only the watched movie matches
	page := get("/discover?type=movie&runtime=90&lang=ko&pick=1")
	if len(picks(page)) != 0 || !strings.Contains(page, "Nothing matches all of that") {
		t.Fatal("an empty shortlist is not explained")
	}
	for _, drop := range []string{"runtime=90", "lang=ko"} {
		found := false
		for _, m := range regexp.MustCompile(`href="(/discover\?[^"]*pick=1[^"]*)"`).FindAllStringSubmatch(page, -1) {
			if !strings.Contains(m[1], drop) {
				found = true
			}
		}
		if !found {
			t.Errorf("no link drops %s", drop)
		}
	}
}
