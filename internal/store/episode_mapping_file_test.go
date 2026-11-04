package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestConfirmedMappingSurvivesReopenAndFailedWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "episode-mappings.json")
	s, err := store.OpenEpisodeMappingFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := store.EpisodeMapping{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
	}
	if err := s.SaveEpisodeMapping(t.Context(), "viewer", want); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.OpenEpisodeMappingFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := reopened.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
	if err != nil || !found || got != want {
		t.Fatalf("reopened confirmation = %+v, found=%v, err=%v; want %+v", got, found, err, want)
	}
	backup := dir + "-moved"
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	changed := want
	changed.First, changed.Last = 5, 6
	if err := s.SaveEpisodeMapping(t.Context(), "viewer", changed); err == nil {
		t.Error("reported a successful confirmation without persistence")
	}
	if err := s.ClearEpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err == nil {
		t.Error("reported a successful clear without persistence")
	}
	if err := s.DeleteAccountMappings(t.Context(), "viewer"); err == nil {
		t.Error("reported successful account cleanup without persistence")
	}
	got, found, err = s.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
	if err != nil || !found || got != want {
		t.Errorf("failed write changed live confirmation: %+v, found=%v, err=%v", got, found, err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	reopened, err = store.OpenEpisodeMappingFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err = reopened.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
	if err != nil || !found || got != want {
		t.Errorf("failed write changed persisted confirmation: %+v, found=%v, err=%v", got, found, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("confirmed file mapping is accessible to other accounts: %v", info.Mode())
		}
	}
	if err := s.ClearEpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil {
		t.Fatal(err)
	}
	reopened, err = store.OpenEpisodeMappingFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := reopened.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || found {
		t.Errorf("cleared mapping returned after reopening: %+v, found=%v, err=%v", got, found, err)
	}
}

func TestConfirmedMappingAccountCleanupPreservesOtherAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mappings.json")
	s, err := store.OpenEpisodeMappingFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"viewer", "other-viewer"} {
		if err := s.SaveEpisodeMapping(t.Context(), uid, store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := s.DeleteAccountMappings(t.Context(), "viewer"); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := store.OpenEpisodeMappingFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := reopened.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || found {
		t.Errorf("deleted account mapping survived: found=%v, err=%v", found, err)
	}
	if got, found, err := reopened.EpisodeMapping(t.Context(), "other-viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got.First != 3 || got.Last != 4 {
		t.Errorf("other account mapping changed: %+v, found=%v, err=%v", got, found, err)
	}
}
