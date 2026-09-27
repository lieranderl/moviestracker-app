package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// pagedTMDB answers every list with 20 titles per page (ids page*100+n,
// no media type) and says how many pages there are.
func pagedTMDB(t *testing.T, totalPages int) (*Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?page="+r.URL.Query().Get("page"))
		mu.Unlock()
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var results []string
		for n := range 20 {
			results = append(results, fmt.Sprintf(`{"id": %d, "title": "Title %d", "name": "Title %d"}`, page*100+n, page*100+n, page*100+n))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"page": %d, "total_pages": %d, "results": [%s]}`, page, totalPages, strings.Join(results, ","))
	}))
	t.Cleanup(server.Close)
	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	return client, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

func TestAListIsReadTwentyTitlesAPageAndEachPageIsCached(t *testing.T) {
	client, asked := pagedTMDB(t, 3)

	for range 2 {
		p, err := client.ListPage(context.Background(), TrendingSeriesDay, 2)
		if err != nil {
			t.Fatal(err)
		}
		if p.Page != 2 || p.TotalPages != 3 || len(p.Items) != 20 || p.Items[0].ID != 200 || p.Items[0].MediaType != "tv" || p.Items[0].Title != "Title 200" {
			t.Fatalf("ListPage(TrendingSeriesDay, 2) = page %d of %d, %d items, first %+v", p.Page, p.TotalPages, len(p.Items), p.Items[0])
		}
	}
	if got := asked(); len(got) != 1 || got[0] != "/3/trending/tv/day?page=2" {
		t.Errorf("TMDB was asked %q, want page 2 of today's trending series once", got)
	}
}

func TestEveryListCanBePaged(t *testing.T) {
	client, asked := pagedTMDB(t, 1000)
	for list, mediaType := range map[List]string{
		TrendingMoviesWeek: "movie", TrendingMoviesDay: "movie", TrendingSeriesWeek: "tv", TrendingSeriesDay: "tv",
		NowPlayingMovies: "movie", PopularMovies: "movie", TopRatedMovies: "movie", PopularSeries: "tv", TopRatedSeries: "tv",
	} {
		p, err := client.ListPage(context.Background(), list, 1)
		if err != nil || len(p.Items) != 20 || p.Items[0].MediaType != mediaType {
			t.Errorf("ListPage(%s, 1) = %+v, %v", list, p, err)
		}
		// TMDB serves no page past 500.
		if p.TotalPages != 500 {
			t.Errorf("ListPage(%s) says %d pages, want 500", list, p.TotalPages)
		}
	}
	if n := len(asked()); n != 9 {
		t.Errorf("TMDB was asked %d times, want 9", n)
	}
}

func TestPagesOutsideTMDBsRangeAreNotAsked(t *testing.T) {
	client, asked := pagedTMDB(t, 3)
	for _, page := range []int{0, -1, 501} {
		if _, err := client.ListPage(context.Background(), PopularMovies, page); err == nil {
			t.Errorf("ListPage(page %d) succeeded", page)
		}
	}
	if got := asked(); len(got) != 0 {
		t.Errorf("TMDB was asked %q", got)
	}
}
