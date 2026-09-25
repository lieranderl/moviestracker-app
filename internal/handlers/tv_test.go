package handlers

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

var breakingBad = &tmdb.TVDetails{
	MediaItem: tmdb.MediaItem{
		ID: 1396, Title: "Breaking Bad", MediaType: "tv", ReleaseDate: "2008-01-20",
		Overview: "A chemistry teacher turns to crime.", VoteAverage: 8.9,
	},
	OriginalTitle: "Breaking Bad", Status: "Ended", LastAirDate: "2013-09-29", ContentRating: "TV-MA",
	NumberOfSeasons: 2, NumberOfEpisodes: 20,
	Creators: []tmdb.CrewMember{{ID: 66633, Name: "Vince Gilligan"}},
	Networks: []string{"AMC"},
	Seasons: []tmdb.SeasonSummary{
		{Number: 1, Name: "Season 1", EpisodeCount: 7, AirDate: "2008-01-20"},
		{Number: 2, Name: "Season 2", EpisodeCount: 13, AirDate: "2009-03-08"},
	},
	Cast: []tmdb.CastMember{{ID: 17419, Name: "Bryan Cranston", Character: "Walter White"}},
}

func tvDetails() *fakeTMDBDetails {
	return &fakeTMDBDetails{
		series: map[int]*tmdb.TVDetails{1396: breakingBad},
		seasons: map[string]*tmdb.Season{
			"1396/1": {ShowID: 1396, Number: 1, Name: "Season 1", Episodes: []tmdb.Episode{{SeasonNumber: 1, Number: 1, Name: "Pilot", Runtime: 58}}},
			"1396/2": {ShowID: 1396, Number: 2, Name: "Season 2", Episodes: []tmdb.Episode{{SeasonNumber: 2, Number: 1, Name: "Seven Thirty-Seven"}}},
		},
	}
}

func TestSeriesPageShowsSeasonsWithFirstSeasonEpisodes(t *testing.T) {
	server := newMediaServer(t, tvDetails(), &fakeJacRed{}, "")

	rec := get(t, server, "/tv/1396", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		"<title>Breaking Bad (2008) · Moviestracker</title>",
		"2008–2013", "2 seasons", "TV-MA",
		"Vince Gilligan", `href="/person/66633"`, "AMC",
		"Bryan Cranston", "Walter White",
		`href="/tv/1396?season=2#seasons"`,
		"S01E01", "Pilot", "58m",
		`@get('/api/torrents?type=tv&id=1396&sort=' + $sort + '&season=' + $sourceSeason`,
		`data-bind:source-season`, `<option value="2">Season 2</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("series page missing %q", want)
		}
	}
	if strings.Contains(body, "Seven Thirty-Seven") {
		t.Error("only the selected season's episodes should be rendered")
	}
}

func TestSeriesPageOpensRequestedSeason(t *testing.T) {
	server := newMediaServer(t, tvDetails(), &fakeJacRed{}, "")

	body := get(t, server, "/tv/1396?season=2", true).Body.String()

	if !strings.Contains(body, "Seven Thirty-Seven") || strings.Contains(body, "Pilot") {
		t.Error("expected season 2 episodes only")
	}
}

func TestSwitchingSeasonPatchesEpisodeList(t *testing.T) {
	server := newMediaServer(t, tvDetails(), &fakeJacRed{}, "")

	body := get(t, server, "/api/tv/1396/season/2", true).Body.String()

	if !strings.Contains(body, "datastar-patch-elements") || !strings.Contains(body, `id="season-episodes"`) || !strings.Contains(body, "Seven Thirty-Seven") {
		t.Errorf("expected season-episodes patch, got %q", body)
	}
	if !strings.Contains(body, "datastar-patch-signals") || !strings.Contains(body, `"season":2`) {
		t.Errorf("expected $season signal patch, got %q", body)
	}
}

func TestUnknownSeasonExplainsInsteadOfFailing(t *testing.T) {
	server := newMediaServer(t, tvDetails(), &fakeJacRed{}, "")

	body := get(t, server, "/api/tv/1396/season/9", true).Body.String()

	if !strings.Contains(body, "could not be loaded") {
		t.Errorf("expected an explanation, got %q", body)
	}
}

func TestUnknownSeriesIsNotFound(t *testing.T) {
	server := newMediaServer(t, tvDetails(), &fakeJacRed{}, "")

	if rec := get(t, server, "/tv/42", true); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if rec := get(t, server, "/tv/1396", false); rec.Code != http.StatusSeeOther {
		t.Fatalf("signed-out status = %d, want 303", rec.Code)
	}
}

func TestSeriesSourcesAreSearchedPerSeasonWithShowAndSeasonYears(t *testing.T) {
	jr := &fakeJacRed{}
	server := newMediaServer(t, tvDetails(), jr, "")

	body := get(t, server, "/api/torrents?type=tv&id=1396&season=2", true).Body.String()

	if len(jr.queries) != 1 {
		t.Fatalf("JacRed queries = %d, want 1", len(jr.queries))
	}
	q := jr.queries[0]
	if q.OriginalTitle != "Breaking Bad" || q.Season != 2 || q.Year != 2008 || q.SeasonYear != 2009 {
		t.Errorf("query = %+v, want season 2 with years 2008/2009", q)
	}
	if !strings.Contains(body, "Season 2") {
		t.Errorf("expected the season in the searching label, got %q", body)
	}
}

func TestSeriesSourcesRequireAKnownSeason(t *testing.T) {
	jr := &fakeJacRed{}
	server := newMediaServer(t, tvDetails(), jr, "")

	body := get(t, server, "/api/torrents?type=tv&id=1396&season=9", true).Body.String()

	if len(jr.queries) != 0 || !strings.Contains(body, "Choose a season") {
		t.Errorf("unknown season should not reach JacRed; got %d queries, body %q", len(jr.queries), body)
	}
}
