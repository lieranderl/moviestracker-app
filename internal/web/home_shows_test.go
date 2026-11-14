package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func TestThisWeeksShowsBelongOnlyToTheSignedInAccount(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	users := store.NewMemory()
	cfg.Store = users
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, name := 95396, "Severance"
		if strings.HasSuffix(r.URL.Path, "/tv/3") {
			id, name = 3, "Silo"
		}
		_, _ = fmt.Fprintf(w, `{"id":%d,"name":%q,"next_episode_to_air":{"season_number":2,"episode_number":5,"air_date":%q}}`, id, name, tomorrow)
	}))
	t.Cleanup(fake.Close)
	cfg.Sources.Details = tmdb.NewClient("key", tmdb.WithBaseURL(fake.URL))
	h := web.New(cfg)
	session := signIn(t, h)
	for uid, series := range map[string]store.FollowedSeries{
		"1098765":         {TMDBID: 95396, Title: "Severance"},
		"another-account": {TMDBID: 3, Title: "Silo"},
	} {
		if err := users.Follow(context.Background(), uid, series, true); err != nil {
			t.Fatal(err)
		}
	}
	row := send(t, h, http.MethodGet, "/api/home/shows", session).Body.String()
	if !strings.Contains(row, `href="/tv/95396"`) || strings.Contains(row, `href="/tv/3"`) {
		t.Fatalf("this week's shows were not scoped to the signed-in account:\n%s", row)
	}
}
