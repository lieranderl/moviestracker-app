package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// fakeTMDBDetails is the TMDB boundary for page handlers.
type fakeTMDBDetails struct {
	movies  map[int]*tmdb.MovieDetails
	series  map[int]*tmdb.TVDetails
	seasons map[string]*tmdb.Season
	people  map[int]*tmdb.Person
	search  map[string]*tmdb.SearchResults
	lists   map[tmdb.List][]tmdb.MediaItem
	err     error
}

func (f *fakeTMDBDetails) List(_ context.Context, list tmdb.List) ([]tmdb.MediaItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.lists[list], nil
}

func lookup[T any](m map[int]*T, id int, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	if v, ok := m[id]; ok {
		return v, nil
	}
	return nil, tmdb.ErrNotFound
}

func (f *fakeTMDBDetails) Movie(_ context.Context, id int) (*tmdb.MovieDetails, error) {
	return lookup(f.movies, id, f.err)
}

func (f *fakeTMDBDetails) TV(_ context.Context, id int) (*tmdb.TVDetails, error) {
	return lookup(f.series, id, f.err)
}

func (f *fakeTMDBDetails) Season(_ context.Context, tvID, n int) (*tmdb.Season, error) {
	if s, ok := f.seasons[fmt.Sprintf("%d/%d", tvID, n)]; ok {
		return s, nil
	}
	return nil, tmdb.ErrNotFound
}

func (f *fakeTMDBDetails) Person(_ context.Context, id int) (*tmdb.Person, error) {
	return lookup(f.people, id, f.err)
}

func (f *fakeTMDBDetails) Search(_ context.Context, q string) (*tmdb.SearchResults, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.search[q]; ok {
		return r, nil
	}
	return &tmdb.SearchResults{Query: q}, nil
}

// fakeJacRed is the JacRed boundary.
type fakeJacRed struct {
	mu      sync.Mutex
	queries []jacred.Query
	results []jacred.Result
	err     error
}

func (f *fakeJacRed) Search(_ context.Context, q jacred.Query) ([]jacred.Result, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	return f.results, f.err
}

var inception = &tmdb.MovieDetails{
	MediaItem: tmdb.MediaItem{
		ID: 27205, Title: "Inception", MediaType: "movie", ReleaseDate: "2010-07-15",
		Overview: "Cobb steals secrets from dreams.", PosterPath: "/poster.jpg", BackdropPath: "/backdrop.jpg",
		VoteAverage: 8.4, VoteCount: 37000, ImdbID: "tt1375666", TrailerKey: "YoHD9XEInc0",
	},
	OriginalTitle: "Inception", Tagline: "Your mind is the scene of the crime.", Runtime: 148, Certification: "PG-13",
	Homepage:        "https://www.warnerbros.com/movies/inception",
	Links:           tmdb.ExternalIDs{IMDb: "tt1375666", Instagram: "inceptionmovie", Twitter: "Bad handle/../x", Wikidata: "Q25188"},
	Genres:          []tmdb.Genre{{ID: 28, Name: "Action"}},
	Cast:            []tmdb.CastMember{{ID: 6193, Name: "Leonardo DiCaprio", Character: "Cobb"}},
	Directors:       []tmdb.CrewMember{{ID: 525, Name: "Christopher Nolan", Job: "Director"}},
	Videos:          []tmdb.Video{{Key: "YoHD9XEInc0", Name: "Official Trailer", Type: "Trailer"}},
	Recommendations: []tmdb.MediaItem{{ID: 157336, Title: "Interstellar", MediaType: "movie"}},
}

func newMediaServer(t *testing.T, details *fakeTMDBDetails, torrents *fakeJacRed, torrURL string) *Server {
	t.Helper()
	cfg := validTestConfig(t)
	cfg.Details = details
	cfg.Torrents = torrents
	if torrURL != "" {
		cfg.TorrServer = torrserver.NewManager(torrURL)
	}
	return newTestServerWithConfig(t, cfg)
}

