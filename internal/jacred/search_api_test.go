package jacred

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The search API (api.jacred.su) wraps results in an object with its own
// field names.
const searchAPIJSON = `{"query":"Inception","total":3,"limit":120,"open":true,"results":[
	{"title":"Начало / Inception [2010, HDRip-AVC] Dub","tracker":"rutracker","size":2340757176,"size_name":"2.18 GB",
	 "created_at":"2025-11-26","seeders":289,"peers":9,"year":2010,"video_type":"sdr","quality":480,
	 "voices":["Дубляж"],"seasons":[],"magnet":"magnet:?xt=urn:btih:6119&tr=x","source_url":"https://rutracker.org/forum/viewtopic.php?t=5053009"},
	{"title":"Inception 2010 2160p HDR","tracker":"kinozal","size":47000000000,"size_name":"43.77 GB",
	 "created_at":"2026-02-04","seeders":40,"peers":5,"year":2010,"video_type":"hdr","quality":2160,
	 "magnet":"magnet:?xt=urn:btih:BBBB","source_url":"https://kinozal.tv/details.php?id=2"},
	{"title":"The Crack: Inception","tracker":"bitru","seeders":90,"year":2019,"quality":480,
	 "magnet":"magnet:?xt=urn:btih:CCCC","source_url":"https://bitru.org/4"}
]}`

func TestTheSearchAPIIsAskedWithTheKeyInAHeader(t *testing.T) {
	var got url.Values
	var auth string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		got, auth = r.URL.Query(), r.Header.Get("Authorization")
		w.Header().Set("X-RateLimit-Remaining", "97")
		w.Header().Set("X-RateLimit-Reset", "1790456400")
		_, _ = w.Write([]byte(searchAPIJSON))
	}, WithSearchAPI(), WithAPIKey("jrs_test"))

	results, err := client.Search(context.Background(), Query{Title: "Начало", OriginalTitle: "Inception", Year: 2010})
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if auth != "Bearer jrs_test" || got.Has("apikey") {
		t.Errorf("key should travel as a Bearer header only: auth=%q query=%v", auth, got)
	}
	if got.Get("query") != "Inception" || got.Get("limit") != "120" {
		t.Errorf("query = %v, want query=original title and the largest page", got)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want the two 2010 releases", results)
	}
	top := results[0]
	if top.Tracker != "rutracker" || top.Seeders != 289 || top.SizeName != "2.18 GB" || top.Size != 2340757176 ||
		top.Year != 2010 || top.SourceURL != "https://rutracker.org/forum/viewtopic.php?t=5053009" || len(top.Voices) != 1 {
		t.Errorf("top = %+v", top)
	}
	if !top.CreatedAt.Equal(time.Date(2025, 11, 26, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("created = %v, want 2025-11-26", top.CreatedAt)
	}
	if !results[1].HDR || results[1].QualityLabel() != "4K" {
		t.Errorf("second = %+v, want the 4K HDR release", results[1])
	}
	left, reset, ok := client.Quota()
	if !ok || left != 97 || reset.Unix() != 1790456400 {
		t.Errorf("Quota() = %d, %v, %v; want 97 left", left, reset, ok)
	}
}

