package handlers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