func get(t *testing.T, server *Server, path string, signedInUser bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if signedInUser {
		req.AddCookie(signedIn(t, server))
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func TestMoviePageRequiresSignIn(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, "")

	rec := get(t, server, "/movie/27205", false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMoviePageShowsDetailsCreditsAndSources(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, "")

	rec := get(t, server, "/movie/27205", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		"<title>Inception (2010) · Moviestracker</title>",
		"2h 28m", "PG-13", "Action",
		"Your mind is the scene of the crime.",
		"Christopher Nolan", `href="/person/525"`,
		"Leonardo DiCaprio", "Cobb", `href="/person/6193"`,
		`href="/movie/157336"`,
		`data-video="YoHD9XEInc0"`,
		`@get('/api/torrents?type=movie&id=27205&sort=' + $sort`,
		`data-init="@get('/api/torrserver/status')"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("movie page missing %q", want)
		}
	}
}

func TestMoviePageShowsOverviewOnceAndOfficialLinks(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movie/27205", true).Body.String())

	if n := strings.Count(body, "Cobb steals secrets from dreams."); n != 1 {
		t.Errorf("overview rendered %d times, want once", n)
	}
	if strings.Contains(body, "themoviedb.org/movie") {
		t.Error("title pages must not link to TMDB")
	}
	for _, want := range []string{
		`href="https://www.warnerbros.com/movies/inception"`,
		`href="https://www.imdb.com/title/tt1375666/"`,
		`href="https://www.instagram.com/inceptionmovie/"`,
		`href="https://www.wikidata.org/wiki/Q25188"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("movie page missing link %s", want)
		}
	}
	if strings.Contains(body, "x.com/") {
		t.Error("profile ids with unexpected characters must be dropped")
	}
}

func TestSourcesAreSearchedOnlyOnRequest(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movie/27205", true).Body.String())

	if strings.Contains(body, `data-init="@get('/api/torrents`) {
		t.Error("JacRed must not be queried until the user asks for sources")
	}
	if !strings.Contains(body, "Find sources") {
		t.Error("expected a Find sources button")
	}
}

func TestTorrentResultsCanBeSortedByDateSeedersOrSize(t *testing.T) {
	jr := &fakeJacRed{results: []jacred.Result{
		{Tracker: "a", Title: "Small popular", SourceURL: "https://a.example/1", Magnet: "magnet:?xt=urn:btih:aaaa", Seeders: 90, Size: 1 << 30},
		{Tracker: "b", Title: "Huge rare", SourceURL: "https://b.example/2", Magnet: "magnet:?xt=urn:btih:bbbb", Seeders: 2, Size: 60 << 30},
	}}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, jr, "")

	bySize := get(t, server, "/api/torrents?type=movie&id=27205&sort=size", true).Body.String()
	if strings.Index(bySize, "Huge rare") > strings.Index(bySize, "Small popular") {
		t.Error("sort=size should list the largest release first")
	}
	bySeeders := html.UnescapeString(get(t, server, "/api/torrents?type=movie&id=27205", true).Body.String())
	if strings.Index(bySeeders, "Small popular") > strings.Index(bySeeders, "Huge rare") {
		t.Error("default sort should list the most seeded release first")
	}
	for _, want := range []string{"$sort = 'date'", "$sort = 'seeders'", "$sort = 'size'"} {
		if !strings.Contains(bySeeders, want) {
			t.Errorf("results missing sort control %q", want)
		}
	}
}

func TestUnknownMovieIsNotFound(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	for _, path := range []string{"/movie/1", "/movie/abc", "/movie/-5"} {
		if rec := get(t, server, path, true); rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, rec.Code)
		}
	}
}

func TestMoviePageDegradesWhenTMDBFails(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{err: errors.New("tmdb down")}, &fakeJacRed{}, "")

	rec := get(t, server, "/movie/27205", true)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TMDB") {
		t.Error("expected an explanation that TMDB is unavailable")
	}
}

