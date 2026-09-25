package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// listingTorrServer lists torrents and accepts added ones into its list.
func listingTorrServer(t *testing.T, torrents ...map[string]any) string {
	t.Helper()
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/torrents":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body["action"] {
			case "add":
				link, _ := body["link"].(string)
				hash := strings.ToLower(strings.TrimPrefix(strings.Split(link, "&")[0], "magnet:?xt=urn:btih:"))
				torrents = append(torrents, map[string]any{"hash": hash, "title": body["title"], "category": body["category"], "stat": 1})
				_, _ = w.Write([]byte(`{}`))
			case "list":
				_ = json.NewEncoder(w).Encode(torrents)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

// titleLinks waits for the TorrServer page to link to want (or to settle).
func titleLinks(t *testing.T, server *Server, want ...string) string {
	t.Helper()
	var page string
	cookie := signedIn(t, server)
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		req := httptest.NewRequest(http.MethodGet, "/torrserver", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		page = rec.Body.String()
		missing := false
		for _, w := range want {
			missing = missing || !strings.Contains(page, w)
		}
		if !missing {
			break
		}
	}
	return page
}

func TestAReleaseSentFromATitlePageLinksBackToIt(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, listingTorrServer(t))

	postSignals(t, server, "/api/torrents/add?type=movie&id=27205", `{"torrMagnet":"magnet:?xt=urn:btih:911F37B11FDC42B79A0C77AEF2F9289E9D73ECD9&dn=x"}`)

	page := titleLinks(t, server, `href="/movie/27205"`)
	if strings.Count(page, `href="/movie/27205"`) != 2 {
		t.Errorf("the torrent's title and its movie icon should both open /movie/27205:\n%s", page)
	}
	if !strings.Contains(page, `aria-label="Open the movie page"`) {
		t.Error("the movie icon has no label")
	}
}

// Torrents added before Moviestracker remembered titles, or by hand with a
// title and category, are looked up on TMDB once: by exact title and year.
func TestEarlierTorrentsAreMatchedToTheirTitleByNameAndYear(t *testing.T) {
	details := &fakeTMDBDetails{search: map[string]*tmdb.SearchResults{
		"Dune":         {Movies: []tmdb.MediaItem{{ID: 841, Title: "Dune", ReleaseDate: "1984-12-14"}}},
		"Extraction 2": {Movies: []tmdb.MediaItem{{ID: 1, Title: "Extraction", ReleaseDate: "2020-04-24"}, {ID: 697843, Title: "Extraction 2", ReleaseDate: "2023-06-09"}}},
		"Lanterns":     {Series: []tmdb.MediaItem{{ID: 94997, Title: "Lanterns", ReleaseDate: "2026-08-16"}}},
		"Heat":         {Movies: []tmdb.MediaItem{{ID: 949, Title: "Heat", ReleaseDate: "1995-12-15"}}},
	}}
	engine := listingTorrServer(t,
		map[string]any{"hash": strings.Repeat("a", 40), "title": "Dune (2021)", "category": "movie", "stat": 5},
		map[string]any{"hash": strings.Repeat("h", 40), "title": "Heat (1995)", "stat": 5},
		map[string]any{"hash": strings.Repeat("b", 40), "title": "Extraction 2 (2023)", "category": "movie", "stat": 5},
		map[string]any{"hash": strings.Repeat("c", 40), "title": "Lanterns (2026)", "category": "tv", "stat": 5},
	)
	server := newMediaServer(t, details, &fakeJacRed{}, engine)

	page := titleLinks(t, server, `href="/movie/697843"`, `href="/tv/94997"`)
	for _, want := range []string{`href="/movie/697843"`, `href="/tv/94997"`, `aria-label="Open the TV page"`} {
		if !strings.Contains(page, want) {
			t.Errorf("TorrServer page lacks %s", want)
		}
	}
	for _, wrong := range []string{`href="/movie/841"`, `href="/movie/1"`, `href="/movie/949"`} {
		if strings.Contains(page, wrong) {
			t.Errorf("TorrServer page links %s: a different year, a different title, or a torrent without a category", wrong)
		}
	}
}
