package web_test

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestTonightWatchlistBelongsOnlyToTheSignedInAccount(t *testing.T) {
	h, session, users := favorites(t)
	for _, entry := range []struct {
		uid string
		id  int
	}{{"1098765", 438631}, {"another-account", 329865}} {
		if err := users.SetTitleState(t.Context(), entry.uid, store.TitleState{Kind: "movie", TMDBID: entry.id, Title: "Saved", State: store.WantToWatch}); err != nil {
			t.Fatal(err)
		}
	}
	page := getWith(t, h, "/discover?source=watchlist&pick=1&pin=329865", session).Body.String()
	if !strings.Contains(page, `href="/movie/438631"`) || strings.Contains(page, `href="/movie/329865"`) {
		t.Fatal("watchlist picks were not scoped to the signed-in account")
	}
}
