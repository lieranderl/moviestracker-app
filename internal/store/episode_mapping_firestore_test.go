package store_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestAccountExportAndDeletionIncludeConfirmedMappings(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, clock *time.Time) {
		mappings, ok := s.(store.EpisodeMappingStore)
		if !ok {
			t.Fatal("account store cannot retain confirmed mappings")
		}
		old := store.WithAccountSession(t.Context(), uid, *clock)
		want := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}
		if err := mappings.SaveEpisodeMapping(old, uid, want); err != nil {
			t.Fatal(err)
		}
		account, err := s.ExportAccount(t.Context(), uid)
		if err != nil {
			t.Fatal(err)
		}
		got := exportedEpisodeMappings(t, account)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("account export mappings = %+v; want only %+v", got, want)
		}
		other, err := s.ExportAccount(t.Context(), uid+"-other")
		if err != nil {
			t.Fatal(err)
		}
		if got := exportedEpisodeMappings(t, other); len(got) != 0 {
			t.Errorf("another account exported private confirmations: %+v", got)
		}
		if err := s.DeleteAccount(old, uid); err != nil {
			t.Fatal(err)
		}
		if got, found, err := mappings.EpisodeMapping(t.Context(), uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || found {
			t.Errorf("deleted account retained %+v, found=%v, err=%v", got, found, err)
		}
		if err := mappings.SaveEpisodeMapping(old, uid, want); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("deleted session recreated a confirmation: %v", err)
		}
		*clock = clock.Add(time.Second)
		fresh := store.WithAccountSession(t.Context(), uid, *clock)
		if err := mappings.SaveEpisodeMapping(fresh, uid, want); err != nil {
			t.Fatal(err)
		}
		if err := mappings.ClearEpisodeMapping(old, uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); !errors.Is(err, store.ErrAccountDeleted) {
			t.Errorf("deleted session could clear a new confirmation: %v", err)
		}
		if got, found, err := mappings.EpisodeMapping(fresh, uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got != want {
			t.Errorf("new session confirmation changed: %+v, found=%v, err=%v", got, found, err)
		}
	})
}

func exportedEpisodeMappings(t *testing.T, account store.Account) []store.EpisodeMapping {
	t.Helper()
	data, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	var export struct {
		Mappings []store.EpisodeMapping `json:"episodeMappings"`
	}
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatal(err)
	}
	return export.Mappings
}

func TestResettingResumePreservesConfirmedEpisodeMapping(t *testing.T) {
	eachStore(t, func(t *testing.T, s store.Store, uid string, _ *time.Time) {
		want := store.EpisodeMapping{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", TMDBID: 95396, Season: 2, First: 3, Last: 4,
		}
		if err := s.SaveEpisodeMapping(t.Context(), uid, want); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveProgress(t.Context(), uid, store.Progress{
			Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
			File: "episode.mp4", Kind: "tv", TMDBID: 95396,
			Position: 10 * time.Minute, Duration: 30 * time.Minute,
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.ResetProgress(t.Context(), uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil {
			t.Fatal(err)
		}
		if recent, err := s.RecentProgress(t.Context(), uid, 12); err != nil || len(recent) != 0 {
			t.Errorf("reset file still appears in Continue watching: %+v, err=%v", recent, err)
		}
		if got, found, err := s.EpisodeMapping(t.Context(), uid, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got != want {
			t.Errorf("resume reset changed confirmed mapping: %+v, found=%v, err=%v", got, found, err)
		}
	})
}
