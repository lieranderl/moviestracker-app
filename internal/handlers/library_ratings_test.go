package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// flakyReviews cannot read reviews until it is fixed.
type flakyReviews struct {
	*store.Memory
	broken bool
}

func (f *flakyReviews) Reviews(ctx context.Context, uid string) ([]store.Review, error) {
	if f.broken {
		return nil, errors.New("reviews unavailable")
	}
	return f.Memory.Reviews(ctx, uid)
}

// A summary read without the user's ratings is not kept: once they can be
// read, a title rated low again starts no row.
func TestALibrarySummaryWithoutRatingsIsNotKept(t *testing.T) {
	ctx := context.Background()
	m := store.NewMemory()
	if err := m.SetTitleState(ctx, "u", store.TitleState{Kind: "movie", TMDBID: 1, Title: "Low", State: store.Watched}); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveReview(ctx, "u", store.Review{Kind: "movie", TMDBID: 1, Rating: 2}); err != nil {
		t.Fatal(err)
	}
	reviews := &flakyReviews{Memory: m, broken: true}
	lib := NewLibrary(m, nil, m.LibraryStamp)
	lib.Consider(nil, reviews)
	if _, err := lib.Summary(ctx, "u"); err == nil {
		t.Fatal("a summary without the ratings reported no error")
	}
	reviews.broken = false
	s, err := lib.Summary(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Seeds) != 0 {
		t.Errorf("a title rated 2 seeds recommendations: %+v", s.Seeds)
	}
}
