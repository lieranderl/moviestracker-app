package handlers

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// severanceServer is a local app showing Severance: season 1 aired whole
// (three episodes), season 2 aired its first two of four.
func severanceServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	episodes := func(season int, aired ...string) []tmdb.Episode {
		var out []tmdb.Episode
		for i, at := range aired {
			out = append(out, tmdb.Episode{Number: i + 1, SeasonNumber: season, Name: "Episode", AirDate: at, Overview: fmt.Sprintf("Plot of S%dE%d", season, i+1), StillPath: fmt.Sprintf("/still-%d-%d.jpg", season, i+1)})
		}
		return out
	}
	details := &fakeTMDBDetails{
		series: map[int]*tmdb.TVDetails{95396: {
			MediaItem: tmdb.MediaItem{ID: 95396, Title: "Severance", MediaType: "tv"},
			Seasons: []tmdb.SeasonSummary{
				{Number: 1, EpisodeCount: 3, AirDate: "2022-02-18"},
				{Number: 2, EpisodeCount: 4, AirDate: "2025-01-17"},
			},
			// As TMDB gives it: no aired count, the last episode out.
			LastEpisode: tmdb.Airing{Season: 2, Number: 2},
		}},
		seasons: map[string]*tmdb.Season{
			"95396/1": {ShowID: 95396, Number: 1, Episodes: episodes(1, "2022-02-18", "2022-02-18", "2022-02-25")},
			"95396/2": {ShowID: 95396, Number: 2, Episodes: episodes(2, "2025-01-17", "2025-01-24", "2099-01-01", "")},
		},
	}
	s := newMediaServer(t, details, nil, "")
	return s, signedIn(t, s)
}

func episodeRequest(t *testing.T, s *Server, cookie *http.Cookie, method, path string) string {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	return html.UnescapeString(rec.Body.String())
}

// A user ticks off episodes; the season shows which, and the series says
// which aired episode comes next.
func TestAUserTicksOffEpisodesAndSeesTheNextUnwatched(t *testing.T) {
	s, cookie := severanceServer(t)
	page := episodeRequest(t, s, cookie, http.MethodGet, "/tv/95396?season=1")
	for _, want := range []string{`id="episode-1-1"`, `aria-pressed="false"`, "@post('/api/episodes?tv=95396&season=1&episode=1&watched=1')", "Mark season watched", "Next: S01E01"} {
		if !strings.Contains(page, want) {
			t.Errorf("the series page lacks %q", want)
		}
	}
	marked := episodeRequest(t, s, cookie, http.MethodPost, "/api/episodes?tv=95396&season=1&episode=1&watched=1")
	if !strings.Contains(marked, `id="season-episodes"`) || !strings.Contains(marked, "@post('/api/episodes?tv=95396&season=1&episode=1&watched=0')") || !strings.Contains(marked, "Next: S01E02") {
		t.Errorf("marking S01E01 watched answered:\n%s", marked)
	}
	season := episodeRequest(t, s, cookie, http.MethodPost, "/api/episodes/season?tv=95396&season=1&watched=1")
	if !strings.Contains(season, "Next: S02E01") || strings.Contains(season, "watched=1')\" aria-pressed") {
		t.Errorf("marking season 1 watched answered:\n%s", season)
	}
	episodeRequest(t, s, cookie, http.MethodPost, "/api/episodes/season?tv=95396&season=2&watched=1")
	page = episodeRequest(t, s, cookie, http.MethodGet, "/tv/95396?season=2")
	if !strings.Contains(page, "All aired episodes watched") {
		t.Error("with every aired episode watched the series still names a next one")
	}
	if strings.Contains(page, `id="episode-2-3"`) || !strings.Contains(page, `id="episode-2-2"`) {
		t.Error("episodes that have not aired should have no watched toggle; aired ones should")
	}
	if strings.Contains(episodeRequest(t, s, cookie, http.MethodGet, "/api/titles/state?type=tv&id=95396"), `data-state="watched" aria-pressed="true"`) {
		t.Error("watching every aired episode marked the whole series watched")
	}
	episodeRequest(t, s, cookie, http.MethodPost, "/api/episodes/season?tv=95396&season=1&watched=0")
	if page := episodeRequest(t, s, cookie, http.MethodGet, "/tv/95396?season=1"); !strings.Contains(page, "Next: S01E01") {
		t.Error("unmarking season 1 did not make S01E01 next again")
	}
}

