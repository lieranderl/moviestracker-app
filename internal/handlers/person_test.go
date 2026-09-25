package handlers

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

var leo = &tmdb.Person{
	ID: 6193, Name: "Leonardo DiCaprio", KnownForDepartment: "Acting",
	Biography: "Born in Los Angeles.\n\nFounded a foundation.",
	Birthday:  "1974-11-11", PlaceOfBirth: "Los Angeles, California, USA", ProfilePath: "/leo.jpg",
	KnownFor: []tmdb.MediaItem{{ID: 27205, Title: "Inception", MediaType: "movie", PosterPath: "/inception.jpg"}},
	Credits: []tmdb.Credit{
		{MediaItem: tmdb.MediaItem{ID: 27205, Title: "Inception", MediaType: "movie", ReleaseDate: "2010-07-15"}, Role: "Cobb"},
		{MediaItem: tmdb.MediaItem{ID: 2222, Title: "Growing Pains", MediaType: "tv", ReleaseDate: "1985-09-24"}, Role: "Luke Brower"},
	},
}

func TestPersonPageShowsProfileKnownForAndCredits(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{people: map[int]*tmdb.Person{6193: leo}}, &fakeJacRed{}, "")

	rec := get(t, server, "/person/6193", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		"<title>Leonardo DiCaprio · Moviestracker</title>",
		"https://image.tmdb.org/t/p/h632/leo.jpg",
		"Acting", "1974-11-11", "Los Angeles, California, USA",
		"<p>Born in Los Angeles.</p>", "<p>Founded a foundation.</p>",
		"Known for", `href="/movie/27205"`,
		"Movies", "TV", "Cobb", `href="/tv/2222"`, "Luke Brower", "2010", "1985",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("person page missing %q", want)
		}
	}
}

func TestUnknownPersonIsNotFound(t *testing.T) {
	server := newMediaServer(t, &fakeTMDBDetails{}, &fakeJacRed{}, "")

	if rec := get(t, server, "/person/1", true); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
