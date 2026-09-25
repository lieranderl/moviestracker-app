package jacred

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const resultsJSON = `[
	{"tracker": "rutor", "url": "https://rutor.info/torrent/1", "title": "Inception (2010) BDRip 720p",
	 "size": 3990000000, "sizeName": "3.72 GB", "createTime": "2020-01-02T00:00:00Z", "sid": 1, "pir": 2,
	 "magnet": "magnet:?xt=urn:btih:AAAA&dn=a", "relased": 2010, "videotype": "sdr", "quality": 720, "voices": ["Dub"]},
	{"tracker": "kinozal", "url": "https://kinozal.tv/details.php?id=2", "title": "Inception / 2010 / 4K HDR",
	 "size": 47000000000, "sizeName": "43.99 GB", "sid": 40, "pir": 5,
	 "magnet": "magnet:?xt=urn:btih:BBBB&dn=b", "relased": 2010, "videotype": "hdr", "quality": 2160},
	{"tracker": "rutracker", "url": "https://rutracker.org/forum/viewtopic.php?t=3", "title": "Same release, fewer seeds",
	 "sizeName": "43.99 GB", "sid": 3, "magnet": "magnet:?xt=urn:btih:bbbb&dn=dup", "relased": 2010, "quality": 2160},
	{"tracker": "bitru", "url": "https://bitru.org/4", "title": "The Crack: Inception / 2019",
	 "sid": 90, "magnet": "magnet:?xt=urn:btih:CCCC", "relased": 2019, "quality": 480},
	{"tracker": "evil", "url": "javascript:alert(1)", "title": "Hostile source link",
	 "sid": 5, "magnet": "magnet:?xt=urn:btih:DDDD", "relased": 2010, "quality": 1080},
	{"tracker": "evil", "url": "https://ok.example/5", "title": "Not a magnet",
	 "sid": 5, "magnet": "http://evil.example/file.torrent", "relased": 2010, "quality": 1080}
]`

func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, append([]Option{WithHTTPClient(server.Client())}, opts...)...)
}

func TestSearchReturnsPlayableResultsMostSeededFirst(t *testing.T) {
	var got url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1.0/torrents" {
			http.NotFound(w, r)
			return
		}
		got = r.URL.Query()
		_, _ = w.Write([]byte(resultsJSON))
	})

	results, err := client.Search(context.Background(), Query{Title: "Начало", OriginalTitle: "Inception", Year: 2010})
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if got.Get("search") != "Inception" || got.Get("altname") != "Начало" {
		t.Errorf("query = %v, want search=original title, altname=localized title", got)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 (year filter, dedupe by infohash, unsafe links dropped)", results)
	}
	top := results[0]
	if top.Tracker != "kinozal" || top.Seeders != 40 || top.QualityLabel() != "4K" || !top.HDR || top.SizeName != "43.99 GB" {
		t.Errorf("top = %+v", top)
	}
	if top.SourceURL != "https://kinozal.tv/details.php?id=2" || top.Magnet != "magnet:?xt=urn:btih:BBBB&dn=b" {
		t.Errorf("links = %q %q", top.SourceURL, top.Magnet)
	}
	if results[1].QualityLabel() != "720p" || results[1].Voices[0] != "Dub" || results[1].Peers != 2 {
		t.Errorf("second = %+v", results[1])
	}
}

func TestSearchFailsWhenUpstreamErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})

	if _, err := client.Search(context.Background(), Query{OriginalTitle: "Inception"}); err == nil {
		t.Fatal("Search() error = nil, want upstream failure")
	}
}

func TestBlankSearchSkipsTheNetwork(t *testing.T) {
	var hits atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })

	results, err := client.Search(context.Background(), Query{Title: "  "})
	if err != nil || len(results) != 0 || hits.Load() != 0 {
		t.Fatalf("Search(blank) = %v, %v with %d requests; want nothing", results, err, hits.Load())
	}
}

