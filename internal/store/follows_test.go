package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachFollowStore runs check against every store that keeps followed
// series: the web app's stores and the local app's file.
func eachFollowStore(t *testing.T, check func(t *testing.T, s store.FollowStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenFollowFile(filepath.Join(t.TempDir(), "follows.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "alex", &clock)
	})
}

// A user follows series, newest first, and stops following one.
func TestAUserFollowsSeries(t *testing.T) {
	eachFollowStore(t, func(t *testing.T, s store.FollowStore, uid string, clock *time.Time) {
		ctx := context.Background()
		for _, f := range []store.FollowedSeries{{TMDBID: 95396, Title: "Severance"}, {TMDBID: 1396, Title: "Breaking Bad"}} {
			*clock = clock.Add(time.Minute)
			if err := s.Follow(ctx, uid, f, true); err != nil {
				t.Fatal(err)
			}
		}
		if on, _ := s.Following(ctx, uid, 95396); !on {
			t.Error("Severance is not followed")
		}
		got, err := s.FollowedSeries(ctx, uid)
		if err != nil || len(got) != 2 || got[0].Title != "Breaking Bad" || got[1].Title != "Severance" {
			t.Fatalf("FollowedSeries() = %+v, %v; want Breaking Bad then Severance", got, err)
		}
		if err := s.Follow(ctx, uid, store.FollowedSeries{TMDBID: 95396}, false); err != nil {
			t.Fatal(err)
		}
		if on, _ := s.Following(ctx, uid, 95396); on {
			t.Error("Severance is still followed")
		}
		if err := s.Follow(ctx, uid, store.FollowedSeries{TMDBID: 0}, true); err == nil {
			t.Error("a series without an ID was followed")
		}
		if other, _ := s.FollowedSeries(ctx, uid+"-other"); len(other) != 0 {
			t.Errorf("another user follows %+v", other)
		}
	})
}
