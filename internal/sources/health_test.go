package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/sources"
)

func TestServiceHealthFollowsTheOutcomeOfRealCalls(t *testing.T) {
	var tmdbDown, jacredDown atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1.0/torrents" && jacredDown.Load(),
			r.URL.Path != "/api/v1.0/torrents" && tmdbDown.Load():
			w.WriteHeader(http.StatusBadGateway)
		case r.URL.Path == "/3/movie/404":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/api/v1.0/torrents":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","page":1,"results":[]}`))
		}
	}))
	defer fake.Close()

	health := sources.NewHealth()
	clients := sources.Connector{TMDBBaseURL: fake.URL, Health: health}.Connect(config.Sources{TMDBKey: "token", JacRedURL: fake.URL})
	ctx := context.Background()

	if got := health.Snapshot(); len(got) != 0 {
		t.Fatalf("health before any call = %+v", got)
	}
	_, _ = clients.Details.Movie(ctx, 603)
	_, _ = clients.Torrents.Search(ctx, jacred.Query{OriginalTitle: "The Matrix"})
	snap := health.Snapshot()
	if tm := snap["TMDB"]; !tm.OK || tm.LastCall.IsZero() || tm.Latency <= 0 || tm.Latency > time.Minute {
		t.Errorf("TMDB after a good call = %+v", tm)
	}
	if jr := snap["JacRed"]; !jr.OK {
		t.Errorf("JacRed after a good call = %+v", jr)
	}

	_, _ = clients.Details.Movie(ctx, 404) // a title TMDB does not have is not an outage
	if tm := health.Snapshot()["TMDB"]; !tm.OK {
		t.Errorf("TMDB after a 404 = %+v, want still OK", tm)
	}

	tmdbDown.Store(true)
	_, _ = clients.Details.Movie(ctx, 604)
	if tm := health.Snapshot()["TMDB"]; tm.OK || tm.Error == "" {
		t.Errorf("TMDB after a 502 = %+v, want the error", tm)
	}
	jacredDown.Store(true)
	_, _ = clients.Torrents.Search(ctx, jacred.Query{OriginalTitle: "Heat"})
	if jr := health.Snapshot()["JacRed"]; jr.OK {
		t.Errorf("JacRed after a 502 = %+v", jr)
	}
}

func TestHealthCountsCallsAndFailuresOfTheLastHour(t *testing.T) {
	var down atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix"}`))
	}))
	defer fake.Close()
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	health := sources.NewHealth(sources.WithClock(func() time.Time { return now }))
	clients := sources.Connector{TMDBBaseURL: fake.URL, Health: health}.Connect(config.Sources{TMDBKey: "token"})
	ctx := context.Background()

	for id := range 3 { // different titles: TMDB answers are cached
		_, _ = clients.Details.Movie(ctx, 600+id)
	}
	down.Store(true)
	_, _ = clients.Details.Movie(ctx, 700)
	if tm := health.Snapshot()["TMDB"]; tm.Calls != 4 || tm.Failures != 1 {
		t.Errorf("TMDB = %d calls, %d failures; want 4 and 1", tm.Calls, tm.Failures)
	}

	now = now.Add(61 * time.Minute)
	down.Store(false)
	_, _ = clients.Details.Movie(ctx, 701)
	if tm := health.Snapshot()["TMDB"]; tm.Calls != 1 || tm.Failures != 0 {
		t.Errorf("an hour later TMDB = %d calls, %d failures; want only the new call", tm.Calls, tm.Failures)
	}
}
