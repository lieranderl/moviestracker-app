package store_test

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// eachFilterStore runs check against every store that keeps release
// filters: the web app's stores and the local app's file.
func eachFilterStore(t *testing.T, check func(t *testing.T, s store.ReleaseFilterStore, uid string)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) { check(t, s, uid) })
	t.Run("file", func(t *testing.T) {
		s, err := store.OpenFilterFile(filepath.Join(t.TempDir(), "filters.json"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "user-"+rand.Text())
	})
}

func TestAUsersReleaseFiltersAreKept(t *testing.T) {
	eachFilterStore(t, func(t *testing.T, s store.ReleaseFilterStore, uid string) {
		ctx := context.Background()
		if got, err := s.ReleaseFilters(ctx, uid); err != nil || got.Sort != "" || len(got.Qualities) != 0 {
			t.Fatalf("a new user's filters = %+v, %v; want none", got, err)
		}
		want := store.ReleaseFilters{Sort: "bitrate", Qualities: []int{2160, 1080}, HDR: true, HideRecordings: true}
		if err := s.SaveReleaseFilters(ctx, uid, want); err != nil {
			t.Fatal(err)
		}
		got, err := s.ReleaseFilters(ctx, uid)
		if err != nil || got.Sort != "bitrate" || !slices.Equal(got.Qualities, []int{2160, 1080}) || !got.HDR || !got.HideRecordings {
			t.Errorf("ReleaseFilters() = %+v, %v; want bitrate, 4K and 1080p, HDR, no recordings", got, err)
		}
	})
}

func TestReleaseFiltersAreChecked(t *testing.T) {
	eachFilterStore(t, func(t *testing.T, s store.ReleaseFilterStore, uid string) {
		for _, f := range []store.ReleaseFilters{{Sort: "random"}, {Sort: "seeders", Qualities: []int{360}}} {
			if err := s.SaveReleaseFilters(context.Background(), uid, f); err == nil {
				t.Errorf("SaveReleaseFilters(%+v) kept it", f)
			}
		}
	})
}

func TestReleaseFiltersLeaveTheLanguageAlone(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		if err := s.SavePreferences(ctx, uid, store.Preferences{Language: "ru"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveReleaseFilters(ctx, uid, store.ReleaseFilters{Sort: "size"}); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Preferences(ctx, uid); err != nil || got.Language != "ru" {
			t.Errorf("Preferences() after saving filters = %+v, %v; want Russian still", got, err)
		}
	})
}
