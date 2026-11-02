package store_test

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachTitleStore runs check against every store that keeps watch states:
// the web app's stores and the local app's file in its data directory.
func eachTitleStore(t *testing.T, check func(t *testing.T, s store.TitleStateStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenTitleFile(filepath.Join(t.TempDir(), "titles.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "user-"+rand.Text(), &clock)
	})
}

var severance = store.TitleState{Kind: "tv", TMDBID: 95396, Title: "Severance", Poster: "/s.jpg"}

func TestAUserMarksATitleTheyWantToWatch(t *testing.T) {
	eachTitleStore(t, func(t *testing.T, s store.TitleStateStore, uid string, _ *time.Time) {
		ctx := context.Background()
		want := severance
		want.State = store.WantToWatch
		if err := s.SetTitleState(ctx, uid, want); err != nil {
			t.Fatal(err)
		}
		got, err := s.TitleState(ctx, uid, "tv", 95396)
		if err != nil || got.State != store.WantToWatch || got.Title != "Severance" || !got.UpdatedAt.Equal(now) {
			t.Errorf("TitleState() = %+v, %v; want Severance to watch, marked now", got, err)
		}
	})
}

func TestWatchingATitleTakesItOffWantToWatch(t *testing.T) {
	eachTitleStore(t, func(t *testing.T, s store.TitleStateStore, uid string, clock *time.Time) {
		ctx := context.Background()
		dune := store.TitleState{Kind: "movie", TMDBID: 438631, Title: "Dune", State: store.WantToWatch}
		arrival := store.TitleState{Kind: "movie", TMDBID: 329865, Title: "Arrival", State: store.WantToWatch}
		watched := severance
		watched.State = store.WantToWatch
		for _, ts := range []store.TitleState{dune, arrival, watched} {
			if err := s.SetTitleState(ctx, uid, ts); err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(time.Minute)
		}
		watched.State = store.Watched
		if err := s.SetTitleState(ctx, uid, watched); err != nil {
			t.Fatal(err)
		}
		titles := func(state string) []string {
			list, err := s.TitleStates(ctx, uid, state)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, ts := range list {
				out = append(out, ts.Title)
			}
			return out
		}
		if got := titles(store.WantToWatch); len(got) != 2 || got[0] != "Arrival" || got[1] != "Dune" {
			t.Errorf("want to watch = %q, want [Arrival Dune], newest first", got)
		}
		if got := titles(store.Watched); len(got) != 1 || got[0] != "Severance" {
			t.Errorf("watched = %q, want [Severance]", got)
		}
	})
}

func TestAUserClearsATitlesState(t *testing.T) {
	eachTitleStore(t, func(t *testing.T, s store.TitleStateStore, uid string, _ *time.Time) {
		ctx := context.Background()
		watched := severance
		watched.State = store.Watched
		if err := s.SetTitleState(ctx, uid, watched); err != nil {
			t.Fatal(err)
		}
		watched.State = ""
		if err := s.SetTitleState(ctx, uid, watched); err != nil {
			t.Fatal(err)
		}
		if got, err := s.TitleState(ctx, uid, "tv", 95396); err != nil || got.State != "" {
			t.Errorf("TitleState() after clearing = %+v, %v; want none", got, err)
		}
		if list, _ := s.TitleStates(ctx, uid, store.Watched); len(list) != 0 {
			t.Errorf("watched after clearing = %+v, want none", list)
		}
		if err := s.SetTitleState(ctx, uid, store.TitleState{Kind: "tv", TMDBID: 95396, State: "loved"}); err == nil {
			t.Error("an unknown state was kept")
		}
	})
}

// Titles marked at the same moment (a coarse clock) still come in one
// order every time: by key, descending.
func TestTitlesMarkedAtTheSameMomentKeepOneOrder(t *testing.T) {
	eachTitleStore(t, func(t *testing.T, s store.TitleStateStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for _, id := range []int{2, 3, 1} {
			if err := s.SetTitleState(ctx, uid, store.TitleState{Kind: "movie", TMDBID: id, Title: "Movie", State: store.Watched}); err != nil {
				t.Fatal(err)
			}
		}
		for range 5 {
			got, err := s.TitleStates(ctx, uid, store.Watched)
			if err != nil || len(got) != 3 || got[0].TMDBID != 3 || got[1].TMDBID != 2 || got[2].TMDBID != 1 {
				t.Fatalf("TitleStates() = %+v, %v; want movies 3, 2, 1", got, err)
			}
		}
	})
}
