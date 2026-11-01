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

func TestAUserMarksAMovieTheyWantToWatchThenWatched(t *testing.T) {
	h, session, users := favorites(t)
	res := send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want", session)
	body := html.UnescapeString(res.Body.String())
	if res.Code != http.StatusOK || !strings.Contains(body, `id="watch-state"`) || !strings.Contains(body, `data-state="want" aria-pressed="true"`) {
		t.Fatalf("marking Dune = %d, want its buttons with Want to watch pressed:\n%s", res.Code, body)
	}
	got, err := users.TitleState(context.Background(), "1098765", "movie", 438631)
	if err != nil || got.State != store.WantToWatch || got.Title != "Dune" || got.Poster != "/d.jpg" {
		t.Errorf("Dune's state = %+v, %v; want to watch, as TMDB names it", got, err)
	}
	body = html.UnescapeString(send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=watched", session).Body.String())
	if !strings.Contains(body, `data-state="watched" aria-pressed="true"`) || !strings.Contains(body, `data-state="want" aria-pressed="false"`) {
		t.Errorf("after watching Dune the buttons are:\n%s", body)
	}
	send(t, h, http.MethodDelete, "/api/titles/state?type=movie&id=438631", session)
	if got, _ := users.TitleState(context.Background(), "1098765", "movie", 438631); got.State != "" {
		t.Errorf("after clearing, Dune is %q", got.State)
	}
}

func TestWatchStatesNeedASignedInUserAndAKnownState(t *testing.T) {
	h, session, _ := favorites(t)
	if res := send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want", httptest.NewRecorder()); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
	for _, q := range []string{"type=book&id=1&state=want", "type=movie&id=0&state=want", "type=movie&id=438631&state=loved"} {
		if res := send(t, h, http.MethodPost, "/api/titles/state?"+q, session); res.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", q, res.Code)
		}
	}
}

func TestATitlePageAsksForItsWatchState(t *testing.T) {
	h, session, _ := favorites(t)
	page := html.UnescapeString(getWith(t, h, "/movie/438631", session).Body.String())
	if !strings.Contains(page, "@get('/api/titles/state?type=movie&id=438631')") {
		t.Error("the movie page does not ask for its watch state")
	}
	body := html.UnescapeString(send(t, h, http.MethodGet, "/api/titles/state?type=movie&id=438631", session).Body.String())
	if !strings.Contains(body, `data-state="want" aria-pressed="false"`) || !strings.Contains(body, "Want to watch") || !strings.Contains(body, "Watched") {
		t.Errorf("an unmarked title's buttons are:\n%s", body)
	}
}
