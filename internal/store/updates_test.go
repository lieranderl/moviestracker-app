package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

var duneInDV = store.Update{Feed: "dvmovies", TMDBID: 438631, Title: "Dune", Poster: "/d.jpg", FoundAt: now.Add(-2 * time.Hour)}

func TestAUserIsToldOnceAboutANewRelease(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := context.Background()
		arrival := store.Update{Feed: "hdr10movies", TMDBID: 329865, Title: "Arrival", FoundAt: now.Add(-time.Hour)}
		if err := s.AddUpdates(ctx, uid, []store.Update{duneInDV, arrival}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Updates(ctx, uid)
		if err != nil || len(got) != 2 || got[0].Title != "Arrival" || got[1].Title != "Dune" || got[0].Seen {
			t.Fatalf("Updates() = %+v, %v; want Arrival then Dune, unseen", got, err)
		}
		if err := s.MarkUpdatesSeen(ctx, uid); err != nil {
			t.Fatal(err)
		}
		// The backend finds Dune again: the user was already told.
		again := duneInDV
		again.FoundAt = now
		if err := s.AddUpdates(ctx, uid, []store.Update{again}); err != nil {
			t.Fatal(err)
		}
		got, _ = s.Updates(ctx, uid)
		for _, u := range got {
			if !u.Seen {
				t.Errorf("%s in %s is unseen again after the backend found it again", u.Title, u.Feed)
			}
		}
		if len(got) != 2 {
			t.Errorf("Updates() = %d, want the same 2", len(got))
		}
	})
}

func TestAnUpdateNeedsAKnownFeedAndMovie(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		for _, u := range []store.Update{{Feed: "rumours", TMDBID: 1}, {Feed: "dvmovies", TMDBID: 0}} {
			if err := s.AddUpdates(context.Background(), uid, []store.Update{u}); err == nil {
				t.Errorf("AddUpdates(%+v) kept it", u)
			}
		}
	})
}
