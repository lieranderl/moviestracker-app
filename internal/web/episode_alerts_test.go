package web_test

import (
	"context"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// A followed series' episode airing brings an alert, once; an episode that
// aired before the user followed the series does not.
func TestAnEpisodeAiringAlertsItsFollowers(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	clock := now.Add(-48 * time.Hour) // followed two days before S01E09 airs
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	send(t, h, http.MethodPost, "/api/follow?id=95396", session)
	clock = now // the day it airs
	if got := bellCount(t, h, session); got != "1" {
		t.Fatalf("after S01E09 aired the bell shows %q, want 1", got)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	for _, want := range []string{"Severance", "S01E09", `href="/tv/95396"`, "Episode aired"} {
		if !strings.Contains(page, want) {
			t.Errorf("the alerts page lacks %q", want)
		}
	}
	clock = clock.Add(2 * time.Hour)
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("the same episode alerted again (bell %q)", got)
	}
}

func TestAnEpisodeThatAiredBeforeFollowingBringsNoAlert(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	clock := now.Add(24 * time.Hour) // followed the day after S01E09 aired
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	send(t, h, http.MethodPost, "/api/follow?id=95396", session)
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("an episode aired before following alerted (bell %q)", got)
	}
}

// A failed lookup cannot consume an episode that aired after the last successful check.
func TestEpisodeAlertsCatchUpAfterAProviderFailure(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	clock := now.Add(-72 * time.Hour)
	users := store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Store = users
	cfg.Now = func() time.Time { return clock }
	var down atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":95396,"name":"Severance","last_episode_to_air":{"season_number":1,"episode_number":9,"air_date":"2026-09-29"}}`))
	}))
	t.Cleanup(fake.Close)
	cfg.Sources.Details = tmdb.NewClient("key", tmdb.WithBaseURL(fake.URL), tmdb.WithClock(func() time.Time { return clock }))
	h := web.New(cfg)
	session := signIn(t, h)
	if err := users.Follow(context.Background(), "1098765", store.FollowedSeries{TMDBID: 95396, Title: "Severance"}, true); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(-48 * time.Hour)
	if got := bellCount(t, h, session); got != "" {
		t.Fatalf("before airing bell = %q", got)
	}
	clock = now
	down.Store(true)
	if got := bellCount(t, h, session); got != "" {
		t.Fatalf("during failure bell = %q", got)
	}
	clock = now.Add(24 * time.Hour)
	down.Store(false)
	if got := bellCount(t, h, session); got != "1" {
		t.Fatalf("after recovery bell = %q, want the missed episode", got)
	}
}
