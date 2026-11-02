package store_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachEpisodeStore runs check against every store that keeps watched
// episodes: the web app's stores and the local app's file.
func eachEpisodeStore(t *testing.T, check func(t *testing.T, s store.EpisodeStore, uid string)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) { check(t, s, uid) })
	t.Run("file", func(t *testing.T) {
		s, err := store.OpenEpisodeFile(filepath.Join(t.TempDir(), "episodes.json"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "alex")
	})
}

const severanceID = 95396

// A user marks episodes watched one at a time or a season at once, and
// unmarks them; each series keeps its own.
func TestAUserMarksEpisodesWatched(t *testing.T) {
	eachEpisodeStore(t, func(t *testing.T, s store.EpisodeStore, uid string) {
		ctx := context.Background()
		steps := []struct {
			season   int
			episodes []int
			watched  bool
		}{
			{1, []int{1, 2}, true},
			{1, []int{2}, false},
			{2, []int{3, 1, 2}, true},
			{1, []int{1}, true}, // marking one again changes nothing
		}
		for _, st := range steps {
			if err := s.MarkEpisodes(ctx, uid, severanceID, st.season, st.episodes, st.watched); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.WatchedEpisodes(ctx, uid, severanceID)
		if want := map[int][]int{1: {1}, 2: {1, 2, 3}}; err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("WatchedEpisodes() = %v, %v; want %v", got, err, want)
		}
		if err := s.MarkEpisodes(ctx, uid, severanceID, 1, []int{1}, false); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.WatchedEpisodes(ctx, uid, severanceID); !reflect.DeepEqual(got, map[int][]int{2: {1, 2, 3}}) {
			t.Errorf("after unmarking season 1's last episode: %v", got)
		}
		if other, _ := s.WatchedEpisodes(ctx, uid, 1396); len(other) != 0 {
			t.Errorf("another series has watched episodes: %v", other)
		}
		for _, bad := range [][]int{{0}, {-1}} {
			if err := s.MarkEpisodes(ctx, uid, severanceID, 1, bad, true); err == nil {
				t.Errorf("episode numbers %v were kept", bad)
			}
		}
		if err := s.MarkEpisodes(ctx, uid, 0, 1, []int{1}, true); err == nil {
			t.Error("a series without an ID was kept")
		}
	})
}

// The local app's file keeps watched episodes across restarts.
func TestTheLocalAppKeepsWatchedEpisodesAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "episodes.json")
	first, err := store.OpenEpisodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.MarkEpisodes(context.Background(), "alex", severanceID, 1, []int{4}, true); err != nil {
		t.Fatal(err)
	}
	again, err := store.OpenEpisodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := again.WatchedEpisodes(context.Background(), "alex", severanceID); !reflect.DeepEqual(got, map[int][]int{1: {4}}) {
		t.Errorf("after a restart: %v", got)
	}
}
