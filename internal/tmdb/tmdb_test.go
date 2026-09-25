package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogFetchesTrendingListsConcurrentlyOnce(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var trendingRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			trendingRequests.Add(1)
			started <- struct{}{}
			<-release
			_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Movie","backdrop_path":"/movie.jpg"}]}`))
		case "/3/trending/tv/week":
			trendingRequests.Add(1)
			started <- struct{}{}
			<-release
			_, _ = w.Write([]byte(`{"results":[{"id":2,"name":"Series","backdrop_path":"/series.jpg"}]}`))
		default:
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	type result struct {
		catalog Catalog
		err     error
	}
	results := make(chan result, 1)
	go func() {
		catalog, err := client.GetCatalog(context.Background())
		results <- result{catalog: catalog, err: err}
	}()

	<-started
	<-started
	close(release)
	got := <-results
	if got.err != nil {
		t.Fatalf("GetCatalog(): %v", got.err)
	}
	if trendingRequests.Load() != 2 {
		t.Fatalf("trending requests = %d, want 2", trendingRequests.Load())
	}
	if len(got.catalog.Movies) != 1 || got.catalog.Movies[0].Title != "Movie" {
		t.Fatalf("movies = %+v", got.catalog.Movies)
	}
	if len(got.catalog.Series) != 1 || got.catalog.Series[0].Title != "Series" {
		t.Fatalf("series = %+v", got.catalog.Series)
	}
	if len(got.catalog.Hero) != 2 {
		t.Fatalf("hero count = %d, want 2", len(got.catalog.Hero))
	}
}

func TestCatalogKeepsUsableListWhenOneTrendingEndpointFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[{"id":2,"name":"Series","backdrop_path":"/series.jpg"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	catalog, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog(): %v", err)
	}
	if len(catalog.Movies) != 0 {
		t.Fatalf("movies = %+v, want empty", catalog.Movies)
	}
	if len(catalog.Series) != 1 || catalog.Series[0].Title != "Series" {
		t.Fatalf("series = %+v", catalog.Series)
	}
	if len(catalog.Hero) != 1 || catalog.Hero[0].Title != "Series" {
		t.Fatalf("hero = %+v", catalog.Hero)
	}
}

