package handlers_test

import (
	"github.com/lieranderl/moviestracker-app/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestUserCanConfirmOneEpisodeOrRange(t *testing.T) {
	users := store.NewMemory()
	if err := users.SaveProgress(t.Context(), "viewer", store.Progress{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "Show/episode.mkv", Kind: "tv", TMDBID: 95396, Position: 600 * time.Second, Duration: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	h, cookie := episodeMappingAPI(t, users, nil)
	params := mappingParams()
	res := mappingRequest(t, h, cookie, http.MethodGet, params)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Example series") {
		t.Fatalf("known file confirmation = %d: %s", res.Code, res.Body)
	}
	params.Set("season", "2")
	params.Set("first", "3")
	params.Set("last", "4")
	res = mappingRequest(t, h, cookie, http.MethodPost, params)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "datastar-patch-elements") {
		t.Fatalf("confirm range = %d: %s", res.Code, res.Body)
	}
	got, found, err := users.EpisodeMapping(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1)
	want := store.EpisodeMapping{
		Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1,
		File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4,
	}
	if err != nil || !found || got != want {
		t.Errorf("saved canonical confirmation = %+v, found=%v, err=%v; want %+v", got, found, err, want)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || len(got) != 0 {
		t.Errorf("saving a confirmation changed watched history: %v, err=%v", got, err)
	}
	if got, found, err := users.Progress(t.Context(), "viewer", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 1); err != nil || !found || got.Position != 600*time.Second {
		t.Errorf("confirmation changed resume: %+v, found=%v, err=%v", got, found, err)
	}
}

func TestUserCanClearConfirmationWithoutReversingHistory(t *testing.T) {
	users := store.NewMemory()
	kept := store.EpisodeMapping{Hash: mappingParams().Get("hash"), Index: 1, File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4}
	if err := users.SaveEpisodeMapping(t.Context(), "viewer", kept); err != nil {
		t.Fatal(err)
	}
	if err := users.MarkEpisodes(t.Context(), "viewer", 95396, 2, []int{3, 4}, true); err != nil {
		t.Fatal(err)
	}
	h, cookie := episodeMappingAPI(t, users, nil)
	res := mappingRequest(t, h, cookie, http.MethodDelete, mappingParams())
	if res.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", res.Code, res.Body)
	}
	if _, found, err := users.EpisodeMapping(t.Context(), "viewer", kept.Hash, 1); err != nil || found {
		t.Fatalf("cleared mapping found=%v err=%v", found, err)
	}
	if got, err := users.WatchedEpisodes(t.Context(), "viewer", 95396); err != nil || len(got[2]) != 2 {
		t.Fatalf("clear reversed history: %v %v", got, err)
	}
}

func episodeMappingAPI(t *testing.T, s store.EpisodeMappingStore, upstream http.Handler) (http.Handler, *http.Cookie) {
	t.Helper()
	if upstream == nil {
		upstream = http.HandlerFunc(mappingCatalogResponse)
	}
	api := httptest.NewServer(upstream)
	t.Cleanup(api.Close)
	client := tmdb.NewClient("fixture", tmdb.WithBaseURL(api.URL), tmdb.WithHTTPClient(api.Client()))
	user, owner, cookie := episodeTestAuth(t, "viewer")
	catalog := handlers.NewCatalog(func() *sources.Clients { return &sources.Clients{Details: client} }, user, "/login", time.Second)
	mux := http.NewServeMux()
	handlers.NewEpisodeMappings(s, owner, catalog, nil).Register(mux)
	return handlers.Language(mux), cookie
}

func mappingParams() url.Values {
	return url.Values{
		"hash": {"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"}, "index": {"1"},
		"file": {"Show/episode.mkv"}, "tv": {"95396"}, "key": {"1"},
	}
}

