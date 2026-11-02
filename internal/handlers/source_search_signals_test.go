package handlers

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Every source search asks the same address and sends its choices as
// signals, so Datastar cancels a search still running when a newer one
// starts: a late answer never replaces the newer one's.
func TestASourceSearchSendsItsChoicesAsSignals(t *testing.T) {
	if search := views.SourcesSearch("tv", 95396); !strings.Contains(search, "@get('/api/torrents?type=tv&id=95396', {filterSignals: {include: /^(sort|qual|hdr|sourceSeason)$/}})") {
		t.Errorf("the search varies its address or sends no choices: %s", search)
	}
	jr := &fakeJacRed{}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, jr, "")
	signals := url.QueryEscape(`{"sort":"size","qual":["2160","","1080",""],"hdr":true}`)
	get(t, server, "/api/torrents?type=movie&id=27205&datastar="+signals, true)
	if len(jr.queries) != 1 {
		t.Fatalf("queries = %+v", jr.queries)
	}
	if q := jr.queries[0]; fmt.Sprint(q.Qualities) != "[2160 1080]" || !q.HDR {
		t.Errorf("query = %+v, want the signals' qualities [2160 1080] and HDR", q)
	}
}

// A page opened before this change still sends its choices in the query,
// with no signals: they are kept.
func TestAnOlderPageKeepsItsQueryChoices(t *testing.T) {
	jr := &fakeJacRed{}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, jr, "")
	get(t, server, "/api/torrents?type=movie&id=27205&sort=size&quality=2160&hdr=1&datastar="+url.QueryEscape(`{}`), true)
	if len(jr.queries) != 1 {
		t.Fatalf("queries = %+v", jr.queries)
	}
	if q := jr.queries[0]; fmt.Sprint(q.Qualities) != "[2160]" || !q.HDR {
		t.Errorf("query = %+v, want the URL's quality 2160 and HDR", q)
	}
}
