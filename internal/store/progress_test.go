package store_test

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

const duneHash = "0123456789abcdef0123456789abcdef01234567"

var duneFile = store.Progress{
	Hash: duneHash, Index: 1, File: "Dune.2021.2160p.mkv",
	Title: "Dune", Poster: "/d5NXSklXo0qyIYkgV94XAgMIckC.jpg", Kind: "movie", TMDBID: 438631,
	Position: 2530 * time.Second, Duration: 9360 * time.Second,
}

// eachProgressStore runs check against every store that keeps progress: the
// web app's stores and the local app's file in its data directory.
func eachProgressStore(t *testing.T, check func(t *testing.T, s store.ProgressStore, uid string, clock *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenProgressFile(filepath.Join(t.TempDir(), "progress.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "user-"+rand.Text(), &clock)
	})
}

func TestWhereAUserStoppedWatchingIsKept(t *testing.T) {
	eachProgressStore(t, func(t *testing.T, s store.ProgressStore, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.SaveProgress(ctx, uid, duneFile); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.Progress(ctx, uid, duneHash, 1)
		if err != nil || !ok {
			t.Fatalf("Progress() = %v, %v; want kept", ok, err)
		}
		if got.Position != 2530*time.Second || got.Duration != 9360*time.Second || got.Title != "Dune" || got.TMDBID != 438631 {
			t.Errorf("Progress() = %+v, want Dune at 42:10 of 2:36:00", got)
		}
		if !got.UpdatedAt.Equal(now) {
			t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
		}
	})
}

func TestContinueWatchingListsUnfinishedFilesNewestFirst(t *testing.T) {
	eachProgressStore(t, func(t *testing.T, s store.ProgressStore, uid string, clock *time.Time) {
		ctx := context.Background()
		arrival := store.Progress{Hash: "1111111111111111111111111111111111111111", Index: 1, Title: "Arrival", Position: 6700 * time.Second, Duration: 6960 * time.Second}
		severance := store.Progress{Hash: "2222222222222222222222222222222222222222", Index: 3, Title: "Severance", Kind: "tv", TMDBID: 95396, Position: 600 * time.Second, Duration: 3300 * time.Second}
		for _, p := range []store.Progress{duneFile, arrival, severance} {
			if err := s.SaveProgress(ctx, uid, p); err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(time.Minute)
		}
		got, err := s.RecentProgress(ctx, uid, 10)
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, p := range got {
			titles = append(titles, p.Title)
		}
		if len(titles) != 2 || titles[0] != "Severance" || titles[1] != "Dune" {
			t.Errorf("RecentProgress() titles = %q, want [Severance Dune] (Arrival, 96%% watched, is finished)", titles)
		}
	})
}

func TestTheLocalAppKeepsProgressAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	ctx := context.Background()
	first, err := store.OpenProgressFile(path, store.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SaveProgress(ctx, "ann", duneFile); err != nil {
		t.Fatal(err)
	}
	again, err := store.OpenProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := again.Progress(ctx, "ann", duneHash, 1)
	if err != nil || !ok || got.Position != 2530*time.Second || got.Title != "Dune" {
		t.Errorf("after a restart Progress() = %+v, %v, %v; want Dune at 42:10", got, ok, err)
	}
	if mine, _, _ := again.Progress(ctx, "bob", duneHash, 1); mine.Title != "" {
		t.Error("another account sees Ann's progress")
	}
}

func TestAUserCanResetOneContinueWatchingEntry(t *testing.T) {
	eachProgressStore(t, func(t *testing.T, s store.ProgressStore, uid string, _ *time.Time) {
		ctx := context.Background()
		other := duneFile
		other.Index = 2
		for _, p := range []store.Progress{duneFile, other} {
			if err := s.SaveProgress(ctx, uid, p); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.SaveProgress(ctx, uid+"-other", duneFile); err != nil {
			t.Fatal(err)
		}
		if err := s.ResetProgress(ctx, uid, duneHash, 1); err != nil {
			t.Fatal(err)
		}
		got, found, err := s.Progress(ctx, uid, duneHash, 1)
		if err != nil || !found || got.Position != 0 || got.Title != "Dune" || got.Duration != 9360*time.Second {
			t.Errorf("reset entry = %+v, %v, %v; want zero resume position and preserved metadata", got, found, err)
		}
		recent, err := s.RecentProgress(ctx, uid, 10)
		if err != nil || len(recent) != 1 || recent[0].Index != 2 {
			t.Errorf("Continue watching after reset = %+v, %v; want only the other file", recent, err)
		}
		untouched, _, err := s.Progress(ctx, uid+"-other", duneHash, 1)
		if err != nil || untouched.Position != 2530*time.Second {
			t.Errorf("another user's progress changed: %+v, %v", untouched, err)
		}
	})
}

func TestProgressResetPersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	ctx := context.Background()
	s, err := store.OpenProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveProgress(ctx, "ann", duneFile); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetProgress(ctx, "ann", duneHash, 1); err != nil {
		t.Fatal(err)
	}
	again, err := store.OpenProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := again.Progress(ctx, "ann", duneHash, 1)
	if err != nil || !found || got.Position != 0 {
		t.Fatalf("after restart reset = %+v, %v, %v", got, found, err)
	}
	recent, err := again.RecentProgress(ctx, "ann", 10)
	if err != nil || len(recent) != 0 {
		t.Errorf("reset entry returned after restart: %+v, %v", recent, err)
	}
}

func TestProgressResetIsIdempotentAndChecksFileIdentity(t *testing.T) {
	eachProgressStore(t, func(t *testing.T, s store.ProgressStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for _, file := range []struct {
			hash  string
			index int
		}{{"invalid", 1}, {duneHash, 0}} {
			if err := s.ResetProgress(ctx, uid, file.hash, file.index); err == nil {
				t.Errorf("reset invalid file %+v succeeded", file)
			}
		}
		if err := s.ResetProgress(ctx, uid, duneHash, 1); err != nil {
			t.Fatal(err)
		}
		if _, found, err := s.Progress(ctx, uid, duneHash, 1); err != nil || found {
			t.Fatalf("reset created a missing progress entry: %v, %v", found, err)
		}
		if err := s.SaveProgress(ctx, uid, duneFile); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := s.ResetProgress(ctx, uid, duneHash, 1); err != nil {
				t.Fatal(err)
			}
		}
		played := duneFile
		played.Position = 5 * time.Minute
		if err := s.SaveProgress(ctx, uid, played); err != nil {
			t.Fatal(err)
		}
		recent, err := s.RecentProgress(ctx, uid, 10)
		if err != nil || len(recent) != 1 || recent[0].Position != 5*time.Minute {
			t.Errorf("new playback did not restore the entry: %+v, %v", recent, err)
		}
	})
}

func TestAFailedLocalResetKeepsTheSavedPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	ctx := context.Background()
	s, err := store.OpenProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveProgress(ctx, "ann", duneFile); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetProgress(ctx, "ann", duneHash, 1); err == nil {
		t.Fatal("reset reported success when the destination is a directory")
	}
	got, found, err := s.Progress(ctx, "ann", duneHash, 1)
	if err != nil || !found || got.Position != 2530*time.Second {
		t.Errorf("failed reset changed saved position: %+v, %v, %v", got, found, err)
	}
}
