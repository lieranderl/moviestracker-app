package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// A user's library stamp changes whenever their favourites or watch states
// do, and only then, so what is worked out from the library can be kept
// until it changes.
func TestTheLibraryStampChangesWithTheLibrary(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		stamp := func() string {
			t.Helper()
			got, err := s.LibraryStamp(ctx, uid)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		changes := []struct {
			name   string
			change func() error
		}{
			{"adding a favourite", func() error {
				return s.AddFavorite(ctx, uid, store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"})
			}},
			{"marking a title watched", func() error {
				return s.SetTitleState(ctx, uid, store.TitleState{Kind: "tv", TMDBID: 95396, Title: "Severance", State: store.Watched})
			}},
			{"clearing a watch state", func() error {
				return s.SetTitleState(ctx, uid, store.TitleState{Kind: "tv", TMDBID: 95396})
			}},
			{"removing a favourite", func() error { return s.RemoveFavorite(ctx, uid, "movie", 438631) }},
		}
		before := stamp()
		if again := stamp(); again != before {
			t.Fatalf("the stamp changed from %q to %q with no change", before, again)
		}
		for _, c := range changes {
			if err := c.change(); err != nil {
				t.Fatal(err)
			}
			after := stamp()
			if after == before {
				t.Errorf("%s left the stamp %q", c.name, after)
			}
			before = after
		}
		if err := s.SavePreferences(ctx, uid, store.Preferences{Language: "ru"}); err != nil {
			t.Fatal(err)
		}
		if got := stamp(); got != before {
			t.Error("changing the language changed the library stamp")
		}
		if other, _ := s.LibraryStamp(ctx, uid+"-other"); other == before {
			t.Error("another user has the same library stamp")
		}
	})
}