func TestCatalogAppendsImagesAndVideosInOneHeroRequest(t *testing.T) {
	var requests atomic.Int32
	var detailQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":10,"title":"Hero","backdrop_path":"/hero.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/3/movie/10":
			detailQuery = r.URL.Query().Get("append_to_response")
			_, _ = w.Write([]byte(`{"images":{"logos":[{"file_path":"/en_logo.png","iso_639_1":"en"}]},"videos":{"results":[{"key":"dQw4w9WgXcQ","site":"YouTube","type":"Trailer","official":true,"iso_639_1":"en"}]},"external_ids":{"imdb_id":"tt1234567"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	catalog, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog(): %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if detailQuery != "images,videos,external_ids" {
		t.Fatalf("append_to_response = %q, want images,videos,external_ids", detailQuery)
	}
	if got := catalog.Hero[0].ImdbID; got != "tt1234567" {
		t.Fatalf("ImdbID = %q, want tt1234567", got)
	}
	if got := catalog.Hero[0].LogoPath; got != "/en_logo.png" {
		t.Fatalf("LogoPath = %q, want /en_logo.png", got)
	}
	if got := catalog.Hero[0].TrailerKey; got != "dQw4w9WgXcQ" {
		t.Fatalf("TrailerKey = %q, want dQw4w9WgXcQ", got)
	}
}

func TestCatalogRejectsInvalidTrailerKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":10,"title":"Hero","backdrop_path":"/hero.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case "/3/movie/10":
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[{"key":"invalid/key","site":"YouTube","type":"Trailer","official":true}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	catalog, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog(): %v", err)
	}
	if got := catalog.Hero[0].TrailerKey; got != "" {
		t.Fatalf("TrailerKey = %q, want empty", got)
	}
}

func TestCatalogBoundsHeroDetailConcurrency(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[
				{"id":1,"title":"One","backdrop_path":"/1.jpg","media_type":"movie"},
				{"id":2,"title":"Two","backdrop_path":"/2.jpg","media_type":"movie"},
				{"id":3,"title":"Three","backdrop_path":"/3.jpg","media_type":"movie"},
				{"id":4,"title":"Four","backdrop_path":"/4.jpg","media_type":"movie"},
				{"id":5,"title":"Five","backdrop_path":"/5.jpg","media_type":"movie"},
				{"id":6,"title":"Six","backdrop_path":"/6.jpg","media_type":"movie"},
				{"id":7,"title":"Seven","backdrop_path":"/7.jpg","media_type":"movie"},
				{"id":8,"title":"Eight","backdrop_path":"/8.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			current := active.Add(1)
			for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
			}
			time.Sleep(25 * time.Millisecond)
			active.Add(-1)
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	catalog, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("GetCatalog(): %v", err)
	}
	if len(catalog.Hero) != 7 {
		t.Fatalf("hero count = %d, want 7", len(catalog.Hero))
	}
	if got := maximum.Load(); got != 3 {
		t.Fatalf("maximum detail concurrency = %d, want 3", got)
	}
}

func TestCatalogColdLoadUsesAtMostNineRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[
				{"id":1,"title":"One","backdrop_path":"/1.jpg","media_type":"movie"},
				{"id":2,"title":"Two","backdrop_path":"/2.jpg","media_type":"movie"},
				{"id":3,"title":"Three","backdrop_path":"/3.jpg","media_type":"movie"},
				{"id":4,"title":"Four","backdrop_path":"/4.jpg","media_type":"movie"},
				{"id":5,"title":"Five","backdrop_path":"/5.jpg","media_type":"movie"},
				{"id":6,"title":"Six","backdrop_path":"/6.jpg","media_type":"movie"},
				{"id":7,"title":"Seven","backdrop_path":"/7.jpg","media_type":"movie"},
				{"id":8,"title":"Eight","backdrop_path":"/8.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	if _, err := client.GetCatalog(context.Background()); err != nil {
		t.Fatalf("GetCatalog(): %v", err)
	}
	if got := requests.Load(); got > 9 {
		t.Fatalf("requests = %d, want at most 9 (2 trending lists + 7 hero details)", got)
	}
}

func TestCatalogFreshCacheAvoidsUpstreamCalls(t *testing.T) {
	now := time.Unix(1_000, 0)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Movie","backdrop_path":"/movie.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithClock(func() time.Time { return now }))
	first, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("first GetCatalog(): %v", err)
	}
	first.Hero[0].Title = "mutated"
	first.Movies[0].Title = "mutated"
	second, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("second GetCatalog(): %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if second.Hero[0].Title != "Movie" || second.Movies[0].Title != "Movie" {
		t.Fatalf("cached catalog was mutated: %+v", second)
	}
}

func TestCatalogConcurrentMissesShareOneRefresh(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Movie","backdrop_path":"/movie.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	start := make(chan struct{})
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			<-start
			_, err := client.GetCatalog(context.Background())
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("GetCatalog(): %v", err)
		}
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestCatalogReturnsStaleSnapshotWhenRefreshFails(t *testing.T) {
	now := time.Unix(1_000, 0)
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() && strings.HasPrefix(r.URL.Path, "/3/trending/") {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Movie","backdrop_path":"/movie.jpg","media_type":"movie"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"images":{"logos":[]},"videos":{"results":[]}}`))
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithClock(func() time.Time { return now }))
	if _, err := client.GetCatalog(context.Background()); err != nil {
		t.Fatalf("initial GetCatalog(): %v", err)
	}
	now = now.Add(11 * time.Minute)
	failing.Store(true)
	catalog, err := client.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("stale GetCatalog(): %v", err)
	}
	if !catalog.Stale {
		t.Fatal("catalog.Stale = false, want true")
	}
	if catalog.Hero[0].Title != "Movie" {
		t.Fatalf("stale hero = %+v", catalog.Hero)
	}
}

