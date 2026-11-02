package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// pages reads every page through read, failing on a repeat or a page over
// size; it returns the IDs in order.
func pages(t *testing.T, size int, read func(after string) ([]int, string, error)) []int {
	t.Helper()
	var all []int
	seen := map[int]bool{}
	after := ""
	for range 10 {
		ids, next, err := read(after)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) > size {
			t.Fatalf("a page of %d, want at most %d", len(ids), size)
		}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("title %d listed twice", id)
			}
			seen[id] = true
		}
		all = append(all, ids...)
		if next == "" {
			return all
		}
		after = next
	}
	t.Fatal("pages never end")
	return nil
}

// Favourites come 60 at a time, newest first, each once even when added at
// the same moment.
func TestFavouritesComeAPageAtATime(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		for i := 1; i <= 130; i++ {
			at := now.Add(time.Duration(i) * time.Minute)
			if i > 125 {
				at = now.Add(125 * time.Minute)
			}
			if err := s.AddFavorite(ctx, uid, store.Favorite{Kind: "movie", TMDBID: i, Title: fmt.Sprint("Movie ", i), AddedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		got := pages(t, 60, func(after string) ([]int, string, error) {
			favs, next, err := s.FavoritesPage(ctx, uid, after)
			ids := make([]int, len(favs))
			for i, f := range favs {
				ids[i] = f.TMDBID
			}
			return ids, next, err
		})
		if len(got) != 130 || got[len(got)-1] != 1 || got[5] != 125 {
			t.Errorf("favourites in pages = %d titles, %v…; want all 130 newest first", len(got), got[:min(8, len(got))])
		}
		if _, _, err := s.FavoritesPage(ctx, uid, "nonsense"); !errors.Is(err, store.ErrCursor) {
			t.Errorf("a malformed cursor gave %v, want ErrCursor", err)
		}
	})
}

// A watch list comes 60 at a time, most recently marked first, and holds
// only titles in that state.
func TestAWatchListComesAPageAtATime(t *testing.T) {
	eachTitleStore(t, func(t *testing.T, s store.TitleStateStore, uid string, clock *time.Time) {
		ctx := context.Background()
		for i := 1; i <= 75; i++ {
			*clock = now.Add(time.Duration(i) * time.Minute)
			state := store.Watched
			if i%5 == 0 {
				state = store.WantToWatch
			}
			if err := s.SetTitleState(ctx, uid, store.TitleState{Kind: "movie", TMDBID: i, Title: fmt.Sprint("Movie ", i), State: state}); err != nil {
				t.Fatal(err)
			}
		}
		got := pages(t, 60, func(after string) ([]int, string, error) {
			list, next, err := s.TitleStatesPage(ctx, uid, store.Watched, after)
			ids := make([]int, len(list))
			for i, ts := range list {
				if ts.State != store.Watched {
					t.Errorf("%s is %q, not watched", ts.Title, ts.State)
				}
				ids[i] = ts.TMDBID
			}
			return ids, next, err
		})
		if len(got) != 60 || got[0] != 74 || got[len(got)-1] != 1 {
			t.Errorf("watched in pages = %d titles, %v…; want the 60 watched, 74 first", len(got), got[:min(5, len(got))])
		}
		want, next, err := s.TitleStatesPage(ctx, uid, store.WantToWatch, "")
		if err != nil || len(want) != 15 || next != "" || want[0].TMDBID != 75 {
			t.Errorf("want to watch = %d titles, next %q, %v", len(want), next, err)
		}
	})
}

// Watch states saved before lists were paged still show in their list.
func TestWatchStatesSavedBeforePagingStillShow(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("Firestore not checked: set FIRESTORE_EMULATOR_HOST (make firestore)")
	}
	ctx := context.Background()
	client, err := firestore.NewClientWithDatabase(ctx, "demo-moviestracker", "moviestracker", option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	uid := "user-" + rand.Text()
	// As SetTitleState saved them before: no listKey.
	_, err = client.Collection("users").Doc(uid).Collection("titles").Doc("movie-27205").Set(ctx, map[string]any{
		"kind": "movie", "tmdbId": 27205, "title": "Inception", "state": store.Watched, "updatedAt": now,
	})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := store.NewFirestore(ctx, "demo-moviestracker", "moviestracker", store.WithClock(func() time.Time { return now.Add(time.Hour) }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	if err := fs.SetTitleState(ctx, uid, store.TitleState{Kind: "tv", TMDBID: 95396, Title: "Severance", State: store.Watched}); err != nil {
		t.Fatal(err)
	}
	for range 2 { // keyed the first time, read as keyed the second
		list, _, err := fs.TitleStatesPage(ctx, uid, store.Watched, "")
		if err != nil || len(list) != 2 || list[0].Title != "Severance" || list[1].Title != "Inception" {
			t.Fatalf("watched = %+v, %v; want Severance then the older Inception", list, err)
		}
	}
}