func mappingRequest(t *testing.T, h http.Handler, cookie *http.Cookie, method string, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/episodes/mapping"
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(params.Encode())
	} else {
		path += "?" + params.Encode()
	}
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Datastar-Request", "true")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func mappingCatalogResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/3/tv/95396":
		_, _ = io.WriteString(w, `{"id":95396,"name":"Example series","seasons":[{"season_number":0,"name":"Specials","episode_count":1},{"season_number":2,"name":"Season 2","episode_count":4}]}`)
	case "/3/tv/95396/season/0":
		_, _ = io.WriteString(w, `{"season_number":0,"episodes":[{"episode_number":1,"season_number":0,"name":"Special"}]}`)
	case "/3/tv/95396/season/2":
		_, _ = io.WriteString(w, `{"season_number":2,"episodes":[{"episode_number":1,"season_number":2,"name":"First"},{"episode_number":2,"season_number":2,"name":"Second"},{"episode_number":3,"season_number":2,"name":"Third"},{"episode_number":4,"season_number":2,"name":"Fourth"}]}`)
	default:
		http.NotFound(w, r)
	}
}

func TestEpisodeMappingRejectsInvalidRangesAndUntrustedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, field, value string
		status             int
	}{
		{"unknown season", "season", "99", http.StatusOK}, {"zero", "first", "0", http.StatusOK},
		{"reversed", "first", "5", http.StatusOK}, {"missing episode", "last", "5", http.StatusOK},
		{"huge range", "last", "9223372036854775807", http.StatusOK},
		{"bad hash", "hash", "bad", http.StatusBadRequest}, {"bad index", "index", "0", http.StatusBadRequest},
		{"unknown series", "tv", "0", http.StatusBadRequest}, {"nonvideo", "file", "notes.txt", http.StatusBadRequest},
		{"oversized path", "file", strings.Repeat("a", 4096) + ".mkv", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := store.NewMemory()
			h, cookie := episodeMappingAPI(t, users, nil)
			p := mappingParams()
			p.Set("season", "2")
			p.Set("first", "3")
			p.Set("last", "4")
			p.Set(tc.field, tc.value)
			res := mappingRequest(t, h, cookie, http.MethodPost, p)
			if res.Code != tc.status {
				t.Fatalf("status=%d: %s", res.Code, res.Body)
			}
			if _, found, err := users.EpisodeMapping(t.Context(), "viewer", mappingParams().Get("hash"), 1); err != nil || found {
				t.Fatalf("invalid confirmation saved: %v %v", found, err)
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		h, _ := episodeMappingAPI(t, store.NewMemory(), nil)
		if res := mappingRequest(t, h, nil, method, mappingParams()); res.Code != http.StatusUnauthorized {
			t.Errorf("signed-out %s=%d", method, res.Code)
		}
	}
}

func TestEpisodeMappingAcceptsSingleSpecialAndIgnoresForeignOwner(t *testing.T) {
	users := store.NewMemory()
	h, cookie := episodeMappingAPI(t, users, nil)
	p := mappingParams()
	p.Set("season", "0")
	p.Set("first", "1")
	p.Set("last", "1")
	p.Set("uid", "another-account")
	res := mappingRequest(t, h, cookie, http.MethodPost, p)
	if res.Code != http.StatusOK {
		t.Fatalf("special=%d: %s", res.Code, res.Body)
	}
	got, found, err := users.EpisodeMapping(t.Context(), "viewer", p.Get("hash"), 1)
	if err != nil || !found || got.Season != 0 || got.First != 1 || got.Last != 1 {
		t.Fatalf("special=%+v %v %v", got, found, err)
	}
	if _, found, err := users.EpisodeMapping(t.Context(), "another-account", p.Get("hash"), 1); err != nil || found {
		t.Fatal("foreign owner accepted")
	}
	if res.Header().Get("Cache-Control") != "no-store" || !strings.Contains(res.Body.String(), `#episode-mapping[data-file-key="aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111-1-1"]`) {
		t.Fatalf("private scoped patch missing: %s %s", res.Header(), res.Body)
	}
}

func TestEpisodeMappingRejectsGapsAndPreservesConfirmationDuringProviderFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		users := store.NewMemory()
		kept := store.EpisodeMapping{Hash: mappingParams().Get("hash"), Index: 1, File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 4, Last: 4}
		if err := users.SaveEpisodeMapping(t.Context(), "viewer", kept); err != nil {
			t.Fatal(err)
		}
		h, cookie := episodeMappingAPI(t, users, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failure {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/season/2") {
				_, _ = io.WriteString(w, `{"season_number":2,"episodes":[{"episode_number":1,"season_number":2},{"episode_number":3,"season_number":2},{"episode_number":4,"season_number":2}]}`)
				return
			}
			mappingCatalogResponse(w, r)
		}))
		p := mappingParams()
		p.Set("season", "2")
		p.Set("first", "1")
		p.Set("last", "3")
		res := mappingRequest(t, h, cookie, http.MethodPost, p)
		got, found, err := users.EpisodeMapping(t.Context(), "viewer", kept.Hash, 1)
		if res.Code != http.StatusOK || err != nil || !found || got != kept {
			t.Fatalf("failed validation replaced record: %+v %v %v %d", got, found, err, res.Code)
		}
		if failure {
			mappingRequest(t, h, cookie, http.MethodDelete, mappingParams())
			if _, found, _ := users.EpisodeMapping(t.Context(), "viewer", kept.Hash, 1); found {
				t.Fatal("provider outage blocked explicit clear")
			}
		}
	}
}

