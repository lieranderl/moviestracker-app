package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestAnAccountRatesAMovieInTheLocalApp(t *testing.T) {
	s := titlesServer(t)
	if page := sendAs(t, s, http.MethodGet, "/movie/438631"); !strings.Contains(page, "@get('/api/titles/review?type=movie&id=438631')") {
		t.Error("the local movie page does not ask for the account's rating")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/titles/review?type=movie&id=438631", strings.NewReader(`{"reviewRating":7,"reviewNote":"Long but good"}`))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signedIn(t, s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rating Dune = %d %s", rec.Code, rec.Body)
	}
	reviews, err := store.OpenReviewFile(filepath.Join(s.store.Dir(), "reviews.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reviews.Review(context.Background(), testUsername, "movie", 438631); got.Rating != 7 || got.Note != "Long but good" {
		t.Errorf("kept in the data directory: %+v, want Dune rated 7 with the note", got)
	}
	titles, err := store.OpenTitleFile(filepath.Join(s.store.Dir(), "titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := titles.TitleState(context.Background(), testUsername, "movie", 438631); got.State != store.Watched {
		t.Errorf("after rating, Dune is %q, want watched", got.State)
	}
}
