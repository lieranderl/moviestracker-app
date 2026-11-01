package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
