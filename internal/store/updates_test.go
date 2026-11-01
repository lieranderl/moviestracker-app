package store_test

import (
	"context"
	"fmt"
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
		got, _, err := s.Updates(ctx, uid, "")
		if err != nil || len(got) != 2 || got[0].Title != "Arrival" || got[1].Title != "Dune" || got[0].Seen {
			t.Fatalf("Updates() = %+v, %v; want Arrival then Dune, unseen", got, err)
		}
		if err := s.MarkUpdatesSeen(ctx, uid, []string{got[0].ID(), got[1].ID()}); err != nil {
			t.Fatal(err)
		}
		// The backend finds Dune again: the user was already told.
		again := duneInDV
		again.FoundAt = now
		if err := s.AddUpdates(ctx, uid, []store.Update{again}); err != nil {
			t.Fatal(err)
		}
		got, _, _ = s.Updates(ctx, uid, "")
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

// manyUpdates are n updates of one feed, found a minute apart, the first
// found last; the last three are found at the same moment.
func manyUpdates(n int) []store.Update {
	out := make([]store.Update, n)
	for i := range out {
		at := now.Add(-time.Duration(i) * time.Minute)
		if i >= n-3 {
			at = now.Add(-time.Duration(n-3) * time.Minute)
		}
		out[i] = store.Update{Feed: "latesttorrentsmovies", TMDBID: i + 1, Title: fmt.Sprint("Movie ", i+1), FoundAt: at}
	}
	return out
}

// Updates come a page at a time, newest first: every update once, even
// those found at the same moment.
func TestUpdatesComeAPageAtATimeNewestFirst(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.AddUpdates(ctx, uid, manyUpdates(130)); err != nil {
			t.Fatal(err)
		}
		first, next, err := s.Updates(ctx, uid, "")
		if err != nil || len(first) != 100 || next == "" || first[0].TMDBID != 1 || first[99].TMDBID != 100 {
			t.Fatalf("first page = %d updates (%v…), next %q, %v", len(first), first[:min(1, len(first))], next, err)
		}
		rest, last, err := s.Updates(ctx, uid, next)
		if err != nil || len(rest) != 30 || last != "" {
			t.Fatalf("second page = %d updates, next %q, %v; want the other 30 and no more", len(rest), last, err)
		}
		seen := map[int]bool{}
		for _, u := range append(first, rest...) {
			if seen[u.TMDBID] {
				t.Errorf("movie %d listed twice", u.TMDBID)
			}
			seen[u.TMDBID] = true
		}
		if len(seen) != 130 {
			t.Errorf("listed %d of 130 updates", len(seen))
		}
		if _, _, err := s.Updates(ctx, uid, "not a cursor"); err == nil {
			t.Error("a malformed cursor was accepted")
		}
	})
}

// Only the updates the user was shown become seen; the count of unseen
// ones covers every update, not just the newest page.
func TestOnlyTheUpdatesShownBecomeSeen(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.AddUpdates(ctx, uid, manyUpdates(130)); err != nil {
			t.Fatal(err)
		}
		if n, err := s.UnseenUpdates(ctx, uid); err != nil || n != 130 {
			t.Fatalf("UnseenUpdates() = %d, %v; want 130", n, err)
		}
		page, _, _ := s.Updates(ctx, uid, "")
		ids := make([]string, len(page))
		for i, u := range page {
			ids[i] = u.ID()
		}
		if err := s.MarkUpdatesSeen(ctx, uid, ids); err != nil {
			t.Fatal(err)
		}
		if n, _ := s.UnseenUpdates(ctx, uid); n != 30 {
			t.Errorf("after the newest 100 were shown, %d unseen; want the older 30", n)
		}
		if err := s.MarkUpdatesSeen(ctx, uid, []string{"dvmovies-999999", "rumours-1"}); err != nil {
			t.Errorf("marking updates the user does not have: %v", err)
		}
		if n, _ := s.UnseenUpdates(ctx, uid); n != 30 {
			t.Errorf("marking unknown updates changed the count to %d", n)
		}
	})
}

// Mark all read covers the updates found up to when the user asked; one
// found later stays unseen. Other users' updates are untouched.
func TestMarkAllReadLeavesLaterUpdatesUnseen(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		other := uid + "-other"
		if err := s.AddUpdates(ctx, uid, manyUpdates(130)); err != nil {
			t.Fatal(err)
		}
		if err := s.AddUpdates(ctx, other, []store.Update{duneInDV}); err != nil {
			t.Fatal(err)
		}
		later := store.Update{Feed: "hdr10movies", TMDBID: 329865, Title: "Arrival", FoundAt: now.Add(time.Second)}
		if err := s.AddUpdates(ctx, uid, []store.Update{later}); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkAllUpdatesSeen(ctx, uid, now); err != nil {
			t.Fatal(err)
		}
		if n, _ := s.UnseenUpdates(ctx, uid); n != 1 {
			t.Errorf("after Mark all read, %d unseen; want only the later Arrival", n)
		}
		if n, _ := s.UnseenUpdates(ctx, other); n != 1 {
			t.Errorf("another user's update was marked seen")
		}
	})
}
