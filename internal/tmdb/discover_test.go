package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestDiscoverAsksTMDBForTheFilteredTitles(t *testing.T) {
	var mu sync.Mutex
	var asked []url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, *r.URL)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":2,"total_pages":7,"results":[{"id":27205,"title":"Inception","poster_path":"/p.jpg","release_date":"2010-07-15","vote_average":8.4}]}`))
	}))
	t.Cleanup(server.Close)
	today := time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC)
	client := NewClient("k", WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithClock(func() time.Time { return today }))

	p, err := client.Discover(context.Background(), DiscoverQuery{MediaType: "movie", Genre: 878, Year: 2010, MinRating: 7, Sort: "newest"}, 2)
	if err != nil {
		t.Fatalf("Discover(): %v", err)
	}
	if p.Page != 2 || p.TotalPages != 7 || len(p.Items) != 1 || p.Items[0].Title != "Inception" || p.Items[0].MediaType != "movie" {
		t.Errorf("Discover() = %+v", p)
	}
	q := asked[0].Query()
	if asked[0].Path != "/3/discover/movie" {
		t.Errorf("path = %s, want /3/discover/movie", asked[0].Path)
	}
	for key, want := range map[string]string{
		"page": "2", "with_genres": "878", "primary_release_year": "2010", "vote_average.gte": "7",
		"vote_count.gte": "100", "sort_by": "primary_release_date.desc", "primary_release_date.lte": "2026-10-08",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	if _, err := client.Discover(context.Background(), DiscoverQuery{MediaType: "tv", Sort: "rating"}, 1); err != nil {
		t.Fatal(err)
	}
	tv := asked[1].Query()
	if asked[1].Path != "/3/discover/tv" || tv.Get("sort_by") != "vote_average.desc" || tv.Get("vote_count.gte") != "100" || tv.Has("with_genres") {
		t.Errorf("series by rating asked %s?%s", asked[1].Path, asked[1].RawQuery)
	}
}

func TestEachKindOfTitleHasItsGenres(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{
		"/3/genre/movie/list": `{"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}]}`,
		"/3/genre/tv/list":    `{"genres":[{"id":10765,"name":"Sci-Fi & Fantasy"}]}`,
	})
	for range 2 {
		movie, err := client.Genres(context.Background(), "movie")
		if err != nil || len(movie) != 2 || movie[1].Name != "Science Fiction" {
			t.Fatalf("movie genres = %+v, %v", movie, err)
		}
	}
	if tv, err := client.Genres(context.Background(), "tv"); err != nil || len(tv) != 1 || tv[0].ID != 10765 {
		t.Errorf("tv genres = %+v, %v", tv, err)
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want 2: each kind's genres are kept", hits.Load())
	}
}
