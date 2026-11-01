package web_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestAUserRatesAMovieWhichMarksItWatched(t *testing.T) {
	h, session, users := favorites(t)
	if page := html.UnescapeString(getWith(t, h, "/movie/438631", session).Body.String()); !strings.Contains(page, "@get('/api/titles/review?type=movie&id=438631')") {
		t.Error("the movie page does not ask for the user's rating and note")
	}
	empty := html.UnescapeString(send(t, h, http.MethodGet, "/api/titles/review?type=movie&id=438631", session).Body.String())
	if !strings.Contains(empty, `id="title-review"`) || strings.Contains(empty, `of 10" checked`) {
		t.Errorf("an unrated movie's review section is:\n%s", empty)
	}
	res := sendSignals(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631", `{"reviewRating":9,"reviewNote":"See it in IMAX."}`, session)
	body := html.UnescapeString(res.Body.String())
	if res.Code != http.StatusOK || !strings.Contains(body, `aria-label="9 of 10" checked`) {
		t.Fatalf("rating Dune 9 = %d, want the stars patched with 9 ticked:\n%s", res.Code, body)
	}
	if !strings.Contains(body, `data-state="watched" aria-pressed="true"`) {
		t.Errorf("rating Dune did not show it watched:\n%s", body)
	}
	got, err := users.Review(context.Background(), "1098765", "movie", 438631)
	if err != nil || got.Rating != 9 || got.Note != "See it in IMAX." || got.Title != "Dune" || got.Poster != "/d.jpg" {
		t.Errorf("kept review = %+v, %v; want Dune rated 9 with the note, as TMDB names it", got, err)
	}
	if state, _ := users.TitleState(context.Background(), "1098765", "movie", 438631); state.State != store.Watched {
		t.Errorf("after rating, Dune is %q, want watched", state.State)
	}
}

func TestANoteAloneDoesNotMarkATitleWatched(t *testing.T) {
	h, session, users := favorites(t)
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want", session)
	body := html.UnescapeString(sendSignals(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631", `{"reviewRating":0,"reviewNote":"Anna recommends it"}`, session).Body.String())
	if strings.Contains(body, `id="watch-state"`) {
		t.Errorf("a note alone patched the watch state:\n%s", body)
	}
	if state, _ := users.TitleState(context.Background(), "1098765", "movie", 438631); state.State != store.WantToWatch {
		t.Errorf("after a note, Dune is %q, want still want to watch", state.State)
	}
	if got, _ := users.Review(context.Background(), "1098765", "movie", 438631); got.Note != "Anna recommends it" || got.Rating != 0 {
		t.Errorf("kept review = %+v", got)
	}
}

func TestARatingMustBeOneToTen(t *testing.T) {
	h, session, _ := favorites(t)
	if res := sendSignals(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631", `{"reviewRating":11,"reviewNote":""}`, session); res.Code != http.StatusBadRequest {
		t.Errorf("rating 11 = %d, want 400", res.Code)
	}
	if res := send(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631", httptest.NewRecorder()); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
}
