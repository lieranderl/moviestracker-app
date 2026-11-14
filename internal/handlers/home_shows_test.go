package handlers

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// daysAway is the day that many days from today, as TMDB dates episodes.
func daysAway(days int) time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, days)
}

// homeShows follows the series, in order, and answers the home row.
func homeShows(t *testing.T, s *Server, cookie *http.Cookie, follow ...string) string {
	t.Helper()
	send := func(method, path string) string {
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
	for _, id := range follow {
		send(http.MethodPost, "/api/follow?id="+id)
	}
	return send(http.MethodGet, "/api/home/shows")
}

func TestHomeShowsFollowedSeriesAiringWithinAWeek(t *testing.T) {
	soon, later := daysAway(3), daysAway(10)
	series := map[int]*tmdb.TVDetails{
		95396: {MediaItem: tmdb.MediaItem{ID: 95396, Title: "Severance", MediaType: "tv"}, Status: "Returning Series",
			NextEpisode: tmdb.Airing{Season: 2, Number: 5, Name: "Trojan's Horse", AirDate: soon}},
		3: {MediaItem: tmdb.MediaItem{ID: 3, Title: "Silo", MediaType: "tv"}, Status: "Returning Series",
			NextEpisode: tmdb.Airing{Season: 3, Number: 1, AirDate: later}},
	}
	s := newMediaServer(t, &fakeTMDBDetails{series: series}, nil, "")
	row := homeShows(t, s, signedIn(t, s), "95396", "3")
	for _, want := range []string{"Your shows this week", "Severance", "S02E05", soon.Format("2 Jan 2006"), `href="/calendar"`} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %q:\n%s", want, row)
		}
	}
	if strings.Contains(row, "Silo") {
		t.Error("a series airing in 10 days is in this week's row")
	}
}

// airedSeries is a followed series whose latest episode, S01E02 of three,
// aired that many days ago, with nothing announced after it.
func airedSeries(id int, title string, daysAgo int) (*tmdb.TVDetails, *tmdb.Season) {
	aired := daysAway(-daysAgo)
	tv := &tmdb.TVDetails{MediaItem: tmdb.MediaItem{ID: id, Title: title, MediaType: "tv"}, Status: "Returning Series",
		LastEpisode: tmdb.Airing{Season: 1, Number: 2, AirDate: aired},
		Seasons:     []tmdb.SeasonSummary{{Number: 1, EpisodeCount: 3, AirDate: aired.AddDate(0, 0, -7).Format(time.DateOnly)}}}
	season := &tmdb.Season{ShowID: id, Number: 1, Episodes: []tmdb.Episode{
		{Number: 1, SeasonNumber: 1, AirDate: aired.AddDate(0, 0, -7).Format(time.DateOnly)},
		{Number: 2, SeasonNumber: 1, AirDate: aired.Format(time.DateOnly)},
	}}
	return tv, season
}

func TestHomeShowsANewEpisodeUntilItIsWatched(t *testing.T) {
	fresh, freshSeason := airedSeries(95396, "Severance", 2)
	old, _ := airedSeries(3, "Silo", 20)
	s := newMediaServer(t, &fakeTMDBDetails{
		series:  map[int]*tmdb.TVDetails{95396: fresh, 3: old},
		seasons: map[string]*tmdb.Season{"95396/1": freshSeason},
	}, nil, "")
	cookie := signedIn(t, s)
	row := homeShows(t, s, cookie, "95396", "3")
	for _, want := range []string{"Severance", "New episode", "S01E02"} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %q:\n%s", want, row)
		}
	}
	if strings.Contains(row, "Silo") {
		t.Error("an episode aired 20 days ago is in this week's row")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/episodes?tv=95396&season=1&episode=2&watched=1", nil)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("marking the episode watched = %d %s", rec.Code, rec.Body)
	}
	if row := homeShows(t, s, cookie); strings.Contains(row, "Severance") {
		t.Errorf("a watched new episode is still in the row:\n%s", row)
	}
}

func TestHomeShowsOfferTheNextEpisodeToContinueAStartedSeries(t *testing.T) {
	started, startedSeason := airedSeries(95396, "Severance", 2)
	caughtUp, caughtUpSeason := airedSeries(3, "Silo", 30)
	caughtUp.NextEpisode = tmdb.Airing{Season: 1, Number: 3, AirDate: daysAway(1)}
	untouched, _ := airedSeries(7, "Andor", 1)
	s := newMediaServer(t, &fakeTMDBDetails{
		series:  map[int]*tmdb.TVDetails{95396: started, 3: caughtUp, 7: untouched},
		seasons: map[string]*tmdb.Season{"95396/1": startedSeason, "3/1": caughtUpSeason},
	}, nil, "")
	cookie := signedIn(t, s)
	for _, mark := range []string{"/api/episodes?tv=95396&season=1&episode=1&watched=1", "/api/episodes/season?tv=3&season=1&watched=1"} {
		req := httptest.NewRequest(http.MethodPost, mark, nil)
		req.Header.Set("Datastar-Request", "true")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s = %d %s", mark, rec.Code, rec.Body)
		}
	}
	row := homeShows(t, s, cookie, "95396", "3", "7")
	if !strings.Contains(row, `href="/tv/95396?season=1#seasons"`) || !strings.Contains(row, "Continue with S01E02") {
		t.Errorf("row does not offer the first unwatched episode of a started series:\n%s", row)
	}
	if strings.Contains(row, `href="/tv/3?season=`) {
		t.Error("row offers to continue a series with nothing left to watch")
	}
	if !strings.Contains(row, "Andor") || strings.Contains(row, `href="/tv/7?season=`) || strings.Count(row, "Continue with") != 1 {
		t.Errorf("row offers to continue a series the user has not started:\n%s", row)
	}
}

func TestHomeShowsAreOnlyForASignedInUserAndEmptyWithNothingThisWeek(t *testing.T) {
	quiet, _ := airedSeries(3, "Silo", 30)
	s := newMediaServer(t, &fakeTMDBDetails{series: map[int]*tmdb.TVDetails{3: quiet}}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/home/shows", nil)
	req.Header.Set("Datastar-Request", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "shows-this-week") {
		t.Errorf("signed out: %d %s", rec.Code, rec.Body)
	}
	row := homeShows(t, s, signedIn(t, s), "3")
	if !strings.Contains(row, `<div id="shows-this-week" class="contents"></div>`) {
		t.Errorf("a week with no episodes does not leave the row empty:\n%s", row)
	}
}