func TestTrendingRejectsUnknownWindowWithoutNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	_, err := client.GetTrendingMovies(context.Background(), "month")
	if !errors.Is(err, ErrInvalidTimeWindow) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidTimeWindow)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestMediaURLsRejectUnsafePaths(t *testing.T) {
	items := []MediaItem{
		{PosterPath: "poster.jpg", BackdropPath: "backdrop.jpg", LogoPath: "logo.png"},
		{PosterPath: "//evil.example/poster.jpg", BackdropPath: "//evil.example/backdrop.jpg", LogoPath: "//evil.example/logo.png"},
		{PosterPath: "/../poster.jpg", BackdropPath: "/../backdrop.jpg", LogoPath: "/../logo.png"},
	}
	for _, item := range items {
		if got := item.PosterURL(); got != "" {
			t.Errorf("PosterURL() = %q, want empty", got)
		}
		if got := item.BackdropURL(); got != "" {
			t.Errorf("BackdropURL() = %q, want empty", got)
		}
		if got := item.LogoURL(); got != "" {
			t.Errorf("LogoURL() = %q, want empty", got)
		}
	}
}

func TestTrendingNormalizesUnknownMediaType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Unexpected","media_type":"person"}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	items, err := client.GetTrendingMovies(context.Background(), "week")
	if err != nil {
		t.Fatalf("GetTrendingMovies(): %v", err)
	}
	if got := items[0].MediaType; got != "movie" {
		t.Fatalf("MediaType = %q, want movie", got)
	}
}

func TestClientGetTrendingMovies(t *testing.T) {
	mockResponse := `{
		"page": 1,
		"results": [
			{
				"id": 101,
				"title": "Interstellar Odyssey",
				"overview": "A voyage across the cosmos.",
				"poster_path": "/poster101.jpg",
				"backdrop_path": "/backdrop101.jpg",
				"release_date": "2026-05-15",
				"vote_average": 8.65,
				"vote_count": 12450
			}
		]
	}`

	var receivedPath, receivedAuthorization string
	var receivedAPIKey bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuthorization = r.Header.Get("Authorization")
		_, receivedAPIKey = r.URL.Query()["api_key"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	client := NewClient("secret-test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	movies, err := client.GetTrendingMovies(ctx, "week")
	if err != nil {
		t.Fatalf("GetTrendingMovies() unexpected error: %v", err)
	}

	if receivedPath != "/3/trending/movie/week" {
		t.Errorf("path = %q, want /3/trending/movie/week", receivedPath)
	}
	if receivedAuthorization != "Bearer secret-test-key" {
		t.Errorf("Authorization = %q, want Bearer secret-test-key", receivedAuthorization)
	}
	if receivedAPIKey {
		t.Error("api_key must not appear in the request URL")
	}

	if len(movies) != 1 {
		t.Fatalf("len(movies) = %d, want 1", len(movies))
	}

	item := movies[0]
	if item.ID != 101 {
		t.Errorf("item.ID = %d, want 101", item.ID)
	}
	if item.Title != "Interstellar Odyssey" {
		t.Errorf("item.Title = %q, want 'Interstellar Odyssey'", item.Title)
	}
	if item.PosterURL() != "https://image.tmdb.org/t/p/w500/poster101.jpg" {
		t.Errorf("item.PosterURL() = %q, want https://image.tmdb.org/t/p/w500/poster101.jpg", item.PosterURL())
	}
	if item.BackdropURL() != "https://image.tmdb.org/t/p/w1280/backdrop101.jpg" {
		t.Errorf("item.BackdropURL() = %q, want https://image.tmdb.org/t/p/w1280/backdrop101.jpg", item.BackdropURL())
	}
	if item.FormattedRating() != "8.7" {
		t.Errorf("item.FormattedRating() = %q, want 8.7", item.FormattedRating())
	}
	if item.VoteCount != 12450 {
		t.Errorf("item.VoteCount = %d, want 12450", item.VoteCount)
	}
	if item.FormattedVoteCount() != "12k" {
		t.Errorf("item.FormattedVoteCount() = %q, want '12k'", item.FormattedVoteCount())
	}
	if item.ReleaseYear() != "2026" {
		t.Errorf("item.ReleaseYear() = %q, want 2026", item.ReleaseYear())
	}
}

func TestClientGetTrendingSeries(t *testing.T) {
	mockResponse := `{
		"page": 1,
		"results": [
			{
				"id": 202,
				"name": "Chronicles of Earth",
				"overview": "An epic sci-fi series spanning centuries.",
				"poster_path": "/series202.jpg",
				"backdrop_path": "/series_bg202.jpg",
				"first_air_date": "2025-11-20",
				"vote_average": 9.12
			}
		]
	}`

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	series, err := client.GetTrendingSeries(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTrendingSeries() unexpected error: %v", err)
	}

	if receivedPath != "/3/trending/tv/week" {
		t.Errorf("path = %q, want /3/trending/tv/week", receivedPath)
	}
	if len(series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(series))
	}
	if series[0].Title != "Chronicles of Earth" {
		t.Errorf("series title = %q, want 'Chronicles of Earth'", series[0].Title)
	}
	if series[0].ReleaseYear() != "2025" {
		t.Errorf("series year = %q, want 2025", series[0].ReleaseYear())
	}
	if series[0].MediaType != "tv" {
		t.Errorf("series media_type = %q, want tv", series[0].MediaType)
	}
}

func TestClientHTTPErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_message": "Invalid API key"}`))
	}))
	defer server.Close()

	client := NewClient("invalid-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	_, err := client.GetTrendingMovies(context.Background(), "week")
	if err == nil {
		t.Fatal("expected error on 401 Unauthorized, got nil")
	}
}