// requestWith sends a request with the given cookies.
func requestWith(t *testing.T, s *Server, method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Datastar-Request", "true")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// With spoilers hidden, unwatched and unaired episodes keep their summaries
// and stills out of the page until the user asks for one.
func TestHiddenSpoilersKeepUnwatchedEpisodesSummariesOutOfThePage(t *testing.T) {
	s, cookie := severanceServer(t)
	requestWith(t, s, http.MethodPost, "/api/episodes?tv=95396&season=1&episode=1&watched=1", cookie)
	on := requestWith(t, s, http.MethodPost, "/api/episodes/spoilers?on=1&tv=95396&season=1", cookie)
	var spoilers *http.Cookie
	for _, c := range on.Result().Cookies() {
		if c.Name == "mt_spoilers" {
			spoilers = c
		}
	}
	if spoilers == nil || spoilers.Value != "1" {
		t.Fatalf("turning spoiler protection on set no cookie:\n%s", on.Body)
	}
	page := html.UnescapeString(requestWith(t, s, http.MethodGet, "/tv/95396?season=1", cookie, spoilers).Body.String())
	if !strings.Contains(page, "Plot of S1E1") || !strings.Contains(page, "/still-1-1.jpg") {
		t.Error("a watched episode's summary is hidden")
	}
	if strings.Contains(page, "Plot of S1E2") || strings.Contains(page, "/still-1-2.jpg") {
		t.Error("an unwatched episode's summary or still is in the page")
	}
	if !strings.Contains(page, "Hide spoilers") || !strings.Contains(page, "@post('/api/episodes/spoilers?on=0&tv=95396&season=1')") {
		t.Error("the protected season offers no button to show spoilers again")
	}
	for _, want := range []string{"Hidden to avoid spoilers", "@get('/api/episodes/reveal?tv=95396&season=1&episode=2')", `aria-pressed="true"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the protected season lacks %q", want)
		}
	}
	if s2 := html.UnescapeString(requestWith(t, s, http.MethodGet, "/tv/95396?season=2", cookie, spoilers).Body.String()); strings.Contains(s2, "Plot of S2E3") {
		t.Error("an episode that has not aired leaks its summary")
	}
	revealed := html.UnescapeString(requestWith(t, s, http.MethodGet, "/api/episodes/reveal?tv=95396&season=1&episode=2", cookie, spoilers).Body.String())
	if !strings.Contains(revealed, "Plot of S1E2") || !strings.Contains(revealed, `id="episode-card-1-2"`) {
		t.Errorf("revealing S1E2 answered:\n%s", revealed)
	}
	if off := html.UnescapeString(requestWith(t, s, http.MethodGet, "/tv/95396?season=1", cookie).Body.String()); !strings.Contains(off, "Plot of S1E2") {
		t.Error("without spoiler protection the summaries are hidden")
	}
}

// Only a signed-in user's requests look up episodes on TMDB.
func TestSignedOutVisitorsCannotLookUpEpisodes(t *testing.T) {
	s, _ := severanceServer(t)
	for _, path := range []string{
		"/api/episodes/reveal?tv=95396&season=1&episode=2",
		"/api/episodes/spoilers?on=1&tv=95396&season=1",
	} {
		method := http.MethodGet
		if strings.Contains(path, "spoilers") {
			method = http.MethodPost
		}
		res := requestWith(t, s, method, path)
		if res.Code != http.StatusUnauthorized || strings.Contains(res.Body.String(), "Plot of") {
			t.Errorf("%s %s signed out = %d:\n%s", method, path, res.Code, res.Body)
		}
	}
}