func TestTorrentSearchStreamsProgressThenResults(t *testing.T) {
	jr := &fakeJacRed{results: []jacred.Result{
		{Tracker: "kinozal", Title: "Inception 2010 2160p HDR", SourceURL: "https://kinozal.tv/details.php?id=2",
			Magnet: "magnet:?xt=urn:btih:bbbb&dn=b", SizeName: "43.99 GB", Seeders: 40, Quality: 2160, HDR: true},
	}}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, jr, "")

	rec := get(t, server, "/api/torrents?type=movie&id=27205", true)

	body := rec.Body.String()
	searching := strings.Index(body, "Searching JacRed")
	result := strings.Index(body, "Inception 2010 2160p HDR")
	if searching < 0 || result < 0 || searching > result {
		t.Fatalf("expected a searching patch followed by results, got %q", body)
	}
	if strings.Count(body, "event: datastar-patch-elements") < 2 {
		t.Errorf("expected at least two element patches")
	}
	for _, want := range []string{`data-magnet="magnet:?xt=urn:btih:bbbb&amp;dn=b"`, `href="https://kinozal.tv/details.php?id=2"`, "4K", "40"} {
		if !strings.Contains(body, want) {
			t.Errorf("results missing %q", want)
		}
	}
	if len(jr.queries) != 1 || jr.queries[0].OriginalTitle != "Inception" || jr.queries[0].Year != 2010 {
		t.Errorf("JacRed queries = %+v", jr.queries)
	}
}

func TestTorrentSearchExplainsUpstreamFailure(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{err: errors.New("timeout")}, "")

	body := get(t, server, "/api/torrents?type=movie&id=27205", true).Body.String()

	if !strings.Contains(body, "JacRed is not responding") {
		t.Errorf("expected upstream failure message, got %q", body)
	}
}

func TestTorrentSearchRequiresSignIn(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	if rec := get(t, server, "/api/torrents?type=movie&id=27205", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// fakeTorrServer records add requests made to a TorrServer.
func fakeTorrServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var adds []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.136"))
		case "/torrents":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["action"] == "add" {
				mu.Lock()
				adds = append(adds, body)
				mu.Unlock()
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &adds
}

func postSignals(t *testing.T, server *Server, path, signals string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(signals))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func TestSendingReleaseToTorrServerUsesTMDBTitleAndPoster(t *testing.T) {
	ts, adds := fakeTorrServer(t)
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, ts.URL)

	magnet := "magnet:?xt=urn:btih:911F37B11FDC42B79A0C77AEF2F9289E9D73ECD9&dn=x"
	rec := postSignals(t, server, "/api/torrents/add?type=movie&id=27205", `{"torrMagnet":"`+magnet+`"}`)

	if len(*adds) != 1 {
		t.Fatalf("TorrServer adds = %d, want 1", len(*adds))
	}
	add := (*adds)[0]
	if add["link"] != magnet || add["title"] != "Inception (2010)" || add["poster"] != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Errorf("add payload = %+v", add)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Added to TorrServer") {
		t.Errorf("expected success toast, got %q", body)
	}
	if !strings.Contains(body, "/api/torrserver/torrent-stats?hash=911f37b11fdc42b79a0c77aef2f9289e9d73ecd9") {
		t.Errorf("expected live progress stream for the added torrent, got %q", body)
	}
}

func TestSendingNonMagnetLinkIsRejected(t *testing.T) {
	ts, adds := fakeTorrServer(t)
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, &fakeJacRed{}, ts.URL)

	rec := postSignals(t, server, "/api/torrents/add?type=movie&id=27205", `{"torrMagnet":"javascript:alert(1)"}`)

	if len(*adds) != 0 {
		t.Fatalf("TorrServer adds = %d, want 0", len(*adds))
	}
	if !strings.Contains(rec.Body.String(), "not a magnet link") {
		t.Errorf("expected rejection toast, got %q", rec.Body.String())
	}
}

func TestTorrServerStatusReportsVersionOrOffline(t *testing.T) {
	ts, _ := fakeTorrServer(t)
	online := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, ts.URL)
	if body := get(t, online, "/api/torrserver/status", true).Body.String(); !strings.Contains(body, "MatriX.136") {
		t.Errorf("online status = %q, want version", body)
	}

	offline := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "http://127.0.0.1:1")
	if body := get(t, offline, "/api/torrserver/status", true).Body.String(); !strings.Contains(body, "Offline") {
		t.Errorf("offline status = %q, want Offline", body)
	}
}
