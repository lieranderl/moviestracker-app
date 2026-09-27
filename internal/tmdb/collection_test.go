package tmdb

import (
	"context"
	"testing"
)

func TestAMovieInACollectionListsTheCollectionsFilmsInReleaseOrder(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{
		"/3/movie/155": `{"id": 155, "title": "The Dark Knight", "release_date": "2008-07-16",
			"belongs_to_collection": {"id": 263, "name": "The Dark Knight Collection"}}`,
		"/3/movie/49026": `{"id": 49026, "title": "The Dark Knight Rises", "release_date": "2012-07-17",
			"belongs_to_collection": {"id": 263, "name": "The Dark Knight Collection"}}`,
		"/3/collection/263": `{"id": 263, "name": "The Dark Knight Collection", "parts": [
			{"id": 49026, "title": "The Dark Knight Rises", "release_date": "2012-07-17", "media_type": "movie"},
			{"id": 999, "title": "Announced", "release_date": ""},
			{"id": 272, "title": "Batman Begins", "release_date": "2005-06-10"},
			{"id": 155, "title": "The Dark Knight", "release_date": "2008-07-16"}
		]}`,
	})

	m, err := client.Movie(context.Background(), 155)
	if err != nil {
		t.Fatal(err)
	}
	if m.Collection == nil || m.Collection.Name != "The Dark Knight Collection" {
		t.Fatalf("Collection = %+v, want The Dark Knight Collection", m.Collection)
	}
	var got []string
	for _, p := range m.Collection.Parts {
		got = append(got, p.Title)
		if p.MediaType != "movie" {
			t.Errorf("%s has media type %q", p.Title, p.MediaType)
		}
	}
	if want := "Batman Begins,The Dark Knight,The Dark Knight Rises,Announced"; join(got) != want {
		t.Errorf("parts = %s, want %s", join(got), want)
	}

	// Its other films share the collection's one request.
	if _, err := client.Movie(context.Background(), 49026); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Errorf("requests = %d, want 3 (two movies, one collection)", hits.Load())
	}
}

func TestAMovieStillLoadsWhenItsCollectionDoesNot(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{
		"/3/movie/1": `{"id": 1, "title": "Lonely", "belongs_to_collection": {"id": 404, "name": "Gone"}}`,
		"/3/movie/2": `{"id": 2, "title": "Standalone"}`,
	})
	for _, id := range []int{1, 2} {
		m, err := client.Movie(context.Background(), id)
		if err != nil || m.Collection != nil {
			t.Errorf("Movie(%d) = collection %+v, %v; want the movie without one", id, m.Collection, err)
		}
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
