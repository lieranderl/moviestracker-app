package handlers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// titlesServer is a local app whose TMDB knows Dune.
func titlesServer(t *testing.T) *Server {
	t.Helper()
	details := &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{
		438631: {MediaItem: tmdb.MediaItem{ID: 438631, Title: "Dune", MediaType: "movie", PosterPath: "/d.jpg"}},
	}}
	return newMediaServer(t, details, nil, "")
}

func sendAs(t *testing.T, s *Server, method, path string) string {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(signedIn(t, s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	return html.UnescapeString(rec.Body.String())
}

func TestAnAccountMarksAMovieWatchedInTheLocalApp(t *testing.T) {
	s := titlesServer(t)
	if page := sendAs(t, s, http.MethodGet, "/movie/438631"); !strings.Contains(page, "@get('/api/titles/state?type=movie&id=438631')") {
		t.Error("the local movie page does not ask for its watch state")
	}
	if body := sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=watched"); !strings.Contains(body, `data-state="watched" aria-pressed="true"`) {
		t.Errorf("marking Dune watched answered:\n%s", body)
	}
	kept, err := store.OpenTitleFile(filepath.Join(s.store.Dir(), "titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kept.TitleState(context.Background(), testUsername, "movie", 438631); got.State != store.Watched || got.Title != "Dune" {
		t.Errorf("kept in the data directory: %+v, want Dune watched", got)
	}
}

func TestTheLocalAppListsWhatAnAccountWantsToWatch(t *testing.T) {
	s := titlesServer(t)
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want")
	page := sendAs(t, s, http.MethodGet, "/lists/want")
	for _, want := range []string{`href="/lists/want" role="tab" aria-selected="true"`, `href="/lists/watched"`, `href="/movie/438631"`} {
		if !strings.Contains(page, want) {
			t.Errorf("local Want to watch tab lacks %q", want)
		}
	}
	if strings.Contains(page, `href="/favorites"`) {
		t.Error("the local app offers favourites, which only the web app keeps")
	}
}

func TestFinishingAMovieInTheLocalPlayerMarksItWatched(t *testing.T) {
	s := progressServer(t)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":9300,"duration":9360.2}`, true)
	kept, err := store.OpenTitleFile(filepath.Join(s.store.Dir(), "titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kept.TitleState(context.Background(), testUsername, "movie", 438631); got.State != store.Watched || got.Title != "Dune" {
		t.Errorf("after finishing Dune its state is %+v, want watched", got)
	}
}

func TestFinishingAnEpisodeDoesNotMarkTheSeriesWatched(t *testing.T) {
	s := progressServer(t)
	s.rememberTitle(duneHash, "tv", 95396)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":3200,"duration":3300}`, true)
	kept, err := store.OpenTitleFile(filepath.Join(s.store.Dir(), "titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := kept.TitleState(context.Background(), testUsername, "tv", 95396); got.State != "" {
		t.Errorf("after one episode the series is %q, want unmarked", got.State)
	}
}

// A long watch list shows 60 titles a page, with a link to the rest.
func TestALongWatchListComesAPageAtATime(t *testing.T) {
	movies := map[int]*tmdb.MovieDetails{}
	for id := 1; id <= 70; id++ {
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprint("Movie ", id), MediaType: "movie"}}
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies}, nil, "")
	cookie := signedIn(t, s) // one session: the local app caps them
	send := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Datastar-Request", "true")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	for id := 1; id <= 70; id++ {
		send(http.MethodPost, fmt.Sprintf("/api/titles/state?type=movie&id=%d&state=watched", id))
	}
	page := html.UnescapeString(send(http.MethodGet, "/lists/watched").Body.String())
	if got := strings.Count(page, `href="/movie/`); got != 60 {
		t.Errorf("the first page shows %d titles, want 60", got)
	}
	more := regexp.MustCompile(`href="(/lists/watched\?after=[^"]+)"`).FindStringSubmatch(page)
	if more == nil || !strings.Contains(page, "Show more") {
		t.Fatal("no link to the rest of the list")
	}
	rest := send(http.MethodGet, more[1]).Body.String()
	if got := strings.Count(rest, `href="/movie/`); got != 10 || strings.Contains(rest, "after=") {
		t.Errorf("the second page shows %d titles (want 10) or links further", got)
	}
	if rec := send(http.MethodGet, "/lists/watched?after=nonsense"); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed page link = %d, want 400", rec.Code)
	}
}

