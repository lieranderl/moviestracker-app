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

// Recommendations follow what the user tells them: a title they rated low
// starts no row, a hidden title shows nowhere until undone, a title they
// want more like starts the first row, and a reset forgets it all.
func TestRecommendationsFollowTheUsersFeedback(t *testing.T) {
	movie := func(id int, recs ...int) *tmdb.MovieDetails {
		m := &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprint("Movie ", id), MediaType: "movie"}}
		for _, r := range recs {
			m.Recommendations = append(m.Recommendations, tmdb.MediaItem{ID: r, Title: fmt.Sprint("Movie ", r), MediaType: "movie"})
		}
		return m
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{
		1: movie(1, 11), 2: movie(2, 12, 13), 12: movie(12), 13: movie(13, 14),
	}}, nil, "")
	cookie := signedIn(t, s)
	send := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Datastar-Request", "true")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
		}
		return html.UnescapeString(rec.Body.String())
	}
	send(http.MethodPost, "/api/titles/state?type=movie&id=1&state=watched", "")
	send(http.MethodPost, "/api/titles/state?type=movie&id=2&state=watched", "")
	send(http.MethodPost, "/api/titles/review?type=movie&id=1", `{"reviewRating":3}`)

	rows := send(http.MethodGet, "/api/recommendations", "")
	if strings.Contains(rows, `href="/movie/11"`) || !strings.Contains(rows, `href="/movie/12"`) {
		t.Errorf("a title rated 3 still starts a row, or a liked one does not:\n%s", rows)
	}
	for _, want := range []string{"@post('/api/recommendations/feedback?type=movie&id=12&feedback=notInterested&on=1')", "@post('/api/recommendations/feedback?type=movie&id=13&feedback=moreLikeThis&on=1')"} {
		if !strings.Contains(rows, want) {
			t.Errorf("recommendation cards lack %q", want)
		}
	}
	hidden := send(http.MethodPost, "/api/recommendations/feedback?type=movie&id=12&feedback=notInterested&on=1", "")
	if strings.Contains(hidden, `href="/movie/12"`) || !strings.Contains(hidden, "@post('/api/recommendations/feedback?type=movie&id=12&feedback=notInterested&on=0')") {
		t.Errorf("hiding movie 12 should drop it and offer Undo:\n%s", hidden)
	}
	if strings.Contains(send(http.MethodGet, "/api/recommendations", ""), `href="/movie/12"`) {
		t.Error("a hidden title came back on the next visit")
	}
	more := send(http.MethodPost, "/api/recommendations/feedback?type=movie&id=13&feedback=moreLikeThis&on=1", "")
	first := strings.Index(more, "More like Movie 13")
	if first < 0 || strings.Index(more, "Because you watched") < first || !strings.Contains(more, `href="/movie/14"`) {
		t.Errorf("asking for more like movie 13 should start the first row:\n%s", more)
	}
	reset := send(http.MethodPost, "/api/recommendations/feedback/reset", "")
	if !strings.Contains(reset, `href="/movie/12"`) || strings.Contains(reset, "More like Movie 13") {
		t.Errorf("a reset should bring movie 12 back and drop the extra row:\n%s", reset)
	}
}
