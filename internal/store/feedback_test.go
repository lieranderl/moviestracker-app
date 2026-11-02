package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachFeedbackStore runs check against every store that keeps
// recommendation feedback: the web app's stores and the local app's file.
func eachFeedbackStore(t *testing.T, check func(t *testing.T, s store.FeedbackStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenFeedbackFile(filepath.Join(t.TempDir(), "feedback.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "alex", &clock)
	})
}

// A user hides titles from recommendations and asks for more like others;
// each can be undone, and all of it reset.
func TestAUserGivesRecommendationFeedback(t *testing.T) {
	eachFeedbackStore(t, func(t *testing.T, s store.FeedbackStore, uid string, clock *time.Time) {
		ctx := context.Background()
		dune := store.TitleRef{Kind: "movie", TMDBID: 438631, Title: "Dune"}
		arrival := store.TitleRef{Kind: "movie", TMDBID: 329865, Title: "Arrival"}
		for _, step := range []struct {
			set store.Feedback
			ref store.TitleRef
			on  bool
		}{
			{store.NotInterested, dune, true},
			{store.MoreLikeThis, arrival, true},
			{store.MoreLikeThis, dune, true},
		} {
			*clock = clock.Add(time.Minute)
			if err := s.SetFeedback(ctx, uid, step.set, step.ref, step.on); err != nil {
				t.Fatal(err)
			}
		}
		hidden, err := s.FeedbackOn(ctx, uid, store.NotInterested)
		if err != nil || len(hidden) != 1 || hidden[0].TMDBID != 438631 {
			t.Fatalf("not interested = %+v, %v", hidden, err)
		}
		more, _ := s.FeedbackOn(ctx, uid, store.MoreLikeThis)
		if len(more) != 2 || more[0].Title != "Dune" || more[1].Title != "Arrival" {
			t.Errorf("more like this = %+v, want Dune then Arrival, newest first", more)
		}
		if err := s.SetFeedback(ctx, uid, store.NotInterested, dune, false); err != nil {
			t.Fatal(err)
		}
		if hidden, _ := s.FeedbackOn(ctx, uid, store.NotInterested); len(hidden) != 0 {
			t.Errorf("undoing not interested left %+v", hidden)
		}
		if err := s.ResetFeedback(ctx, uid); err != nil {
			t.Fatal(err)
		}
		if more, _ := s.FeedbackOn(ctx, uid, store.MoreLikeThis); len(more) != 0 {
			t.Errorf("after a reset: %+v", more)
		}
		if err := s.SetFeedback(ctx, uid, "rumours", dune, true); err == nil {
			t.Error("unknown feedback was kept")
		}
	})
}
