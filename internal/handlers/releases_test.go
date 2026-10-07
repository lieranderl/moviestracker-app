package handlers

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestTheMovieSourcesSayWhenItCameOutInCinemasDigitallyAndOnDisc(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	movie := *inception
	movie.Releases = tmdb.Releases{Cinema: day(2010, time.July, 15), Digital: day(2010, time.November, 29), Physical: day(2010, time.December, 7)}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: &movie}}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movie/27205", true).Body.String())

	for _, want := range []string{"In cinemas", "15 Jul 2010", "Digital", "29 Nov 2010", "DVD / Blu-ray", "7 Dec 2010"} {
		if !strings.Contains(body, want) {
			t.Errorf("movie page lacks %q", want)
		}
	}
	if strings.Contains(body, "Not out digitally yet") {
		t.Error("a movie out digitally should not warn that only cinema recordings exist")
	}
}

func TestAMovieOnlyInCinemasWarnsThatItsSourcesAreCinemaRecordings(t *testing.T) {
	movie := *inception
	movie.Releases = tmdb.Releases{Cinema: time.Now().AddDate(0, -1, 0), Digital: time.Now().AddDate(0, 1, 0)}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: &movie}}, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/movie/27205", true).Body.String())

	for _, want := range []string{"Not out digitally yet", "expected"} {
		if !strings.Contains(body, want) {
			t.Errorf("movie page lacks %q", want)
		}
	}
}
