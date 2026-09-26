package jacred

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
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
