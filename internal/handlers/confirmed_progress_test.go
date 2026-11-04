package handlers_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestFinishingConfirmedFileMarksItsCanonicalEpisodeRange(t *testing.T) {
	users := store.NewMemory()
	if err := users.MarkEpisodes(t.Context(), "viewer", 95396, 1, []int{2}, true); err != nil {
		t.Fatal(err)
	}
	if err := users.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4,
	}); err != nil {
		t.Fatal(err)
	}
	h, cookie := confirmedProgress(t, users, users, "viewer")
	torrent := torrserver.Torrent{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Title: "Example series",
		Data:      `{"tmdb":{"id":95396,"type":"tv"}}`,
		FileStats: []torrserver.FileStat{{ID: 1, Path: "Show/episode.mkv"}},
	}
	if res := reportConfirmedProgress(t, h, cookie, torrent, 1, 600); res.Code != http.StatusNoContent {
		t.Fatalf("unfinished report = %d: %s", res.Code, res.Body)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || !reflect.DeepEqual(got, map[int][]int{1: {2}}) {
		t.Fatalf("unfinished file changed episodes: %v, err=%v", got, err)
	}
	for range 2 {
		if res := reportConfirmedProgress(t, h, cookie, torrent, 1, 3500); res.Code != http.StatusNoContent {
			t.Fatalf("finished report = %d: %s", res.Code, res.Body)
		}
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || !reflect.DeepEqual(got, map[int][]int{1: {2}, 2: {3, 4}}) {
		t.Errorf("finished confirmed range = %v, err=%v; want S01E02 and S02E03–04", got, err)
	}
	if got, found, err := users.Progress(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got.Position != 3500*time.Second || got.File != "Show/episode.mkv" {
		t.Errorf("file resume changed: %+v, found=%v, err=%v", got, found, err)
	}
	if got, err := users.TitleState(t.Context(), "viewer", "tv", 95396); err != nil || got.State != "" {
		t.Errorf("episode completion marked the whole series: %+v, err=%v", got, err)
	}
}

func TestConfirmedEpisodeMappingOverridesFilenameWithoutCrossingFileContext(t *testing.T) {
	for _, c := range []struct {
		name, owner, hash, file, data, storedFile string
		index                                     int
		want                                      map[int][]int
	}{
		{
			name: "confirmed overrides filename", owner: "viewer", hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			file: "Show.S01E02.mkv", storedFile: "Show.S01E02.mkv", data: `{"tmdb":{"id":95396,"type":"tv"}}`, index: 1,
			want: map[int][]int{2: {3, 4}},
		},
		{
			name: "another account", owner: "other-viewer", hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			file: "Show/episode.mkv", storedFile: "Show/episode.mkv", data: `{"tmdb":{"id":95396,"type":"tv"}}`, index: 1,
			want: map[int][]int{},
		},
		{
			name: "another hash", owner: "viewer", hash: "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
			file: "Show/episode.mkv", storedFile: "Show/episode.mkv", data: `{"tmdb":{"id":95396,"type":"tv"}}`, index: 1,
			want: map[int][]int{},
		},
		{
			name: "another index", owner: "viewer", hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			file: "Show/episode.mkv", storedFile: "Show/episode.mkv", data: `{"tmdb":{"id":95396,"type":"tv"}}`, index: 2,
			want: map[int][]int{},
		},
		{
			name: "changed path with misleading filename", owner: "viewer", hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			file: "Changed.S01E02.mkv", storedFile: "Show/episode.mkv", data: `{"tmdb":{"id":95396,"type":"tv"}}`, index: 1,
			want: map[int][]int{},
		},
		{
			name: "changed series", owner: "viewer", hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			file: "Show/episode.mkv", storedFile: "Show/episode.mkv", data: `{"tmdb":{"id":99999,"type":"tv"}}`, index: 1,
			want: map[int][]int{},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			users := store.NewMemory()
			if err := users.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
				Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
				File: c.storedFile, TMDBID: 95396, Season: 2, First: 3, Last: 4,
			}); err != nil {
				t.Fatal(err)
			}
			h, cookie := confirmedProgress(t, users, users, c.owner)
			torrent := torrserver.Torrent{
				Hash: c.hash, Title: "Example series", Data: c.data,
				FileStats: []torrserver.FileStat{{ID: c.index, Path: c.file}},
			}
			if res := reportConfirmedProgress(t, h, cookie, torrent, c.index, 3500); res.Code != http.StatusNoContent {
				t.Fatalf("completion = %d: %s", res.Code, res.Body)
			}
			got, err := users.WatchedEpisodes(t.Context(), c.owner, 95396)
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("canonical episodes = %v, err=%v; want %v", got, err, c.want)
			}
			if got, err := users.WatchedEpisodes(t.Context(), c.owner, 99999); err != nil || len(got) != 0 {
				t.Errorf("mapped a different series: %v, err=%v", got, err)
			}
			if c.owner != "viewer" {
				if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || len(got) != 0 {
					t.Errorf("another account's report changed the mapping owner's history: %v, err=%v", got, err)
				}
			}
		})
	}
}

