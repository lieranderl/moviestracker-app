package web_test

import (
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebAccountCanConfirmAnEpisodeWithoutChangingHistory(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/tv/95396":
			_, _ = w.Write([]byte(`{"id":95396,"name":"Example series","seasons":[{"season_number":2,"episode_count":4}]}`))
		case "/3/tv/95396/season/2":
			_, _ = w.Write([]byte(`{"season_number":2,"episodes":[{"season_number":2,"episode_number":3}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	users := store.NewMemory()
	g := newGoogle(t)
	cfg := g.config()
	cfg.Store = users
	cfg.Sources = sources.Connector{TMDBBaseURL: api.URL}.Connect(config.Sources{TMDBKey: "fixture"})
	h := web.New(cfg)
	session := signIn(t, h)
	r := httptest.NewRequest(http.MethodPost, "/api/episodes/mapping", strings.NewReader("hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=1&file=episode.mkv&tv=95396&key=1&season=2&first=3&last=3&uid=someone-else"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Datastar-Request", "true")
	for _, c := range session.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("web confirmation=%d: %s", w.Code, w.Body)
	}
	got, found, err := users.EpisodeMapping(t.Context(), "1098765", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
	if err != nil || !found || got.First != 3 || got.Last != 3 {
		t.Fatalf("web owner confirmation=%+v %v %v", got, found, err)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "1098765", 95396); err != nil || len(got) != 0 {
		t.Fatalf("save changed history: %v %v", got, err)
	}
}
