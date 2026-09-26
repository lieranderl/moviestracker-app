package views_test

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// jacredCredit is the credit Moviestracker's jacred.su project asks for
// beside the results: its referral link, which counts the visits it brings.
const jacredCredit = `href="https://jacred.su/r/c2abc53e-233f-42ad-9f6a-e12f2eeaef9c"` // #nosec G101 -- a referral link, not a credential

func TestTitleSourcesCreditJacRedWithALink(t *testing.T) {
	for name, c := range map[string]string{
		"movie": render(t, views.MoviePage(testUser, &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: 1, Title: "The Odyssey", MediaType: "movie"}})),
		"tv": render(t, views.TVPage(testUser, &tmdb.TVDetails{MediaItem: tmdb.MediaItem{ID: 2, Title: "Lanterns", MediaType: "tv"}, Seasons: []tmdb.SeasonSummary{{Number: 1}}},
			&tmdb.Season{Number: 1}, 1)),
	} {
		out := html.UnescapeString(c)
		sources := out[strings.Index(out, `id="sources"`):]
		if !strings.Contains(sources, "Search is powered by JacRed") || !strings.Contains(sources, jacredCredit) {
			t.Errorf("%s: the Sources section lacks a link crediting JacRed", name)
		}
	}
}

// Torrents can be added by hand, so the TorrServer list credits no one.
func TestTheTorrServerListDoesNotCreditJacRed(t *testing.T) {
	torrents := []torrserver.Torrent{{Hash: "abc", Title: "The Odyssey 2026 1080p"}}
	out := render(t, views.TorrServer(testUser, "http://nas:8090", "http://192.168.1.20:8095", testLinks, torrserver.EchoInfo{Version: "1.0"}, torrents, views.GStreamerSetup{}))
	if strings.Contains(out, "powered by JacRed") {
		t.Error("the TorrServer page credits JacRed for torrents it may not have found")
	}
}
