package web_test

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestTheHomePageRecommendsTitlesLikeOnesTheUserWatched(t *testing.T) {
	h, session, users := favorites(t)
	if page := html.UnescapeString(getWith(t, h, "/", session).Body.String()); !strings.Contains(page, "@get('/api/recommendations')") {
		t.Fatal("the home page does not ask for recommendations")
	}
	if body := send(t, h, http.MethodGet, "/api/recommendations", session).Body.String(); strings.Contains(body, "Because you") {
		t.Errorf("a user who marked nothing gets recommendations:\n%s", body)
	}
	ctx := context.Background()
	if err := users.SetTitleState(ctx, "1098765", store.TitleState{Kind: "movie", TMDBID: 438631, Title: "Dune", State: store.Watched}); err != nil {
		t.Fatal(err)
	}
	body := html.UnescapeString(send(t, h, http.MethodGet, "/api/recommendations", session).Body.String())
	for _, want := range []string{`id="recommendations"`, "Because you watched Dune", `href="/movie/329865"`, `href="/movie/335984"`} {
		if !strings.Contains(body, want) {
			t.Errorf("recommendations lack %q:\n%s", want, body)
		}
	}
	if err := users.SetTitleState(ctx, "1098765", store.TitleState{Kind: "movie", TMDBID: 329865, Title: "Arrival", State: store.WantToWatch}); err != nil {
		t.Fatal(err)
	}
	if body := send(t, h, http.MethodGet, "/api/recommendations", session).Body.String(); strings.Contains(body, `href="/movie/329865"`) {
		t.Error("Arrival, already on Want to watch, is still recommended")
	}
}