func TestClient_GetTrendingMovies_V3APIKeyRequestFormation(t *testing.T) {
	var receivedPath string
	var receivedAuthorization string
	var receivedAPIKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuthorization = r.Header.Get("Authorization")
		receivedAPIKey = r.URL.Query().Get("api_key")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":101,"title":"Interstellar Odyssey"}]}`))
	}))
	defer server.Close()

	const v3Key = "fedcba9876543210fedcba9876543210" // #nosec G101 -- a made-up v3 key
	client := NewClient(v3Key, WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	movies, err := client.GetTrendingMovies(ctx, "week")
	if err != nil {
		t.Fatalf("GetTrendingMovies() unexpected error: %v", err)
	}

	if receivedPath != "/3/trending/movie/week" {
		t.Errorf("path = %q, want /3/trending/movie/week", receivedPath)
	}
	if receivedAuthorization != "" {
		t.Errorf("Authorization = %q, want empty for v3 key", receivedAuthorization)
	}
	if receivedAPIKey != v3Key {
		t.Errorf("api_key query param = %q, want %q", receivedAPIKey, v3Key)
	}
	if len(movies) != 1 {
		t.Fatalf("len(movies) = %d, want 1", len(movies))
	}
}

func TestMediaItem_FormattedVoteCount(t *testing.T) {
	tests := []struct {
		name      string
		voteCount int
		want      string
	}{
		{name: "zero", voteCount: 0, want: ""},
		{name: "negative", voteCount: -10, want: ""},
		{name: "under 1000", voteCount: 850, want: "850"},
		{name: "exact 1000", voteCount: 1000, want: "1k"},
		{name: "1200", voteCount: 1200, want: "1k"},
		{name: "2000", voteCount: 2000, want: "2k"},
		{name: "2400", voteCount: 2400, want: "2k"},
		{name: "9900", voteCount: 9900, want: "10k"},
		{name: "10000", voteCount: 10000, want: "10k"},
		{name: "12450", voteCount: 12450, want: "12k"},
		{name: "56844", voteCount: 56844, want: "57k"},
		{name: "120000", voteCount: 120000, want: "120k"},
		{name: "999800", voteCount: 999800, want: "1M"},
		{name: "1200000", voteCount: 1200000, want: "1.2M"},
		{name: "2000000", voteCount: 2000000, want: "2M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := MediaItem{VoteCount: tt.voteCount}
			if got := item.FormattedVoteCount(); got != tt.want {
				t.Errorf("FormattedVoteCount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMediaItem_TMDBRatingTitle(t *testing.T) {
	tests := []struct {
		name        string
		voteAverage float64
		voteCount   int
		want        string
	}{
		{
			name:        "with votes",
			voteAverage: 8.65,
			voteCount:   12450,
			want:        "TMDB: 8.7/10 (12k votes)",
		},
		{
			name:        "without votes",
			voteAverage: 8.65,
			voteCount:   0,
			want:        "TMDB: 8.7/10",
		},
		{
			name:        "no rating no votes",
			voteAverage: 0,
			voteCount:   0,
			want:        "TMDB: NR/10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := MediaItem{VoteAverage: tt.voteAverage, VoteCount: tt.voteCount}
			if got := item.TMDBRatingTitle(); got != tt.want {
				t.Errorf("TMDBRatingTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}