func TestDeletedAccountCannotLeaveEpisodeMappingsForReusedUsername(t *testing.T) {
	var mappingPath string
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
		mappingPath = filepath.Join(st.Dir(), "episode-mappings.json")
		mappings, err := store.OpenEpisodeMappingFile(mappingPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := mappings.SaveEpisodeMapping(t.Context(), "anna", store.EpisodeMapping{Hash: mappingParams().Get("hash"), Index: 1, File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4}); err != nil {
			t.Fatal(err)
		}
	})
	admin := l.admin(t)
	old := l.signIn(t, l.accounts.Lookup("anna"))
	l.action(t, "/api/settings/users/anna/delete", `{}`, admin)
	l.action(t, "/api/settings/users", `{"newUsername":"anna","newPassword":"replacement password","newRole":"viewer"}`, admin)
	mappings, err := store.OpenEpisodeMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := mappings.EpisodeMapping(t.Context(), "anna", mappingParams().Get("hash"), 1); err != nil || found {
		t.Fatalf("replacement inherited confirmation: %v %v", found, err)
	}
	res := mappingRequest(t, l.server, old, http.MethodDelete, mappingParams())
	if res.Code == http.StatusOK {
		t.Fatal("deleted session can mutate mappings")
	}
}

// A filename naming a season the series lacks cannot preselect it: the form
// asks for a season, so picking any one of them loads its episodes.
func TestAFilenameSeasonTheSeriesLacksLeavesTheSeasonToChoose(t *testing.T) {
	h, cookie := episodeMappingAPI(t, store.NewMemory(), nil)
	p := mappingParams()
	p.Set("file", "Show/Example.S05E01.mkv")
	res := mappingRequest(t, h, cookie, http.MethodGet, p)
	if body := res.Body.String(); strings.Contains(body, "From the file name") || strings.Contains(body, "Choose a season from this series.") {
		t.Errorf("read-only view offers an unknown filename season: %s", body)
	}
	p.Set("editing", "1")
	res = mappingRequest(t, h, cookie, http.MethodGet, p)
	if body := res.Body.String(); !strings.Contains(body, `<option value="" selected>`) || strings.Contains(body, "Choose a season from this series.") {
		t.Errorf("edit form preselects an unknown season: %s", body)
	}
}

// Opening a file's menu asks TMDB for the series only; its season's
// episodes load once the user edits, so a season outage cannot hide a
// saved confirmation behind an error.
func TestTheReadOnlyViewLoadsNoSeasonEpisodes(t *testing.T) {
	users := store.NewMemory()
	kept := store.EpisodeMapping{Hash: mappingParams().Get("hash"), Index: 1, File: "Show/episode.mkv", TMDBID: 95396, Season: 2, First: 3, Last: 4}
	if err := users.SaveEpisodeMapping(t.Context(), "viewer", kept); err != nil {
		t.Fatal(err)
	}
	var seasons atomic.Int32
	h, cookie := episodeMappingAPI(t, users, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/season/") {
			seasons.Add(1)
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		mappingCatalogResponse(w, r)
	}))
	res := mappingRequest(t, h, cookie, http.MethodGet, mappingParams())
	if body := res.Body.String(); !strings.Contains(body, "S02E03–E04") || strings.Contains(body, "could not be loaded") || seasons.Load() != 0 {
		t.Errorf("read-only view asked for %d seasons: %s", seasons.Load(), body)
	}
}