func TestTheSearchAPIAsksForTheSeason(t *testing.T) {
	var got url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"results":[]}`))
	}, WithSearchAPI())
	if _, err := client.Search(context.Background(), Query{OriginalTitle: "Severance", Year: 2022, Season: 2}); err != nil {
		t.Fatal(err)
	}
	if got.Get("season") != "2" {
		t.Errorf("query = %v, want season=2", got)
	}
}

// jacred.su moved its API to api.jacred.su; saved settings name jacred.su.
func TestJacredSuIsSearchedThroughItsAPIHost(t *testing.T) {
	for _, base := range []string{"https://jacred.su", "https://jacred.su/", "http://jacred.su", "https://api.jacred.su"} {
		var asked *http.Request
		hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			asked = r
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Header: http.Header{}}, nil
		})}
		_, err := NewClient(base, WithHTTPClient(hc), WithAPIKey("k")).Search(context.Background(), Query{OriginalTitle: "Inception"})
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		if got := asked.URL.Scheme + "://" + asked.URL.Host + asked.URL.Path; got != "https://api.jacred.su/api/search" {
			t.Errorf("%s searched %s", base, got)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Without a key (or with a wrong one) JacRed says 401; with the day's quota
// used up, 429 and when to come back.
func TestRefusalsSayWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		status int
		header string
		check  func(error) bool
	}{
		{http.StatusUnauthorized, "", func(err error) bool { return errors.Is(err, ErrKeyNeeded) }},
		{http.StatusForbidden, "", func(err error) bool { return errors.Is(err, ErrBlocked) }},
		{http.StatusTooManyRequests, "3600", func(err error) bool {
			var limit *LimitError
			return errors.As(err, &limit) && limit.Retry == time.Hour
		}},
	} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(tc.status)
		}, WithSearchAPI())
		if _, err := client.Search(context.Background(), Query{OriginalTitle: "Inception"}); !tc.check(err) {
			t.Errorf("status %d: err = %v", tc.status, err)
		}
	}
}

// A refused search is not remembered: once the key is added it works.
func TestRefusedSearchesAreNotCached(t *testing.T) {
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(searchAPIJSON))
	}, WithSearchAPI())
	q := Query{OriginalTitle: "Inception", Year: 2010}
	_, _ = client.Search(context.Background(), q)
	if results, err := client.Search(context.Background(), q); err != nil || len(results) != 2 {
		t.Fatalf("second search = %d results, %v", len(results), err)
	}
}

// One quality, or HDR, is filtered by JacRed, which then returns all of them
// instead of the best-seeded 120 of everything. Several qualities are one
// search (the API takes one), filtered here.
func TestQualityAndHDRNarrowTheSearch(t *testing.T) {
	for _, tc := range []struct {
		q         Query
		quality   string
		videotype string
		want      []string
	}{
		{Query{Qualities: []int{2160}}, "2160", "", []string{"kinozal"}},
		{Query{Qualities: []int{480, 2160}}, "", "", []string{"rutracker", "kinozal"}},
		{Query{HDR: true}, "", "hdr", []string{"kinozal"}},
		{Query{Qualities: []int{480}, HDR: true}, "480", "hdr", nil},
	} {
		var got url.Values
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Query()
			_, _ = w.Write([]byte(searchAPIJSON))
		}, WithSearchAPI())
		tc.q.OriginalTitle, tc.q.Year = "Inception", 2010
		results, err := client.Search(context.Background(), tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if got.Get("quality") != tc.quality || got.Get("videotype") != tc.videotype {
			t.Errorf("%+v asked quality=%q videotype=%q", tc.q, got.Get("quality"), got.Get("videotype"))
		}
		var trackers []string
		for _, r := range results {
			trackers = append(trackers, r.Tracker)
		}
		if strings.Join(trackers, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%+v kept %v, want %v", tc.q, trackers, tc.want)
		}
	}
}

// A different quality is a different search, not the cached one.
func TestQualityIsPartOfTheCachedSearch(t *testing.T) {
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(searchAPIJSON))
	}, WithSearchAPI())
	for _, q := range []Query{{OriginalTitle: "Inception"}, {OriginalTitle: "Inception", Qualities: []int{2160}}, {OriginalTitle: "Inception", HDR: true}} {
		_, _ = client.Search(context.Background(), q)
	}
	if calls != 3 {
		t.Errorf("%d searches, want 3", calls)
	}
}

// apiReleases is a search API page of releases with these titles, from
// year, with distinct hashes.
func apiReleases(year int, titles ...string) string {
	var b strings.Builder
	b.WriteString(`{"results":[`)
	for i, title := range titles {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"title":%q,"tracker":"rutor","seeders":%d,"year":%d,"magnet":"magnet:?xt=urn:btih:%x","source_url":"https://rutor.info/%d"}`,
			title, 100-i, year, []byte(title), i)
	}
	b.WriteString("]}")
	return b.String()
}

// fakeSearchAPI answers each query from pages; others find nothing.
func fakeSearchAPI(t *testing.T, pages map[string]string, asked *[]string) *Client {
	t.Helper()
	return newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		*asked = append(*asked, q.Get("query")+"@"+q.Get("season"))
		page, ok := pages[q.Get("query")]
		switch {
		case !ok:
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.HasPrefix(page, "status "):
			code, _ := strconv.Atoi(strings.TrimPrefix(page, "status "))
			w.WriteHeader(code)
		default:
			_, _ = w.Write([]byte(page))
		}
	}, WithSearchAPI())
}

func titles(results []Result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Title)
	}
	slices.Sort(out)
	return out
}

