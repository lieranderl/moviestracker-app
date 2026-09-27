package handlers

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestAMoviesCollectionIsShownBeforeRecommendations(t *testing.T) {
	darkKnight := &tmdb.MovieDetails{
		MediaItem: tmdb.MediaItem{ID: 155, Title: "The Dark Knight", MediaType: "movie", ReleaseDate: "2008-07-16"},
		Collection: &tmdb.Collection{ID: 263, Name: "The Dark Knight Collection", Parts: []tmdb.MediaItem{
			{ID: 272, Title: "Batman Begins", MediaType: "movie"},
			{ID: 155, Title: "The Dark Knight", MediaType: "movie"},
			{ID: 49026, Title: "The Dark Knight Rises", MediaType: "movie"},
		}},
		Recommendations: []tmdb.MediaItem{{ID: 27205, Title: "Inception", MediaType: "movie"}},
	}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{155: darkKnight}}, &fakeJacRed{}, "")

	body := get(t, server, "/movie/155", true).Body.String()

	collection, recommended := strings.Index(body, "The Dark Knight Collection"), strings.Index(body, "Recommended")
	if collection < 0 || recommended < 0 || collection > recommended {
		t.Fatalf("the collection (at %d) should come before Recommended (at %d)", collection, recommended)
	}
	rail := body[collection:recommended]
	for _, want := range []string{`href="/movie/272"`, `href="/movie/49026"`, `aria-current="page"`} {
		if !strings.Contains(rail, want) {
			t.Errorf("collection rail lacks %q", want)
		}
	}
	// The film marked is the movie itself: the first link after the mark.
	marked := rail[strings.Index(rail, `aria-current="page"`):]
	if strings.Count(rail, `aria-current="page"`) != 1 || !strings.HasPrefix(marked[strings.Index(marked, `href="/movie/`):], `href="/movie/155"`) {
		t.Error("the movie itself should be the one marked in its collection")
	}
}
