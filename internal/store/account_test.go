package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// keepEverything gives uid one of everything the store keeps, with its
// list shared; it returns the list's share ID.
func keepEverything(t *testing.T, s store.Store, uid string) string {
	t.Helper()
	ctx := context.Background()
	steps := []func() error{
		func() error { return s.SavePreferences(ctx, uid, store.Preferences{Language: "ru"}) },
		func() error {
			return s.AddFavorite(ctx, uid, store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"})
		},
		func() error {
			_, err := s.SaveTorrServer(ctx, uid, store.TorrServer{Name: "Home", URL: "http://nas:8090"})
			return err
		},
		func() error {
			return s.SaveProgress(ctx, uid, store.Progress{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1, Title: "Dune", Position: time.Minute, Duration: time.Hour})
		},
		func() error {
			return s.SetTitleState(ctx, uid, store.TitleState{Kind: "tv", TMDBID: 95396, Title: "Severance", State: store.Watched})
		},
		func() error { return s.AddUpdates(ctx, uid, []store.Update{duneInDV}) },
		func() error { return s.SaveReleaseFilters(ctx, uid, store.ReleaseFilters{HDR: true}) },
		func() error { return s.MarkEpisodes(ctx, uid, 95396, 1, []int{1, 2}, true) },
		func() error {
			return s.Follow(ctx, uid, store.FollowedSeries{TMDBID: 95396, Title: "Severance"}, true)
		},
		func() error {
			return s.SetFeedback(ctx, uid, store.NotInterested, store.TitleRef{Kind: "movie", TMDBID: 329865, Title: "Arrival"}, true)
		},
		func() error {
			return s.SaveReview(ctx, uid, store.Review{Kind: "movie", TMDBID: 438631, Title: "Dune", Rating: 9, Note: "Loud"})
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	l, err := s.CreateCustomList(ctx, uid, "Sci-fi")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddToList(ctx, uid, l.ID, store.ListTitle{Kind: "movie", TMDBID: 438631, Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	shared, err := s.ShareList(ctx, uid, l.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return shared.ShareID
}

// A user can take away everything kept about them.
func TestAUserExportsEverythingKeptAboutThem(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		keepEverything(t, s, uid)
		got, err := s.ExportAccount(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case got.Preferences.Language != "ru",
			len(got.Favorites) != 1 || got.Favorites[0].Title != "Dune",
			len(got.TorrServers) != 1 || got.TorrServers[0].Name != "Home",
			len(got.Progress) != 1 || got.Progress[0].Position != time.Minute,
			len(got.TitleStates) != 1 || got.TitleStates[0].Title != "Severance",
			len(got.Updates) != 1 || got.Updates[0].Title != "Dune",
			!got.ReleaseFilters.HDR,
			len(got.Lists) != 1 || got.Lists[0].Name != "Sci-fi" || len(got.Lists[0].Titles) != 1 || got.Lists[0].ShareID == "",
			len(got.Reviews) != 1 || got.Reviews[0].Note != "Loud",
			len(got.Episodes[95396][1]) != 2,
			len(got.Follows) != 1 || got.Follows[0].Title != "Severance",
			len(got.Feedback[store.NotInterested]) != 1 || got.Feedback[store.NotInterested][0].Title != "Arrival":
			t.Errorf("export lacks something kept: %+v", got)
		}
	})
}

// Deleting an account deletes everything kept for it, stops its shared
// links, leaves other users alone, and can be tried again.
func TestDeletingAnAccountDeletesEverythingKeptForIt(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		shareID := keepEverything(t, s, uid)
		other := uid + "-other"
		otherShare := keepEverything(t, s, other)
		for range 2 { // a retry after a failure finds less, and still succeeds
			if err := s.DeleteAccount(ctx, uid); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.ExportAccount(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if got.Preferences.Language != "" || len(got.Favorites)+len(got.TorrServers)+len(got.Progress)+len(got.TitleStates)+len(got.Updates)+len(got.Lists)+len(got.Reviews)+len(got.Follows)+len(got.Feedback) != 0 || got.ReleaseFilters.HDR {
			t.Errorf("after deletion the account still holds %+v", got)
		}
		if _, ok, err := s.SharedList(ctx, shareID); ok || err != nil {
			t.Errorf("the deleted account's shared link still opens (%v)", err)
		}
		if _, ok, _ := s.SharedList(ctx, otherShare); !ok {
			t.Error("another user's shared link stopped working")
		}
		if eps, _ := s.WatchedEpisodes(ctx, uid, 95396); len(eps) != 0 {
			t.Errorf("the deleted account still has watched episodes: %v", eps)
		}
		if favs, _ := s.Favorites(ctx, other); len(favs) != 1 {
			t.Error("another user's favourites went too")
		}
	})
}

// Deleting an account notes when, so sessions signed in before can end.
func TestADeletedAccountRemembersWhenItWasDeleted(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := context.Background()
		if at, err := s.AccountDeletedAt(ctx, uid); err != nil || !at.IsZero() {
			t.Fatalf("a live account was deleted at %v (%v)", at, err)
		}
		keepEverything(t, s, uid)
		*clock = now.Add(time.Hour)
		if err := s.DeleteAccount(ctx, uid); err != nil {
			t.Fatal(err)
		}
		if at, err := s.AccountDeletedAt(ctx, uid); err != nil || !at.Equal(now.Add(time.Hour)) {
			t.Errorf("AccountDeletedAt() = %v, %v; want the deletion time", at, err)
		}
		if at, _ := s.AccountDeletedAt(ctx, uid+"-other"); !at.IsZero() {
			t.Error("another user's account is noted as deleted")
		}
	})
}

func TestADeletedSessionCannotRecreateAccountData(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := store.WithAccountSession(context.Background(), uid, *clock)
		keepEverything(t, s, uid)
		*clock = clock.Add(time.Second)
		if err := s.DeleteAccount(context.Background(), uid); err != nil {
			t.Fatal(err)
		}
		writes := []struct {
			name  string
			write func() error
		}{
			{"preferences", func() error { return s.SavePreferences(ctx, uid, store.Preferences{Language: "ru"}) }},
			{"favourites", func() error {
				return s.AddFavorite(ctx, uid, store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"})
			}},
			{"servers", func() error {
				_, err := s.SaveTorrServer(ctx, uid, store.TorrServer{Name: "Home", URL: "http://nas:8090"})
				return err
			}},
			{"progress", func() error {
				return s.SaveProgress(ctx, uid, store.Progress{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1, Position: time.Minute, Duration: time.Hour})
			}},
			{"watch states", func() error {
				return s.SetTitleState(ctx, uid, store.TitleState{Kind: "movie", TMDBID: 438631, State: store.WantToWatch})
			}},
			{"alerts", func() error { return s.AddUpdates(ctx, uid, []store.Update{duneInDV}) }},
			{"release filters", func() error { return s.SaveReleaseFilters(ctx, uid, store.ReleaseFilters{HDR: true}) }},
			{"lists", func() error { _, err := s.CreateCustomList(ctx, uid, "Sci-fi"); return err }},
			{"reviews", func() error { return s.SaveReview(ctx, uid, store.Review{Kind: "movie", TMDBID: 438631, Rating: 9}) }},
			{"episodes", func() error { return s.MarkEpisodes(ctx, uid, 95396, 1, []int{1}, true) }},
			{"followed series", func() error { return s.Follow(ctx, uid, store.FollowedSeries{TMDBID: 95396, Title: "Severance"}, true) }},
			{"alert checkpoints", func() error {
				return s.SaveAlertCheckpoints(ctx, uid, store.AlertCheckpoints{Library: "legacy", Feeds: map[string]time.Time{"hdr10movies": *clock}})
			}},
		}
		for _, w := range writes {
			t.Run(w.name, func(t *testing.T) {
				if err := w.write(); !errors.Is(err, store.ErrAccountDeleted) {
					t.Errorf("revoked write = %v; want account-session rejection", err)
				}
			})
		}
	})
}

func TestARevokedSessionCannotMutateARecreatedAccount(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		old := store.WithAccountSession(context.Background(), uid, *clock)
		keepEverything(t, s, uid)
		*clock = clock.Add(time.Second)
		if err := s.DeleteAccount(context.Background(), uid); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(time.Second)
		keepEverything(t, s, uid)
		account, err := s.ExportAccount(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		list, server := account.Lists[0].ID, account.TorrServers[0].ID
		writes := []struct {
			name  string
			write func() error
		}{
			{"remove favourite", func() error { return s.RemoveFavorite(old, uid, "movie", 438631) }},
			{"remove server", func() error { return s.RemoveTorrServer(old, uid, server) }},
			{"mark alert seen", func() error { return s.MarkUpdatesSeen(old, uid, []string{duneInDV.ID()}) }},
			{"mark all alerts seen", func() error { return s.MarkAllUpdatesSeen(old, uid, *clock) }},
			{"remove list", func() error { return s.RemoveCustomList(old, uid, list) }},
			{"share list", func() error { _, err := s.ShareList(old, uid, list, false); return err }},
			{"add list title", func() error {
				return s.AddToList(old, uid, list, store.ListTitle{Kind: "movie", TMDBID: 329865, Title: "Arrival"})
			}},
			{"remove list title", func() error { return s.RemoveFromList(old, uid, list, "movie", 438631) }},
		}
		for _, w := range writes {
			t.Run(w.name, func(t *testing.T) {
				if err := w.write(); !errors.Is(err, store.ErrAccountDeleted) {
					t.Errorf("revoked mutation = %v; want account-session rejection", err)
				}
			})
		}
	})
}

func TestAnOldDeletionCannotEraseARecreatedAccount(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		old := store.WithAccountSession(context.Background(), uid, *clock)
		keepEverything(t, s, uid)
		*clock = clock.Add(time.Second)
		if err := s.DeleteAccount(old, uid); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(time.Second)
		keepEverything(t, s, uid)
		if err := s.DeleteAccount(old, uid); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("old deletion restarted = %v; want rejection", err)
		}
		fresh, err := s.ExportAccount(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		if len(fresh.Favorites) != 1 || len(fresh.Lists) != 1 {
			t.Error("a stale deletion erased the recreated account")
		}
	})
}
