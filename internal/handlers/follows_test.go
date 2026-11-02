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

// Following series lists their next episodes, dated soonest first, then
// those with no date announced, then those that ended.
func TestFollowedSeriesShowTheirNextEpisodes(t *testing.T) {
	series := map[int]*tmdb.TVDetails{
		95396: {MediaItem: tmdb.MediaItem{ID: 95396, Title: "Severance", MediaType: "tv"}, Status: "Returning Series",
			NextEpisode: tmdb.Airing{Season: 2, Number: 5, Name: "Trojan's Horse", AirDate: time.Date(2099, time.January, 10, 0, 0, 0, 0, time.UTC)}},
		1396: {MediaItem: tmdb.MediaItem{ID: 1396, Title: "Breaking Bad", MediaType: "tv"}, Status: "Ended"},
		3:    {MediaItem: tmdb.MediaItem{ID: 3, Title: "Silo", MediaType: "tv"}, Status: "Returning Series"},
	}
	s := newMediaServer(t, &fakeTMDBDetails{series: series}, nil, "")
	cookie := signedIn(t, s)
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
	if page := send(http.MethodGet, "/tv/95396"); !strings.Contains(page, "@get('/api/follow/state?id=95396')") {
		t.Error("the series page does not offer to follow it")
	}
	for _, id := range []string{"1396", "3", "95396"} {
		if body := send(http.MethodPost, "/api/follow?id="+id); !strings.Contains(body, `aria-pressed="true"`) {
			t.Errorf("following %s answered:\n%s", id, body)
		}
	}
	page := send(http.MethodGet, "/calendar")
	sev, silo, bb := strings.Index(page, "Severance"), strings.Index(page, "Silo"), strings.Index(page, "Breaking Bad")
	if sev < 0 || silo < 0 || bb < 0 || sev > silo || silo > bb {
		t.Errorf("upcoming order: Severance %d, Silo %d, Breaking Bad %d; want dated, undated, ended", sev, silo, bb)
	}
	for _, want := range []string{"S02E05", "Trojan's Horse", "10 Jan 2099", "Date not announced", "Ended"} {
		if !strings.Contains(page, want) {
			t.Errorf("upcoming page lacks %q", want)
		}
	}
	send(http.MethodDelete, "/api/follow?id=1396")
	if page := send(http.MethodGet, "/calendar"); strings.Contains(page, "Breaking Bad") {
		t.Error("an unfollowed series is still listed")
	}
}
