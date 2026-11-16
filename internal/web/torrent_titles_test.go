package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// searchingTMDB is a web app whose stand-in TMDB search finds Dune (2021)
// and nothing else, counting the searches for each query.
func searchingTMDB(t *testing.T) (h http.Handler, session *httptest.ResponseRecorder, searches func(query string) int) {
	t.Helper()
	var mu sync.Mutex
	count := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/3/search/multi" {
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[]}`))
			return
		}
		query := r.URL.Query().Get("query")
		mu.Lock()
		count[query]++
		mu.Unlock()
		if query == "Dune" {
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"media_type":"movie","id":841,"title":"Dune","release_date":"1984-12-14"},{"media_type":"movie","id":438631,"title":"Dune","release_date":"2021-09-15"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[]}`))
	}))
	t.Cleanup(fake.Close)
	cfg := newGoogle(t).config()
	cfg.Store = store.NewMemory()
	cfg.Sources = sources.Connector{TMDBBaseURL: fake.URL}.Connect(config.Sources{TMDBKey: "tmdb-key"})
	h = web.New(cfg)
	return h, signIn(t, h), func(query string) int {
		mu.Lock()
		defer mu.Unlock()
		return count[query]
	}
}

// eventually posts body to path until the answer contains want (or 3s pass).
func eventually(t *testing.T, post func() string, want string) string {
	t.Helper()
	var body string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if body = post(); strings.Contains(body, want) {
			break
		}
	}
	return body
}

const duneByName = `{"torrents":[{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","title":"Dune (2021)","category":"movie","timestamp":1700000000,"stat":5,"stat_string":"Torrent in db"}]}`

// A torrent added outside Moviestracker, named "Name (Year)", opens its
// title page on the TorrServer page and on Home once TMDB is searched.
func TestTorrentsNamedByTitleAndYearLinkToTheirTitlePage(t *testing.T) {
	h, session, _ := searchingTMDB(t)

	if first := postTorrents(t, h, duneByName, session).Body.String(); !strings.Contains(first, `"_tsListMatching":true`) {
		t.Errorf("while Dune (2021) is looked up the list does not ask to be posted again:\n%s", first)
	}
	page := eventually(t, func() string { return postTorrents(t, h, duneByName, session).Body.String() }, `href="/movie/438631"`)
	if !strings.Contains(page, `href="/movie/438631"`) || !strings.Contains(page, `aria-label="Open the movie page"`) {
		t.Errorf("the TorrServer page does not link Dune (2021) to /movie/438631:\n%s", page)
	}
	if !strings.Contains(page, `"_tsListMatching":false`) {
		t.Errorf("once matched the list still asks to be posted again:\n%s", page)
	}
	if strings.Contains(page, `href="/movie/841"`) {
		t.Error("Dune (2021) is linked to the 1984 Dune")
	}
	if home := postRecent(t, h, duneByName, session).Body.String(); !strings.Contains(home, `href="/movie/438631"`) || !strings.Contains(home, `"_tsRecentMatching":false`) {
		t.Errorf("Home's recent torrent does not open /movie/438631:\n%s", home)
	}
}

// A name TMDB has no title for is not searched again on every list.
func TestATorrentWithoutATitleIsNotSearchedAgainRightAway(t *testing.T) {
	h, session, searches := searchingTMDB(t)
	unknown := `{"hash":"bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222","title":"Nothing Like It (2020)","category":"movie","stat":5}`

	postTorrents(t, h, `{"torrents":[`+unknown+`]}`, session)
	both := `{"torrents":[` + unknown + `,{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","title":"Dune (2021)","category":"movie","stat":5}]}`
	page := eventually(t, func() string { return postTorrents(t, h, both, session).Body.String() }, `href="/movie/438631"`)
	if !strings.Contains(page, `href="/movie/438631"`) {
		t.Fatal("Dune (2021) was never matched")
	}
	if n := searches("Nothing Like It"); n == 0 || n > 2 {
		t.Errorf("the unknown torrent was searched %d times, want once per language (at most 2)", n)
	}
	if strings.Count(page, `aria-label="Open the movie page"`) != 1 {
		t.Error("the unknown torrent links to a movie page")
	}
}

// The TorrServer page and Home post their list again while its titles are
// being matched, though the list itself did not change.
func TestPagesPostTheirListAgainWhileTitlesAreMatched(t *testing.T) {
	h, session, _ := searchingTMDB(t)
	home := html.UnescapeString(getWith(t, h, "/", session).Body.String())
	if !strings.Contains(home, "$_tsRecentMatching && tsList(el, $_tsRecentUrl, true)") {
		t.Error("Home does not post its torrents again while their titles are matched")
	}
	page := html.UnescapeString(getWith(t, h, "/torrserver", session).Body.String())
	if !strings.Contains(page, "tsList(el, $tsSelected, $_tsListMatching)") {
		t.Error("the TorrServer page does not post its torrents again while their titles are matched")
	}
}
