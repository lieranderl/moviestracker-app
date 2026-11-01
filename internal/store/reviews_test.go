package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachReviewStore runs check against every store that keeps ratings and
// notes: the web app's stores and the local app's file in its data
// directory.
func eachReviewStore(t *testing.T, check func(t *testing.T, s store.ReviewStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenReviewFile(filepath.Join(t.TempDir(), "reviews.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "user-"+rand.Text(), &clock)
	})
}

func TestAUserRatesATitleAndKeepsANoteOnIt(t *testing.T) {
	eachReviewStore(t, func(t *testing.T, s store.ReviewStore, uid string, _ *time.Time) {
		ctx := context.Background()
		r := store.Review{Kind: "movie", TMDBID: 438631, Title: "Dune", Poster: "/d.jpg", Rating: 9, Note: "  See it in IMAX.  "}
		if err := s.SaveReview(ctx, uid, r); err != nil {
			t.Fatal(err)
		}
		got, err := s.Review(ctx, uid, "movie", 438631)
		if err != nil || got.Rating != 9 || got.Note != "See it in IMAX." || got.Title != "Dune" || !got.UpdatedAt.Equal(now) {
			t.Errorf("Review() = %+v, %v; want Dune rated 9 with its note trimmed, saved now", got, err)
		}
		if other, _ := s.Review(ctx, uid, "movie", 329865); other.Rating != 0 || other.Note != "" {
			t.Errorf("an unrated title's review = %+v, want none", other)
		}
		r.Rating, r.Note = 0, " "
		if err := s.SaveReview(ctx, uid, r); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Review(ctx, uid, "movie", 438631); got != (store.Review{}) {
			t.Errorf("after clearing rating and note the review is %+v, want none", got)
		}
	})
}

func TestARatingIsOneToTenAndANoteAtMostTwoThousandCharacters(t *testing.T) {
	eachReviewStore(t, func(t *testing.T, s store.ReviewStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for _, r := range []store.Review{
			{Kind: "movie", TMDBID: 438631, Rating: 11},
			{Kind: "movie", TMDBID: 438631, Rating: -1},
			{Kind: "movie", TMDBID: 438631, Note: strings.Repeat("я", 2001)},
		} {
			if err := s.SaveReview(ctx, uid, r); !errors.Is(err, store.ErrReview) {
				t.Errorf("SaveReview(rating %d, note of %d) = %v, want ErrReview", r.Rating, len([]rune(r.Note)), err)
			}
		}
		if err := s.SaveReview(ctx, uid, store.Review{Kind: "book", TMDBID: 1, Rating: 5}); err == nil {
			t.Error("a book was rated")
		}
		if err := s.SaveReview(ctx, uid, store.Review{Kind: "tv", TMDBID: 95396, Note: strings.Repeat("я", 2000)}); err != nil {
			t.Errorf("a note of 2000 characters: %v", err)
		}
	})
}

func TestARatingTheFileCouldNotSaveIsSavedWhenTriedAgain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "reviews.json")
	s, err := store.OpenReviewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dune := store.Review{Kind: "movie", TMDBID: 438631, Title: "Dune", Rating: 8}
	if err := os.RemoveAll(dir); err != nil { // the data volume is gone for a moment
		t.Fatal(err)
	}
	failed := s.SaveReview(ctx, "anna", dune)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if failed == nil {
		t.Fatal("rating in a directory that is gone succeeded")
	}
	if got, _ := s.Review(ctx, "anna", "movie", 438631); got.Rating != 0 {
		t.Errorf("after the failed save Dune is rated %d, want unrated", got.Rating)
	}
	if err := s.SaveReview(ctx, "anna", dune); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.OpenReviewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reopened.Review(ctx, "anna", "movie", 438631); got.Rating != 8 {
		t.Errorf("after trying again the file rates Dune %d, want 8", got.Rating)
	}
}
