package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestTheLocalHomePageRecommendsAtMostThreeRowsWithoutRepeats(t *testing.T) {
	related := func(ids ...int) []tmdb.MediaItem {
		var out []tmdb.MediaItem
		for _, id := range ids {
			out = append(out, tmdb.MediaItem{ID: id, Title: "Title", MediaType: "movie"})
		}
		return out
	}
	movies := map[int]*tmdb.MovieDetails{}
	for id, recs := range map[int][]int{4: {11, 12}, 3: {12, 13}, 2: {14}, 1: {15}} {
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: "Seed", MediaType: "movie"}, Recommendations: related(recs...)}
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies}, nil, "")
	for _, id := range []string{"1", "2", "3", "4"} {
		sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id="+id+"&state=watched")
	}
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := strings.Count(body, "Because you watched"); got != 3 {
		t.Errorf("%d rows, want 3", got)
	}
	if got := strings.Count(body, `href="/movie/12"`); got != 1 {
		t.Errorf("movie 12 shows %d times, want once", got)
	}
}