func TestSearchResultsAreCachedBriefly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var hits atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(resultsJSON))
	}, WithCacheTTL(time.Minute), WithClock(func() time.Time { return now }))

	q := Query{OriginalTitle: "Inception", Year: 2010}
	for range 2 {
		if _, err := client.Search(context.Background(), q); err != nil {
			t.Fatalf("Search(): %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("requests within TTL = %d, want 1", hits.Load())
	}
	now = now.Add(2 * time.Minute)
	if _, err := client.Search(context.Background(), q); err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("requests after TTL = %d, want 2", hits.Load())
	}
}

const seasonJSON = `[
	{"tracker": "rutor", "url": "https://rutor.info/1", "title": "Breaking Bad S01-S05 complete / 2008-2013", "sid": 50,
	 "magnet": "magnet:?xt=urn:btih:1111", "relased": 2008, "quality": 1080, "seasons": [1, 2, 3, 4, 5]},
	{"tracker": "kinozal", "url": "https://kinozal.tv/2", "title": "Breaking Bad Season 2 (2009)", "sid": 20,
	 "magnet": "magnet:?xt=urn:btih:2222", "relased": 2009, "quality": 720, "seasons": [2]},
	{"tracker": "knaben", "url": "https://knaben.xyz/3", "title": "Breaking Bad S02E10 Over 2160p", "sid": 9,
	 "magnet": "magnet:?xt=urn:btih:3333", "relased": 0, "quality": 2160, "seasons": [2]},
	{"tracker": "rutor", "url": "https://rutor.info/4", "title": "Breaking Bad + El Camino bundle (2019)", "sid": 90,
	 "magnet": "magnet:?xt=urn:btih:4444", "relased": 2019, "quality": 1080, "seasons": [1, 2, 3, 4, 5]},
	{"tracker": "rutor", "url": "https://rutor.info/5", "title": "Breaking Bad Season 3", "sid": 70,
	 "magnet": "magnet:?xt=urn:btih:5555", "relased": 2010, "quality": 1080, "seasons": [3]}
]`

func TestSeasonSearchKeepsReleasesOfThatSeasonAndShowYears(t *testing.T) {
	var got url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(seasonJSON))
	})

	results, err := client.Search(context.Background(), Query{OriginalTitle: "Breaking Bad", Year: 2008, Season: 2, SeasonYear: 2009})
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if got.Get("season") != "2" {
		t.Errorf("season param = %q, want 2", got.Get("season"))
	}
	var titles []string
	for _, r := range results {
		titles = append(titles, r.Title)
	}
	want := []string{"Breaking Bad S01-S05 complete / 2008-2013", "Breaking Bad Season 2 (2009)", "Breaking Bad S02E10 Over 2160p"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("results = %q, want %q", titles, want)
	}
	if len(results[0].Seasons) != 5 {
		t.Errorf("Seasons = %v, want the pack's five seasons", results[0].Seasons)
	}
}

func TestSortOrdersBySeedersDateOrSizeWithoutMutatingInput(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	in := []Result{
		{Title: "small-new", Seeders: 5, Size: 1, CreatedAt: day(20)},
		{Title: "big-old", Seeders: 1, Size: 9, CreatedAt: day(1)},
		{Title: "mid-mid", Seeders: 9, Size: 5, CreatedAt: day(10)},
	}
	for by, want := range map[string]string{
		"seeders": "mid-mid|small-new|big-old",
		"date":    "small-new|mid-mid|big-old",
		"size":    "big-old|mid-mid|small-new",
		"bogus":   "mid-mid|small-new|big-old",
	} {
		var titles []string
		for _, r := range Sort(in, by) {
			titles = append(titles, r.Title)
		}
		if got := strings.Join(titles, "|"); got != want {
			t.Errorf("Sort(%q) = %s, want %s", by, got, want)
		}
	}
	if in[0].Title != "small-new" {
		t.Error("Sort must not reorder the caller's slice")
	}
}

func TestFailedSearchNeverExposesTheAPIKey(t *testing.T) {
	const key = "private-jacred-key"
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close() // requests now fail at the transport, as on a network outage

	_, err := NewClient(server.URL, WithAPIKey(key)).Search(context.Background(), Query{OriginalTitle: "Inception"})
	if err == nil {
		t.Fatal("Search() error = nil, want a transport error")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("Search() error leaks the API key: %v", err)
	}
}