func TestMappingLookupFailurePreservesProgressAndMarksNoFilenameEpisode(t *testing.T) {
	// A real closed external database client makes lookup fail without
	// replacing the episode service or any other internal domain package.
	t.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:1")
	unavailable, err := store.NewFirestore(t.Context(), "demo-mapping-unavailable", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	if err := unavailable.Close(); err != nil {
		t.Fatal(err)
	}
	users := store.NewMemory()
	h, cookie := confirmedProgress(t, users, unavailable, "viewer")
	torrent := torrserver.Torrent{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Title: "Example series",
		Data: `{"tmdb":{"id":95396,"type":"tv"}}`, FileStats: []torrserver.FileStat{{ID: 1, Path: "Show.S01E02.mkv"}},
	}
	res := reportConfirmedProgress(t, h, cookie, torrent, 1, 3500)
	if res.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable confirmation lookup = %d: %s; want 503", res.Code, res.Body)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || len(got) != 0 {
		t.Errorf("failed lookup fell back to filename: %v, err=%v", got, err)
	}
	if got, found, err := users.Progress(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got.Position != 3500*time.Second {
		t.Errorf("failed lookup lost file progress: %+v, found=%v, err=%v", got, found, err)
	}
}

func TestConfirmingAFileDoesNotBackfillItsEarlierCompletion(t *testing.T) {
	users := store.NewMemory()
	h, cookie := confirmedProgress(t, users, users, "viewer")
	if res := reportConfirmedProgress(t, h, cookie, torrserver.Torrent{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Title: "Example series",
		Data: `{"tmdb":{"id":95396,"type":"tv"}}`, FileStats: []torrserver.FileStat{{ID: 1, Path: "Show/episode.mkv"}},
	}, 1, 3500); res.Code != http.StatusNoContent {
		t.Fatalf("earlier completion = %d: %s", res.Code, res.Body)
	}
	if err := users.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4,
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || len(got) != 0 {
		t.Errorf("confirmation backfilled watched episodes: %v, err=%v", got, err)
	}
}

func TestUnconfirmedFilesKeepExactFilenameBehavior(t *testing.T) {
	for _, c := range []struct {
		file string
		want map[int][]int
	}{
		{"Show.S01E02.mkv", map[int][]int{1: {2}}},
		{"Show/episode.mkv", map[int][]int{}},
	} {
		t.Run(c.file, func(t *testing.T) {
			users := store.NewMemory()
			h, cookie := confirmedProgress(t, users, users, "viewer")
			if res := reportConfirmedProgress(t, h, cookie, torrserver.Torrent{
				Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Title: "Example series",
				Data: `{"tmdb":{"id":95396,"type":"tv"}}`, FileStats: []torrserver.FileStat{{ID: 1, Path: c.file}},
			}, 1, 3500); res.Code != http.StatusNoContent {
				t.Fatalf("completion = %d: %s", res.Code, res.Body)
			}
			if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("filename episodes = %v, err=%v; want %v", got, err, c.want)
			}
		})
	}
}

