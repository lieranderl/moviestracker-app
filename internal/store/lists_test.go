package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachListStore runs check against every store that keeps custom lists:
// the web app's stores and the local app's file in its data directory.
func eachListStore(t *testing.T, check func(t *testing.T, s store.CustomListStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenListFile(filepath.Join(t.TempDir(), "lists.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "user-"+rand.Text(), &clock)
	})
}

var (
	duneTitle    = store.ListTitle{Kind: "movie", TMDBID: 438631, Title: "Dune", Poster: "/d.jpg"}
	arrivalTitle = store.ListTitle{Kind: "movie", TMDBID: 329865, Title: "Arrival"}
)

// listTitles are the titles' names in l, in order.
func listTitles(l store.CustomList) []string {
	var out []string
	for _, t := range l.Titles {
		out = append(out, t.Title)
	}
	return out
}

func TestAUserMakesAListAndFillsItNewestFirst(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, clock *time.Time) {
		ctx := context.Background()
		made, err := s.CreateCustomList(ctx, uid, "  Sci-fi nights ")
		if err != nil {
			t.Fatal(err)
		}
		if made.ID == "" || made.Name != "Sci-fi nights" {
			t.Fatalf("CreateCustomList() = %+v, want an ID and the name trimmed", made)
		}
		for _, title := range []store.ListTitle{duneTitle, arrivalTitle, duneTitle} {
			*clock = clock.Add(time.Minute)
			if err := s.AddToList(ctx, uid, made.ID, title); err != nil {
				t.Fatal(err)
			}
		}
		got, ok, err := s.CustomList(ctx, uid, made.ID)
		if err != nil || !ok {
			t.Fatalf("CustomList() = %v, %v", ok, err)
		}
		if names := listTitles(got); len(names) != 2 || names[0] != "Arrival" || names[1] != "Dune" {
			t.Errorf("titles = %q, want [Arrival Dune]: newest first, Dune once", names)
		}
		if got.Titles[1].Poster != "/d.jpg" || !got.Titles[1].AddedAt.Equal(now.Add(time.Minute)) {
			t.Errorf("Dune = %+v, want its poster, added when first added", got.Titles[1])
		}
		if err := s.RemoveFromList(ctx, uid, made.ID, "movie", 438631); err != nil {
			t.Fatal(err)
		}
		if got, _, _ := s.CustomList(ctx, uid, made.ID); len(got.Titles) != 1 || got.Titles[0].Title != "Arrival" {
			t.Errorf("after removing Dune: %q", listTitles(got))
		}
	})
}

func TestAUsersListsComeMostRecentlyChangedFirstAndCanBeDeleted(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, clock *time.Time) {
		ctx := context.Background()
		var made []store.CustomList
		for _, name := range []string{"Sci-fi", "Comfort", "Oscars"} {
			*clock = clock.Add(time.Minute)
			l, err := s.CreateCustomList(ctx, uid, name)
			if err != nil {
				t.Fatal(err)
			}
			made = append(made, l)
		}
		*clock = clock.Add(time.Minute)
		if err := s.AddToList(ctx, uid, made[0].ID, duneTitle); err != nil {
			t.Fatal(err)
		}
		if err := s.RemoveCustomList(ctx, uid, made[1].ID); err != nil {
			t.Fatal(err)
		}
		lists, err := s.CustomLists(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, l := range lists {
			names = append(names, l.Name)
		}
		if len(names) != 2 || names[0] != "Sci-fi" || names[1] != "Oscars" {
			t.Errorf("lists = %q, want [Sci-fi Oscars]: Sci-fi changed last, Comfort deleted", names)
		}
		if len(lists) > 0 && len(lists[0].Titles) != 1 {
			t.Errorf("Sci-fi's titles = %q, want [Dune]", listTitles(lists[0]))
		}
		if _, ok, err := s.CustomList(ctx, uid, made[1].ID); ok || err != nil {
			t.Errorf("a deleted list = %v, %v; want none", ok, err)
		}
		if lists, _ := s.CustomLists(ctx, "someone-else-"+uid); len(lists) != 0 {
			t.Errorf("another user's lists = %d, want none", len(lists))
		}
	})
}

func TestAListNeedsANameAndItsOwner(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for _, name := range []string{"", "   ", strings.Repeat("я", 61)} {
			if _, err := s.CreateCustomList(ctx, uid, name); err == nil {
				t.Errorf("a list named %q was made", name)
			}
		}
		mine, err := s.CreateCustomList(ctx, uid, "Mine")
		if err != nil {
			t.Fatal(err)
		}
		other := "someone-else-" + uid
		if err := s.AddToList(ctx, other, mine.ID, duneTitle); !errors.Is(err, store.ErrNoList) {
			t.Errorf("another user adding to my list: %v, want ErrNoList", err)
		}
		if err := s.RemoveCustomList(ctx, other, mine.ID); !errors.Is(err, store.ErrNoList) {
			t.Errorf("another user deleting my list: %v, want ErrNoList", err)
		}
		for _, id := range []string{"", "a/b", "../lists"} {
			if _, ok, err := s.CustomList(ctx, uid, id); ok || err != nil {
				t.Errorf("list %q = %v, %v; want none", id, ok, err)
			}
			if err := s.AddToList(ctx, uid, id, duneTitle); !errors.Is(err, store.ErrNoList) {
				t.Errorf("adding to list %q: %v, want ErrNoList", id, err)
			}
		}
		if err := s.AddToList(ctx, uid, mine.ID, store.ListTitle{Kind: "book", TMDBID: 1}); err == nil {
			t.Error("a book was added to a list")
		}
	})
}