// A watch list can be searched, narrowed to movies or series, and sorted by
// title or by the user's rating (unrated last), in a shareable address.
func TestAWatchListIsSearchedFilteredAndSorted(t *testing.T) {
	movies := map[int]*tmdb.MovieDetails{
		1: {MediaItem: tmdb.MediaItem{ID: 1, Title: "Dune", MediaType: "movie"}},
		2: {MediaItem: tmdb.MediaItem{ID: 2, Title: "Arrival", MediaType: "movie"}},
	}
	series := map[int]*tmdb.TVDetails{3: {MediaItem: tmdb.MediaItem{ID: 3, Title: "Severance", MediaType: "tv"}}}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies, series: series}, nil, "")
	cookie := signedIn(t, s)
	send := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Datastar-Request", "true")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
		}
		return html.UnescapeString(rec.Body.String())
	}
	for _, p := range []string{"/api/titles/state?type=movie&id=1&state=watched", "/api/titles/state?type=movie&id=2&state=watched", "/api/titles/state?type=tv&id=3&state=watched"} {
		send(http.MethodPost, p, "")
	}
	send(http.MethodPost, "/api/titles/review?type=movie&id=2", `{"reviewRating":8}`)
	send(http.MethodPost, "/api/titles/review?type=tv&id=3", `{"reviewRating":9}`)
	order := func(page string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`href="/(movie|tv)/(\d+)"`).FindAllStringSubmatch(page, -1) {
			out = append(out, m[1]+"/"+m[2])
		}
		return out
	}
	cases := []struct {
		query string
		want  []string
		count string
	}{
		{"?sort=title", []string{"movie/2", "movie/1", "tv/3"}, "3 titles"},
		{"?sort=rating", []string{"tv/3", "movie/2", "movie/1"}, "3 titles"},
		{"?q=dun", []string{"movie/1"}, "1 title"},
		{"?kind=tv", []string{"tv/3"}, "1 title"},
		{"?kind=movie&sort=title", []string{"movie/2", "movie/1"}, "2 titles"},
	}
	for _, c := range cases {
		page := send(http.MethodGet, "/lists/watched"+c.query, "")
		if got := order(page); !slices.Equal(got, c.want) {
			t.Errorf("%s shows %v, want %v", c.query, got, c.want)
		}
		if !strings.Contains(page, c.count) {
			t.Errorf("%s does not say %q", c.query, c.count)
		}
	}
	page := send(http.MethodGet, "/lists/watched?q=dun&kind=movie&sort=rating", "")
	for _, want := range []string{`name="q" value="dun"`, `<option value="movie" selected>`, `<option value="rating" selected>`, `method="get"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the controls do not keep the query: lack %q", want)
		}
	}
}

// A long search is cut to 100 characters, never inside one.
func TestALongSearchIsCutByCharacters(t *testing.T) {
	s := titlesServer(t)
	long := strings.Repeat("д", 150)
	page := sendAs(t, s, http.MethodGet, "/lists/watched?q="+url.QueryEscape(long))
	if !strings.Contains(page, `name="q" value="`+strings.Repeat("д", 100)+`"`) {
		t.Error("a 150-character search is not kept as its first 100 characters")
	}
	if strings.ContainsRune(page, utf8.RuneError) {
		t.Error("the cut search splits a character")
	}
}

func TestLibrarySearchKeepsUnicodeCharactersWithinTheInputLimit(t *testing.T) {
	title := strings.Repeat("Ж", 49) + "星"
	s := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{1: {MediaItem: tmdb.MediaItem{ID: 1, Title: title, MediaType: "movie"}}}}, nil, "")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=1&state=watched")
	page := html.UnescapeString(sendAs(t, s, http.MethodGet, "/lists/watched?q="+url.QueryEscape(title)))
	if !strings.Contains(page, `name="q" value="`+title+`"`) || !strings.Contains(page, `href="/movie/1"`) {
		t.Error("a query within 100 characters was corrupted or excluded its matching title")
	}
}
