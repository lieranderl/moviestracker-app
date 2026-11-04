package store_test

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestConfirmedEpisodeMappingBelongsToOneAccountAndFile(t *testing.T) {
	eachEpisodeMappingStore(t, func(t *testing.T, s store.EpisodeMappingStore) {
		ctx := t.Context()
		want := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}
		if err := s.SaveEpisodeMapping(ctx, "viewer", want); err != nil {
			t.Fatal(err)
		}
		got, found, err := s.EpisodeMapping(ctx, "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
		if err != nil || !found || got != want {
			t.Fatalf("confirmed range = %+v, found=%v, err=%v; want %+v", got, found, err, want)
		}
		for _, other := range []struct {
			owner string
			hash  string
			index int
		}{
			{"other-viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1},
			{"viewer", "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222", 1},
			{"viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 2},
		} {
			if got, found, err := s.EpisodeMapping(ctx, other.owner, other.hash, other.index); err != nil || found {
				t.Errorf("another account/file read %+v, found=%v, err=%v", got, found, err)
			}
		}
	})
}

func TestConfirmedEpisodeMappingCanBeClearedWithoutAffectingOtherFiles(t *testing.T) {
	eachEpisodeMappingStore(t, func(t *testing.T, s store.EpisodeMappingStore) {
		first := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}
		second := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 2,
			File: "another.mp4", TMDBID: 95396, Season: 2, First: 5, Last: 6,
		}
		for _, m := range []store.EpisodeMapping{first, second} {
			if err := s.SaveEpisodeMapping(t.Context(), "viewer", m); err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			if err := s.ClearEpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil {
				t.Fatal(err)
			}
		}
		if got, found, err := s.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || found {
			t.Errorf("cleared confirmation = %+v, found=%v, err=%v", got, found, err)
		}
		if got, found, err := s.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 2); err != nil || !found || got != second {
			t.Errorf("other file confirmation = %+v, found=%v, err=%v; want %+v", got, found, err, second)
		}
	})
}

func TestConfirmedEpisodeMappingRejectsInvalidFileOrEpisodeContext(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*store.EpisodeMapping)
	}{
		{"invalid hash", func(m *store.EpisodeMapping) { m.Hash = "not-a-hash" }},
		{"zero file ID", func(m *store.EpisodeMapping) { m.Index = 0 }},
		{"missing file path", func(m *store.EpisodeMapping) { m.File = "" }},
		{"oversized file path", func(m *store.EpisodeMapping) { m.File = strings.Repeat("x", 4097) }},
		{"missing series", func(m *store.EpisodeMapping) { m.TMDBID = 0 }},
		{"negative season", func(m *store.EpisodeMapping) { m.Season = -1 }},
		{"zero episode", func(m *store.EpisodeMapping) { m.First = 0 }},
		{"reversed range", func(m *store.EpisodeMapping) { m.Last = 2 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			eachEpisodeMappingStore(t, func(t *testing.T, s store.EpisodeMappingStore) {
				m := store.EpisodeMapping{
					Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
					File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
				}
				c.change(&m)
				if err := s.SaveEpisodeMapping(t.Context(), "viewer", m); err == nil {
					t.Error("accepted invalid file/episode confirmation")
				}
			})
		})
	}
}

func TestConfirmedEpisodeMappingRequiresAnAccount(t *testing.T) {
	eachEpisodeMappingStore(t, func(t *testing.T, s store.EpisodeMappingStore) {
		mapping := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}
		for _, uid := range []string{"", " \t", "other/account"} {
			if err := s.SaveEpisodeMapping(t.Context(), uid, mapping); err == nil {
				t.Errorf("saved a confirmation without an account: %q", uid)
			}
			if _, _, err := s.EpisodeMapping(t.Context(), uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err == nil {
				t.Errorf("read a confirmation without an account: %q", uid)
			}
			if err := s.ClearEpisodeMapping(t.Context(), uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err == nil {
				t.Errorf("cleared a confirmation without a valid account: %q", uid)
			}
		}
	})
}

func eachEpisodeMappingStore(t *testing.T, check func(*testing.T, store.EpisodeMappingStore)) {
	t.Helper()
	names := []string{"memory", "file"}
	if os.Getenv("FIRESTORE_EMULATOR_HOST") != "" {
		names = append(names, "firestore")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if name == "memory" {
				check(t, store.NewMemory())
				return
			}
			if name == "firestore" {
				check(t, episodeMappingFirestore(t))
				return
			}
			s, err := store.OpenEpisodeMappingFile(filepath.Join(t.TempDir(), "mappings.json"))
			if err != nil {
				t.Fatal(err)
			}
			check(t, s)
		})
	}
}

func episodeMappingFirestore(t *testing.T) *store.Firestore {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Fatal("mapping fixture requires the disposable Firestore emulator")
	}
	s, err := store.NewFirestore(t.Context(), "demo-mapping-"+strings.ToLower(rand.Text()[:10]), "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestConfirmedEpisodeMappingSupportsSingleSpecialAndNormalizedIdentity(t *testing.T) {
	eachEpisodeMappingStore(t, func(t *testing.T, s store.EpisodeMappingStore) {
		if err := s.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
			Hash: "AAAA1111AAAA1111AAAA1111AAAA1111AAAA1111", Index: 7,
			File: "special.mp4", TMDBID: 95396, Season: 0, First: 1, Last: 1,
		}); err != nil {
			t.Fatal(err)
		}
		got, found, err := s.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 7)
		want := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 7,
			File: "special.mp4", TMDBID: 95396, Season: 0, First: 1, Last: 1,
		}
		if err != nil || !found || got != want {
			t.Errorf("single special confirmation = %+v, found=%v, err=%v; want %+v", got, found, err, want)
		}
	})
}

func TestConfirmedEpisodeMappingsKeepConcurrentDistinctFileWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mappings.json")
	file, err := store.OpenEpisodeMappingFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backends := map[string]store.EpisodeMappingStore{"memory": store.NewMemory(), "file": file}
	if os.Getenv("FIRESTORE_EMULATOR_HOST") != "" {
		backends["firestore"] = episodeMappingFirestore(t)
	}
	for name, s := range backends {
		t.Run(name, func(t *testing.T) {
			var writes sync.WaitGroup
			errs := make(chan error, 32)
			for index := 1; index <= 32; index++ {
				writes.Go(func() {
					errs <- s.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
						Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: index,
						File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
					})
				})
			}
			writes.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if name == "file" {
				s, err = store.OpenEpisodeMappingFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			for index := 1; index <= 32; index++ {
				got, found, err := s.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", index)
				if err != nil || !found || got.Index != index || got.TMDBID != 95396 || got.First != 3 || got.Last != 4 {
					t.Errorf("file %d confirmation = %+v, found=%v, err=%v", index, got, found, err)
				}
			}
		})
	}
}