func confirmedProgress(t *testing.T, users *store.Memory, mappings store.EpisodeMappingStore, uid string, episodeStores ...store.EpisodeStore) (http.Handler, *http.Cookie) {
	t.Helper()
	sessions := auth.NewSessionManager(8, rand.Reader)
	t.Cleanup(sessions.Close)
	token, err := sessions.CreateSession(&auth.User{Username: uid, Name: uid})
	if err != nil {
		t.Fatal(err)
	}
	user := func(r *http.Request) *auth.User {
		cookie, err := r.Cookie(auth.SessionCookieName)
		if err != nil {
			return nil
		}
		return sessions.GetUser(cookie.Value)
	}
	owner := func(r *http.Request) (string, bool) {
		if u := user(r); u != nil {
			return u.Username, true
		}
		return "", false
	}
	catalog := handlers.NewCatalog(func() *sources.Clients { return &sources.Clients{} }, user, "/login", time.Second)
	var episodeStore store.EpisodeStore = users
	if len(episodeStores) > 0 {
		episodeStore = episodeStores[0]
	}
	episodes := handlers.NewEpisodes(episodeStore, owner, catalog)
	episodes.UseFileMappings(mappings)
	progress := handlers.NewProgress(users, owner, nil)
	progress.MarkFinishedEpisodesIn(episodes)
	progress.MarkFinishedMoviesIn(handlers.NewTitleStates(users, owner, catalog))
	mux := http.NewServeMux()
	progress.Register(mux)
	return mux, &http.Cookie{
		Name: auth.SessionCookieName, Value: token,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}

func TestFailedEpisodeHistoryWriteKeepsLiveHistoryAndCanBeRetried(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	episodes, err := store.OpenEpisodeFile(filepath.Join(dir, "episodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := episodes.MarkEpisodes(t.Context(), "viewer", 95396, 1, []int{2}, true); err != nil {
		t.Fatal(err)
	}
	users := store.NewMemory()
	if err := users.SaveEpisodeMapping(t.Context(), "viewer", store.EpisodeMapping{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4,
	}); err != nil {
		t.Fatal(err)
	}
	h, cookie := confirmedProgress(t, users, users, "viewer", episodes)
	torrent := torrserver.Torrent{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Title: "Example series",
		Data: `{"tmdb":{"id":95396,"type":"tv"}}`, FileStats: []torrserver.FileStat{{ID: 1, Path: "Show/episode.mkv"}},
	}
	backup := dir + "-moved"
	if err := os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(backup); err == nil {
			_ = os.Rename(backup, dir)
		}
	})
	if res := reportConfirmedProgress(t, h, cookie, torrent, 1, 3500); res.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable episode history = %d: %s", res.Code, res.Body)
	}
	if got, err := episodes.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || !reflect.DeepEqual(got, map[int][]int{1: {2}}) {
		t.Errorf("failed persistence changed live history: %v, err=%v", got, err)
	}
	if err := os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if res := reportConfirmedProgress(t, h, cookie, torrent, 1, 3500); res.Code != http.StatusNoContent {
			t.Fatalf("retry = %d: %s", res.Code, res.Body)
		}
	}
	reopened, err := store.OpenEpisodeFile(filepath.Join(dir, "episodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || !reflect.DeepEqual(got, map[int][]int{1: {2}, 2: {3, 4}}) {
		t.Errorf("retried history = %v, err=%v", got, err)
	}
}

func reportConfirmedProgress(t *testing.T, h http.Handler, cookie *http.Cookie, torrent torrserver.Torrent, index, position int) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"hash": torrent.Hash, "index": index, "position": position, "duration": 3600, "torrent": torrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/progress", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
