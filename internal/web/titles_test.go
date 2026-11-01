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

func TestMyListsShowFavouritesWantToWatchAndWatchedAsTabs(t *testing.T) {
	h, session, _ := favorites(t)
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want", session)
	page := html.UnescapeString(getWith(t, h, "/lists/want", session).Body.String())
	for _, want := range []string{`role="tablist"`, `href="/favorites"`, `href="/lists/want" role="tab" aria-selected="true"`, `href="/lists/watched"`, `href="/movie/438631"`, "My lists"} {
		if !strings.Contains(page, want) {
			t.Errorf("Want to watch tab lacks %q", want)
		}
	}
	if page := getWith(t, h, "/lists/watched", session).Body.String(); !strings.Contains(page, "Nothing watched yet") {
		t.Error("an empty Watched tab does not say so")
	}
	if page := html.UnescapeString(getWith(t, h, "/favorites", session).Body.String()); !strings.Contains(page, `href="/favorites" role="tab" aria-selected="true"`) {
		t.Error("the favourites page is not the lists' first tab")
	}
	if res := getWith(t, h, "/lists/loved", session); res.Code != http.StatusNotFound {
		t.Errorf("/lists/loved = %d, want 404", res.Code)
	}
}

func TestFinishingAMovieInThePlayerMarksItWatched(t *testing.T) {
	h, session, users := favorites(t)
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want", session)
	finished := strings.Replace(duneProgress, `"position":2530.4`, `"position":9300`, 1)
	if res := postPlayer(t, h, "/api/progress", finished, session); res.Code != http.StatusNoContent {
		t.Fatalf("saving the end of Dune = %d %s", res.Code, res.Body)
	}
	got, _ := users.TitleState(context.Background(), "1098765", "movie", 438631)
	if got.State != store.Watched || got.Title != "Dune" || got.Poster != "/d.jpg" {
		t.Errorf("after finishing Dune its state is %+v, want watched, as TMDB names it", got)
	}
}