// The English name finds most titles in one search: torrents are filed
// under it, beside the Russian one.
func TestTheSearchAPIAsksTheEnglishNameFirst(t *testing.T) {
	many := make([]string, 60)
	for i := range many {
		many[i] = fmt.Sprintf("Начало / Inception (2010) part %d", i)
	}
	var asked []string
	client := fakeSearchAPI(t, map[string]string{"Inception": apiReleases(2010, many...)}, &asked)

	results, err := client.Search(context.Background(), Query{
		IMDbID: "tt1375666", EnglishTitle: "Inception", OriginalTitle: "Inception", Title: "Начало", Year: 2010,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "Inception@" || len(results) != 60 {
		t.Errorf("asked %v and kept %d results, want the English name only and its 60 releases", asked, len(results))
	}
}

// When the English name finds few releases the IMDb id is asked too, for
// the season, and both are kept; releases not named after the title, which
// the id or a common word also finds, are left out.
func TestFewReleasesByTheEnglishNameAreToppedUpByTheIMDbID(t *testing.T) {
	var asked []string
	client := fakeSearchAPI(t, map[string]string{
		"The Bear":   apiReleases(2024, "Медведь 3 сезон (1-10 из 10) / The Bear (2024) WEBRip", "The.Bear.S03.1080p.WEB"),
		"tt14452776": apiReleases(2024, "Медведь 3 сезон (1-10 из 10) / The Bear (2024) WEBRip", "Медведь / The Bear [3 сезон] (2024) LostFilm", "Маша и Медведь / Сезон: 1-7 (2024)"),
	}, &asked)

	results, err := client.Search(context.Background(), Query{
		IMDbID: "tt14452776", EnglishTitle: "The Bear", OriginalTitle: "The Bear", Title: "Медведь", Year: 2022, Season: 3, SeasonYear: 2024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "The Bear@3,tt14452776@3" {
		t.Errorf("asked %v, want the English name, then the IMDb id, for season 3", asked)
	}
	want := []string{"The.Bear.S03.1080p.WEB", "Медведь / The Bear [3 сезон] (2024) LostFilm", "Медведь 3 сезон (1-10 из 10) / The Bear (2024) WEBRip"}
	if got := titles(results); !slices.Equal(got, want) {
		t.Errorf("kept %q, want %q", got, want)
	}
}

// When neither finds the title, its original and local names are tried in
// turn; a name the API fails on is passed over.
func TestTheOriginalAndLocalNamesAreTriedWhenNothingElseFindsTheTitle(t *testing.T) {
	var asked []string
	client := fakeSearchAPI(t, map[string]string{
		"기생충":      "status 502",
		"Паразиты": apiReleases(2019, "Паразиты / Gisaengchung (2019) BDRip"),
	}, &asked)

	results, err := client.Search(context.Background(), Query{
		IMDbID: "tt6751668", EnglishTitle: "Parasite", OriginalTitle: "기생충", Title: "Паразиты", Year: 2019,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, ",") != "Parasite@,tt6751668@,기생충@,Паразиты@" || len(results) != 1 {
		t.Errorf("asked %v and kept %d results", asked, len(results))
	}
}

// Each name is asked once, and a malformed IMDb id never.
func TestEachNameIsAskedOnce(t *testing.T) {
	var asked []string
	client := fakeSearchAPI(t, nil, &asked)
	_, _ = client.Search(context.Background(), Query{IMDbID: "1375666", EnglishTitle: "Verity", OriginalTitle: "verity", Title: "Верити", Year: 2026})
	if strings.Join(asked, ",") != "Verity@,Верити@" {
		t.Errorf("asked %v", asked)
	}
}

// A refusal ends the search: the next name would be refused too.
func TestTheSearchAPIStopsAtARefusal(t *testing.T) {
	var asked []string
	client := fakeSearchAPI(t, map[string]string{"Inception": "status 429"}, &asked)
	_, err := client.Search(context.Background(), Query{IMDbID: "tt1375666", EnglishTitle: "Inception", Title: "Начало"})
	if _, ok := errors.AsType[*LimitError](err); !ok || len(asked) != 1 {
		t.Errorf("err = %v after %v, want the limit after one search", err, asked)
	}
}

// A search the API failed throughout is an error, so the backup JacRed
// can answer it, not an empty result.
func TestASearchThatFailedEverywhereIsAnError(t *testing.T) {
	var asked []string
	client := fakeSearchAPI(t, map[string]string{"Inception": "status 502", "tt1375666": "status 502"}, &asked)
	if _, err := client.Search(context.Background(), Query{IMDbID: "tt1375666", EnglishTitle: "Inception"}); err == nil {
		t.Errorf("no error after %v", asked)
	}
}

// Other JacRed protocols search by name only: they do not resolve IMDb ids
// reliably.
func TestOnlyTheSearchAPIIsAskedByIMDbID(t *testing.T) {
	for name, tc := range map[string]struct {
		opts []Option
		path string
		key  string
	}{
		"jacred-fdb": {nil, "/api/v1.0/torrents", "search"},
		"jackett":    {[]Option{WithJackettAPI()}, "/api/v2.0/indexers/all/results", "Query"},
	} {
		var got string
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Query().Get(tc.key)
			_, _ = w.Write([]byte(`[]`))
		}, tc.opts...)
		_, _ = client.Search(context.Background(), Query{OriginalTitle: "Inception", IMDbID: "tt1375666"})
		if got != "Inception" {
			t.Errorf("%s asked %s=%q, want the title", name, tc.key, got)
		}
	}
}
