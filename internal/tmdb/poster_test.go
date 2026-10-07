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
