package tmdb

import (
	"context"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

func TestAMoviesPosterComesInTheLanguageAskedAndIsKept(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{"/3/movie/438631": `{"id": 438631, "title": "Dune", "poster_path": "/dune-en.jpg"}`})
	ctx := i18n.WithLang(context.Background(), i18n.English)
	for range 2 {
		poster, err := client.MoviePoster(ctx, 438631)
		if err != nil || poster != "/dune-en.jpg" {
			t.Fatalf("MoviePoster() = %q, %v; want /dune-en.jpg", poster, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("requests = %d, want 1: the poster is kept", hits.Load())
	}
}

// A title's name and poster come in the language asked, for movies and
// series alike, and are kept.
func TestATitlesNameComesInTheLanguageAskedAndIsKept(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{
		"/3/movie/438631": `{"id": 438631, "title": "Дюна", "poster_path": "/dune-ru.jpg"}`,
		"/3/tv/95396":     `{"id": 95396, "name": "Разделение", "poster_path": "/sev-ru.jpg"}`,
	})
	ctx := i18n.WithLang(context.Background(), i18n.Russian)
	for range 2 {
		movie, err := client.Title(ctx, "movie", 438631)
		if err != nil || movie.Title != "Дюна" || movie.PosterPath != "/dune-ru.jpg" || movie.MediaType != "movie" || movie.ID != 438631 {
			t.Fatalf("Title(movie) = %+v, %v", movie, err)
		}
		series, err := client.Title(ctx, "tv", 95396)
		if err != nil || series.Title != "Разделение" || series.PosterPath != "/sev-ru.jpg" || series.MediaType != "tv" {
			t.Fatalf("Title(tv) = %+v, %v", series, err)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want 2: titles are kept", hits.Load())
	}
	if _, err := client.Title(ctx, "person", 1); err == nil {
		t.Error("a person is not a title")
	}
}
