package store_test

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// stores are the implementations every behavior is checked against: the
// in-memory store, and Firestore when its emulator runs
// (FIRESTORE_EMULATOR_HOST, as `make firestore` and CI start it).
func stores(t *testing.T) map[string]func(t *testing.T, clock func() time.Time) store.Store {
	t.Helper()
	out := map[string]func(t *testing.T, clock func() time.Time) store.Store{
		"memory": func(t *testing.T, clock func() time.Time) store.Store { return store.NewMemory(store.WithClock(clock)) },
	}
	if os.Getenv("FIRESTORE_EMULATOR_HOST") != "" {
		out["firestore"] = func(t *testing.T, clock func() time.Time) store.Store {
			t.Helper()
			fs, err := store.NewFirestore(context.Background(), "demo-moviestracker", "moviestracker", store.WithClock(clock))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fs.Close() })
			return fs
		}
	}
	return out
}

// eachStore runs check against every store, as a new user of it.
func eachStore(t *testing.T, check func(t *testing.T, s store.Store, uid string, clock *time.Time)) {
	t.Helper()
	if len(stores(t)) == 1 {
		t.Log("Firestore not checked: set FIRESTORE_EMULATOR_HOST (make firestore)")
	}
	for name, open := range stores(t) {
		t.Run(name, func(t *testing.T) {
			clock := now
			check(t, open(t, func() time.Time { return clock }), "user-"+rand.Text(), &clock)
		})
	}
}

func TestANewUserHasNoPreferences(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		got, err := s.Preferences(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		if got != (store.Preferences{}) {
			t.Errorf("Preferences() = %+v, want none", got)
		}
	})
}

func TestAUsersLanguageIsKept(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.SavePreferences(ctx, uid, store.Preferences{Language: "ru"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Preferences(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if got.Language != "ru" {
			t.Errorf("Language = %q, want %q", got.Language, "ru")
		}
	})
}

var (
	dune    = store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune", Poster: "/d5NXSklXo0qyIYkgV94XAgMIckC.jpg"}
	severed = store.Favorite{Kind: "tv", TMDBID: 95396, Title: "Severance", Poster: "/pPHpeI2X1qEd1CS1SeyrdhZ4qnT.jpg"}
)

func TestANewUserHasNoFavourites(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		favs, err := s.Favorites(ctx, uid)
		if err != nil || len(favs) != 0 {
			t.Errorf("Favorites() = %v, %v, want none", favs, err)
		}
		if is, err := s.IsFavorite(ctx, uid, "movie", 438631); err != nil || is {
			t.Errorf("IsFavorite(Dune) = %v, %v, want false", is, err)
		}
	})
}

func TestFavouritesAreListedNewestFirst(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := context.Background()
		if err := s.AddFavorite(ctx, uid, dune); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(time.Minute)
		if err := s.AddFavorite(ctx, uid, severed); err != nil {
			t.Fatal(err)
		}
		favs, err := s.Favorites(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if len(favs) != 2 || favs[0].Title != "Severance" || favs[1].Title != "Dune" {
			t.Fatalf("Favorites() = %+v, want Severance then Dune", favs)
		}
		if favs[1].Kind != "movie" || favs[1].TMDBID != 438631 || favs[1].Poster != dune.Poster || !favs[1].AddedAt.Equal(now) {
			t.Errorf("Dune = %+v, want it as added at %v", favs[1], now)
		}
		if is, err := s.IsFavorite(ctx, uid, "tv", 95396); err != nil || !is {
			t.Errorf("IsFavorite(Severance) = %v, %v, want true", is, err)
		}
	})
}

func TestAFavouriteIsKeptOnce(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := context.Background()
		for range 2 {
			if err := s.AddFavorite(ctx, uid, dune); err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(time.Hour)
		}
		favs, err := s.Favorites(ctx, uid)
		if err != nil || len(favs) != 1 || !favs[0].AddedAt.Equal(now) {
			t.Errorf("Favorites() = %+v, %v, want Dune once, as first added", favs, err)
		}
	})
}

func TestAFavouriteCanBeRemoved(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.AddFavorite(ctx, uid, dune); err != nil {
			t.Fatal(err)
		}
		for range 2 { // removing one that is gone is fine
			if err := s.RemoveFavorite(ctx, uid, "movie", 438631); err != nil {
				t.Fatal(err)
			}
		}
		if is, err := s.IsFavorite(ctx, uid, "movie", 438631); err != nil || is {
			t.Errorf("IsFavorite(Dune) after removing = %v, %v, want false", is, err)
		}
	})
}

func TestUsersSeeOnlyTheirOwnFavourites(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.AddFavorite(ctx, uid, dune); err != nil {
			t.Fatal(err)
		}
		other := uid + "-other"
		if favs, err := s.Favorites(ctx, other); err != nil || len(favs) != 0 {
			t.Errorf("another user's Favorites() = %v, %v, want none", favs, err)
		}
	})
}

func TestOnlyMoviesAndShowsCanBeFavourites(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		for _, f := range []store.Favorite{
			{Kind: "../users", TMDBID: 1, Title: "x"},
			{Kind: "movie", TMDBID: 0, Title: "x"},
		} {
			if err := s.AddFavorite(ctx, uid, f); err == nil {
				t.Errorf("AddFavorite(%s %d) succeeded, want an error", f.Kind, f.TMDBID)
			}
		}
	})
}

func TestAUserKeepsTheirTorrServers(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		ctx := context.Background()
		home, err := s.SaveTorrServer(ctx, uid, store.TorrServer{Name: "Home", URL: "http://localhost:8090"})
		if err != nil {
			t.Fatal(err)
		}
		if home.ID == "" {
			t.Fatal("a new TorrServer got no ID")
		}
		*clock = clock.Add(time.Minute)
		nas, err := s.SaveTorrServer(ctx, uid, store.TorrServer{Name: "NAS", URL: "https://nas.tailnet.ts.net:8091"})
		if err != nil {
			t.Fatal(err)
		}
		home.Name = "Laptop"
		if _, err := s.SaveTorrServer(ctx, uid, home); err != nil {
			t.Fatal(err)
		}
		list, err := s.TorrServers(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].ID != home.ID || list[0].Name != "Laptop" || list[1].URL != "https://nas.tailnet.ts.net:8091" {
			t.Fatalf("TorrServers() = %+v, want Laptop then NAS, in the order added", list)
		}
		if err := s.RemoveTorrServer(ctx, uid, nas.ID); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.TorrServers(ctx, uid); len(list) != 1 || list[0].ID != home.ID {
			t.Errorf("after removing NAS, TorrServers() = %+v, want Laptop only", list)
		}
	})
}

func TestATorrServerNeedsAnHTTPAddress(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		for _, url := range []string{"", "localhost:8090", "ftp://nas", "javascript:alert(1)", "http://"} {
			if _, err := s.SaveTorrServer(context.Background(), uid, store.TorrServer{Name: "x", URL: url}); err == nil {
				t.Errorf("SaveTorrServer(%q) succeeded, want an error", url)
			}
		}
	})
}
