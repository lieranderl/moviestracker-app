package tmdb

import (
	"context"
	"testing"
	"time"
)

func TestASeriesKnowsItsLatestAndNextEpisode(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{
		"/3/tv/95396": `{
			"id": 95396, "name": "Severance", "status": "Returning Series",
			"last_episode_to_air": {"season_number": 2, "episode_number": 4, "name": "Woe's Hollow", "air_date": "2026-10-03", "runtime": 52},
			"next_episode_to_air": {"season_number": 2, "episode_number": 5, "name": "Trojan's Horse", "air_date": "2026-10-10"}
		}`,
		"/3/tv/1396": `{"id": 1396, "name": "Breaking Bad", "status": "Ended", "next_episode_to_air": null,
			"last_episode_to_air": {"season_number": 5, "episode_number": 16, "name": "Felina", "air_date": "2013-09-29"}}`,
	})

	running, err := client.TV(context.Background(), 95396)
	if err != nil {
		t.Fatalf("TV(): %v", err)
	}
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	if want := (Airing{Season: 2, Number: 4, Name: "Woe's Hollow", AirDate: day(2026, time.October, 3)}); running.LastEpisode != want {
		t.Errorf("LastEpisode = %+v, want %+v", running.LastEpisode, want)
	}
	if want := (Airing{Season: 2, Number: 5, Name: "Trojan's Horse", AirDate: day(2026, time.October, 10)}); running.NextEpisode != want {
		t.Errorf("NextEpisode = %+v, want %+v", running.NextEpisode, want)
	}

	ended, err := client.TV(context.Background(), 1396)
	if err != nil {
		t.Fatalf("TV(): %v", err)
	}
	if !ended.NextEpisode.AirDate.IsZero() || ended.NextEpisode.Number != 0 {
		t.Errorf("an ended show's NextEpisode = %+v, want none", ended.NextEpisode)
	}
}