func TestAUserKeepsAtMostFiftyLists(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for i := range 50 {
			if _, err := s.CreateCustomList(ctx, uid, fmt.Sprint("List ", i)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.CreateCustomList(ctx, uid, "One too many"); !errors.Is(err, store.ErrTooManyLists) {
			t.Errorf("the 51st list: %v, want ErrTooManyLists", err)
		}
	})
}

func TestAListHoldsAtMostFiveHundredTitles(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, _ *time.Time) {
		if _, ok := s.(*store.Firestore); ok {
			t.Skip("500 transactions on one document exhaust the emulator's memory; Firestore checks the cap as the other stores do")
		}
		ctx := context.Background()
		l, err := s.CreateCustomList(ctx, uid, "Everything")
		if err != nil {
			t.Fatal(err)
		}
		for id := 1; id <= 500; id++ {
			if err := s.AddToList(ctx, uid, l.ID, store.ListTitle{Kind: "movie", TMDBID: id, Title: fmt.Sprint(id)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.AddToList(ctx, uid, l.ID, duneTitle); !errors.Is(err, store.ErrListFull) {
			t.Errorf("the 501st title: %v, want ErrListFull", err)
		}
		if err := s.AddToList(ctx, uid, l.ID, store.ListTitle{Kind: "movie", TMDBID: 7}); err != nil {
			t.Errorf("adding a title already in a full list: %v, want none", err)
		}
	})
}

func TestASharedListIsReadByItsLinkUntilSharingStops(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		l, err := s.CreateCustomList(ctx, uid, "For Anna")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddToList(ctx, uid, l.ID, duneTitle); err != nil {
			t.Fatal(err)
		}
		shared, err := s.ShareList(ctx, uid, l.ID, true)
		if err != nil || len(shared.ShareID) < 16 {
			t.Fatalf("ShareList(on) = %+v, %v; want a long random link ID", shared, err)
		}
		if again, _ := s.ShareList(ctx, uid, l.ID, true); again.ShareID != shared.ShareID {
			t.Errorf("sharing a shared list again changed its link")
		}
		got, ok, err := s.SharedList(ctx, shared.ShareID)
		if err != nil || !ok || got.Name != "For Anna" || len(got.Titles) != 1 {
			t.Errorf("SharedList() = %+v, %v, %v; want For Anna with Dune", got, ok, err)
		}
		if mine, _, _ := s.CustomList(ctx, uid, l.ID); mine.ShareID != shared.ShareID {
			t.Errorf("the owner's list has link %q, want %q", mine.ShareID, shared.ShareID)
		}
		if _, err := s.ShareList(ctx, uid, l.ID, false); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.SharedList(ctx, shared.ShareID); ok {
			t.Error("the link still opens the list after sharing stopped")
		}
		reshared, _ := s.ShareList(ctx, uid, l.ID, true)
		if reshared.ShareID == shared.ShareID {
			t.Error("sharing again brought the old link back")
		}
		if err := s.RemoveCustomList(ctx, uid, l.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.SharedList(ctx, reshared.ShareID); ok {
			t.Error("a deleted list's link still opens it")
		}
		if _, err := s.ShareList(ctx, "someone-else-"+uid, l.ID, true); !errors.Is(err, store.ErrNoList) {
			t.Errorf("sharing another user's list: %v, want ErrNoList", err)
		}
		for _, id := range []string{"", "a/b", "nope"} {
			if _, ok, err := s.SharedList(ctx, id); ok || err != nil {
				t.Errorf("SharedList(%q) = %v, %v; want none", id, ok, err)
			}
		}
	})
}

func TestListsMadeAtOnceStillStopAtFifty(t *testing.T) {
	eachListStore(t, func(t *testing.T, s store.CustomListStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for i := range 48 {
			if _, err := s.CreateCustomList(ctx, uid, fmt.Sprint("List ", i)); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		for i := range 6 {
			wg.Go(func() { _, _ = s.CreateCustomList(ctx, uid, fmt.Sprint("Racing ", i)) })
		}
		wg.Wait()
		if lists, _ := s.CustomLists(ctx, uid); len(lists) != 50 {
			t.Errorf("after six lists made at once from 48 there are %d, want 50", len(lists))
		}
	})
}

func TestAChangeTheListFileCouldNotSaveIsSavedWhenTriedAgain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "lists.json")
	s, err := store.OpenListFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	l, err := s.CreateCustomList(ctx, "anna", "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil { // the data volume is gone for a moment
		t.Fatal(err)
	}
	failed := s.AddToList(ctx, "anna", l.ID, duneTitle)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if failed == nil {
		t.Fatal("adding to a list whose directory is gone succeeded")
	}
	if got, _, _ := s.CustomList(ctx, "anna", l.ID); len(got.Titles) != 0 {
		t.Errorf("after the failed save the list shows %q, want nothing", listTitles(got))
	}
	if err := s.AddToList(ctx, "anna", l.ID, duneTitle); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.OpenListFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := reopened.CustomList(ctx, "anna", l.ID); len(got.Titles) != 1 {
		t.Errorf("after trying again the file keeps %q, want [Dune]", listTitles(got))
	}
}
