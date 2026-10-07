package tmdb

import (
	"context"
	"testing"
	"time"
)

func TestAMoviesFirstReleaseOfEachKindIsItsEarliestAnywhere(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{"/3/movie/27205": `{
		"id": 27205, "title": "Inception",
		"release_dates": {"results": [
			{"iso_3166_1": "US", "release_dates": [
				{"type": 1, "release_date": "2010-07-08T00:00:00.000Z"},
				{"type": 3, "release_date": "2010-07-16T00:00:00.000Z"},
				{"type": 4, "release_date": "2010-12-03T00:00:00.000Z"},
				{"type": 5, "release_date": "2010-12-07T00:00:00.000Z"}
			]},
			{"iso_3166_1": "GB", "release_dates": [
				{"type": 2, "release_date": "2010-07-15T00:00:00.000Z"},
				{"type": 4, "release_date": "2010-11-29T00:00:00.000Z"},
				{"type": 5, "release_date": "bogus"}
			]}
		]}
	}`})

	m, err := client.Movie(context.Background(), 27205)
	if err != nil {
		t.Fatalf("Movie(): %v", err)
	}
	day := func(m time.Month, d int) time.Time { return time.Date(2010, m, d, 0, 0, 0, 0, time.UTC) }
	if want := (Releases{Cinema: day(time.July, 15), Digital: day(time.November, 29), Physical: day(time.December, 7)}); m.Releases != want {
		t.Errorf("Releases = %+v, want %+v (a premiere is not a cinema release)", m.Releases, want)
	}
}
