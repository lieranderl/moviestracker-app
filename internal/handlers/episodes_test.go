package handlers

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestASeriesStillAiringSaysWhatIsOutAndWhatComesNext(t *testing.T) {
	show := *breakingBad
	show.Status = "Returning Series"
	show.LastEpisode = tmdb.Airing{Season: 2, Number: 4, AirDate: time.Date(2026, time.October, 3, 0, 0, 0, 0, time.UTC)}
	show.NextEpisode = tmdb.Airing{Season: 2, Number: 5, AirDate: time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)}
	details := tvDetails()
	details.series[1396] = &show
	server := newMediaServer(t, details, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/tv/1396", true).Body.String())

	for _, want := range []string{"Next episode: S2 E5, 10 Oct 2026", "Season 2 · 4 of 13 aired"} {
		if !strings.Contains(body, want) {
			t.Errorf("series page lacks %q", want)
		}
	}
	if strings.Contains(body, "Season 1 ·") {
		t.Error("a season that has finished airing should not count its episodes")
	}
}
