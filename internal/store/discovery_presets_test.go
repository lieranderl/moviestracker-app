package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func eachPresetStore(t *testing.T, check func(*testing.T, store.DiscoveryPresetStore, string, *time.Time)) {
	t.Helper()
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) { check(t, s, uid, clock) })
	t.Run("file", func(t *testing.T) {
		clock := now
		s, err := store.OpenDiscoveryPresetFile(filepath.Join(t.TempDir(), "presets.json"), store.WithClock(func() time.Time { return clock }))
		if err != nil {
			t.Fatal(err)
		}
		check(t, s, "alex", &clock)
	})
}

func TestAUserKeepsNamedDiscoveryFilters(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, _ *time.Time) {
		ctx := context.Background()
		filters := store.DiscoveryFilters{Kind: "movie", Genre: 878, Year: 2021, MinRating: 7, Sort: "rating", MaxRuntime: 120, Language: "fr"}
		made, err := s.CreateDiscoveryPreset(ctx, uid, "  Friday night  ", filters)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 1 {
			t.Fatalf("saved presets = %v, %v; want one", got, err)
		}
		if made.ID == "" || made.CreatedAt.IsZero() || got[0] != made || got[0].Name != "Friday night" || got[0].Filters != filters {
			t.Errorf("saved preset = %+v; want named filters and stable identity", got[0])
		}
	})
}

func TestAUserRenamesThenRemovesOnlyTheirChosenPreset(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, _ *time.Time) {
		ctx := context.Background()
		first, err := s.CreateDiscoveryPreset(ctx, uid, "Friday", store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.CreateDiscoveryPreset(ctx, uid, "Sunday", store.DiscoveryFilters{Kind: "tv", Sort: "newest"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RenameDiscoveryPreset(ctx, uid, first.ID, "  Cinema night  "); err != nil {
			t.Fatal(err)
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 2 {
			t.Fatalf("renamed presets = %v, %v; want two", got, err)
		}
		for _, p := range got {
			if p.ID == first.ID && (p.Name != "Cinema night" || p.Filters != first.Filters || !p.CreatedAt.Equal(first.CreatedAt)) {
				t.Errorf("renaming changed preset filters or creation identity: %+v", p)
			}
		}
		for range 2 {
			if err := s.DeleteDiscoveryPreset(ctx, uid, first.ID); err != nil {
				t.Fatal(err)
			}
		}
		got, err = s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 1 || got[0] != second {
			t.Errorf("remaining presets = %v, %v; want only Sunday", got, err)
		}
	})
}

func TestAnAccountKeepsAtMostTwentyDiscoveryPresets(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, _ *time.Time) {
		ctx := context.Background()
		for i := range 20 {
			if _, err := s.CreateDiscoveryPreset(ctx, uid, fmt.Sprintf("Evening %d", i), store.DiscoveryFilters{Kind: "movie", Sort: "popular"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.CreateDiscoveryPreset(ctx, uid, "Too many", store.DiscoveryFilters{Kind: "movie", Sort: "popular"}); err == nil {
			t.Error("an account created a twenty-first preset")
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 20 {
			t.Errorf("presets after the limit = %d, %v; want 20", len(got), err)
		}
	})
}

func TestDiscoveryPresetsRejectInvalidNamesAndFilters(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, _ *time.Time) {
		ctx := context.Background()
		valid := store.DiscoveryFilters{Kind: "movie", Sort: "popular"}
		for _, name := range []string{"", " \t ", strings.Repeat("夜", 61), "one\ntwo"} {
			if _, err := s.CreateDiscoveryPreset(ctx, uid, name, valid); err == nil {
				t.Errorf("invalid preset name %q was kept", name)
			}
		}
		for _, filters := range []store.DiscoveryFilters{
			{Kind: "book"}, {Kind: "movie", Genre: -1}, {Kind: "movie", Year: -1},
			{Kind: "movie", MinRating: math.NaN()}, {Kind: "movie", MinRating: 11},
			{Kind: "movie", MaxRuntime: -1}, {Kind: "movie", Language: "../"},
		} {
			if _, err := s.CreateDiscoveryPreset(ctx, uid, "Invalid filters", filters); err == nil {
				t.Errorf("invalid preset filters %+v were kept", filters)
			}
		}
		made, err := s.CreateDiscoveryPreset(ctx, uid, strings.Repeat("夜", 60), valid)
		if err != nil {
			t.Fatalf("60 Unicode characters must fit: %v", err)
		}
		if err := s.RenameDiscoveryPreset(ctx, uid, made.ID, " "); err == nil {
			t.Error("renaming a preset to an empty name succeeded")
		}
		if _, err := s.CreateDiscoveryPreset(ctx, "", "No owner", valid); err == nil {
			t.Error("a preset without an account was kept")
		}
	})
}

func TestPresetsArePrivateCopiesListedNewestFirst(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, clock *time.Time) {
		ctx := context.Background()
		var latest store.DiscoveryPreset
		for _, name := range []string{"Friday", "Saturday", "Sunday"} {
			*clock = clock.Add(time.Minute)
			var err error
			latest, err = s.CreateDiscoveryPreset(ctx, uid, name, store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 3 {
			t.Fatalf("presets = %v, %v", got, err)
		}
		if got[0].Name != "Sunday" || got[1].Name != "Saturday" || got[2].Name != "Friday" {
			t.Errorf("presets are not newest first: %v", got)
		}
		got[0].Name, got[0].Filters.Kind = "Changed copy", "tv"
		again, err := s.DiscoveryPresets(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range again {
			if p.ID == latest.ID && p != latest {
				t.Error("changing returned data changed the saved preset")
			}
		}
		other := uid + "-other"
		if theirs, err := s.DiscoveryPresets(ctx, other); err != nil || len(theirs) != 0 {
			t.Errorf("another account sees presets: %v, %v", theirs, err)
		}
		if err := s.RenameDiscoveryPreset(ctx, other, latest.ID, "Stolen"); !errors.Is(err, store.ErrPresetNotFound) {
			t.Errorf("foreign rename = %v; want not found", err)
		}
		if err := s.DeleteDiscoveryPreset(ctx, other, latest.ID); err != nil {
			t.Fatal(err)
		}
		kept, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(kept) != 3 {
			t.Errorf("foreign deletion changed the owner: %v, %v", kept, err)
		}
	})
}

func TestTheLocalAppKeepsPresetsAcrossRestartsAndFailedWrites(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "presets.json")
	s, err := store.OpenDiscoveryPresetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	made, err := s.CreateDiscoveryPreset(ctx, "alex", "Friday", store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.OpenDiscoveryPresetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.DiscoveryPresets(ctx, "alex")
	if err != nil || len(got) != 1 || got[0] != made {
		t.Fatalf("after restart = %v, %v", got, err)
	}
	backup := dir + "-moved"
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDiscoveryPreset(ctx, "alex", "Lost create", made.Filters); err == nil {
		t.Error("create reported success without persistence")
	}
	if err := s.RenameDiscoveryPreset(ctx, "alex", made.ID, "Lost rename"); err == nil {
		t.Error("rename reported success without persistence")
	}
	if err := s.DeleteDiscoveryPreset(ctx, "alex", made.ID); err == nil {
		t.Error("delete reported success without persistence")
	}
	got, err = s.DiscoveryPresets(ctx, "alex")
	if err != nil || len(got) != 1 || got[0] != made {
		t.Errorf("failed writes changed the visible saved data: %v, %v", got, err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameDiscoveryPreset(ctx, "alex", made.ID, "Retry works"); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.OpenDiscoveryPresetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = reopened.DiscoveryPresets(ctx, "alex")
	if err != nil || len(got) != 1 || got[0].Name != "Retry works" {
		t.Errorf("retry after persistence recovery = %v, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows exposes the read-only attribute here, not Unix owner/group permissions.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("preset file must be owner-only: %v", info.Mode().Perm())
	}
}

func TestConcurrentPresetCreatesCannotExceedTheAccountLimit(t *testing.T) {
	eachPresetStore(t, func(t *testing.T, s store.DiscoveryPresetStore, uid string, _ *time.Time) {
		ctx := context.Background()
		filters := store.DiscoveryFilters{Kind: "movie", Sort: "popular"}
		for i := range 19 {
			if _, err := s.CreateDiscoveryPreset(ctx, uid, fmt.Sprintf("Night %d", i), filters); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan error, 4)
		for range 4 {
			wg.Go(func() { _, err := s.CreateDiscoveryPreset(ctx, uid, "Last slot", filters); results <- err })
		}
		wg.Wait()
		close(results)
		saved := 0
		for err := range results {
			if err == nil {
				saved++
			} else if !errors.Is(err, store.ErrPresetLimit) {
				t.Errorf("concurrent create failed unexpectedly: %v", err)
			}
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if saved != 1 || err != nil || len(got) != 20 {
			t.Errorf("concurrent creates saved %d, total %d, %v; want one final slot", saved, len(got), err)
		}
	})
}

func TestAccountExportAndDeletionIncludeDiscoveryPresets(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		ctx := context.Background()
		made, err := s.CreateDiscoveryPreset(ctx, uid, "Friday", store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
		if err != nil {
			t.Fatal(err)
		}
		exported, err := s.ExportAccount(ctx, uid)
		if err != nil || len(exported.DiscoveryPresets) != 1 || exported.DiscoveryPresets[0] != made {
			t.Errorf("export omitted saved filters: %+v, %v", exported, err)
		}
		if err := s.DeleteAccount(ctx, uid); err != nil {
			t.Fatal(err)
		}
		got, err := s.DiscoveryPresets(ctx, uid)
		if err != nil || len(got) != 0 {
			t.Errorf("deleted account retained presets: %v, %v", got, err)
		}
	})
}

func TestARevokedSessionCannotChangeDiscoveryPresets(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		old := store.WithAccountSession(context.Background(), uid, *clock)
		*clock = clock.Add(time.Second)
		if err := s.DeleteAccount(context.Background(), uid); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(time.Second)
		made, err := s.CreateDiscoveryPreset(context.Background(), uid, "Fresh account", store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateDiscoveryPreset(old, uid, "Old session", made.Filters); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("revoked create = %v", err)
		}
		if err := s.RenameDiscoveryPreset(old, uid, made.ID, "Old session"); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("revoked rename = %v", err)
		}
		if err := s.DeleteDiscoveryPreset(old, uid, made.ID); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("revoked delete = %v", err)
		}
		got, err := s.DiscoveryPresets(context.Background(), uid)
		if err != nil || len(got) != 1 || got[0] != made {
			t.Errorf("revoked operations changed the new account: %v, %v", got, err)
		}
	})
}

func TestAnEmptyJSONPresetFileCanAcceptSavedFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenDiscoveryPresetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDiscoveryPreset(t.Context(), "alex", "Friday", store.DiscoveryFilters{Kind: "movie"}); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPresetChangesPreserveUnknownStoredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	const seed = `{"alex":{"first":{"id":"first","name":"Friday","filters":{"type":"movie","sort":"popular","futureFilter":"kept"},"createdAt":"2026-11-02T12:00:00Z","futurePreset":{"value":"kept"}}},"anna":{"other":{"id":"other","name":"Private","filters":{"type":"tv"},"futurePreset":"kept"}}}`
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenDiscoveryPresetFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameDiscoveryPreset(t.Context(), "alex", "first", "Sunday"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateDiscoveryPreset(t.Context(), "alex", "New", store.DiscoveryFilters{Kind: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDiscoveryPreset(t.Context(), "alex", p.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the test-owned temporary preset file
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["alex"]["first"]["futurePreset"]) != `{"value":"kept"}` || string(got["anna"]["other"]["futurePreset"]) != `"kept"` || !strings.Contains(string(got["alex"]["first"]["filters"]), `"futureFilter":"kept"`) {
		t.Errorf("future fields lost: %s", data)
	}
}
