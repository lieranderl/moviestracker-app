package torrserver_test

import (
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestEpisodeFilesAreRecognisedByTheirCode(t *testing.T) {
	cases := map[string]string{
		"Breaking.Bad.S01.2160p.WEB-DL/Breaking.Bad.S01E02.Cats.in.the.Bag.2160p.mkv": "S01E02",
		"show.s1e2.720p.mkv":                     "S01E02",
		"Show.S02E05-E06.1080p.mkv":              "S02E05–E06",
		"Show.S02E05E06.1080p.mkv":               "S02E05–E06",
		"Show 3x07 HDTV.avi":                     "S03E07",
		"Show.Season.1/Show.E04.1080p.mkv":       "S01E04",
		"Show/Season 2/Episode 5.mkv":            "S02E05",
		"Show/S03/05. Pilot.mkv":                 "S03E05",
		"Сериал/Сезон 2/Серия 07.mkv":            "S02E07",
		"Сериал/7 серия.mkv":                     "E07",
		"Show/Ep 12.mkv":                         "E12",
		"Movie.2021.1920x1080.x264.H.265.mkv":    "",
		"Movie.2160p.HDR10.DDP5.1.Atmos.mkv":     "",
		"Movie.2021.1080p.BluRay.E-AC3.mkv":      "",
		"Show.S01.Extras/Behind.the.Scenes.mkv":  "",
		"The.Movie.Part.2.2019.1080p.WEB-DL.mkv": "",
		"Season 1/featurette.mkv":                "",
	}
	for path, want := range cases {
		if got := torrserver.EpisodeCode(path); got != want {
			t.Errorf("EpisodeCode(%q) = %q, want %q", path, got, want)
		}
	}
}

// A file names its episodes exactly only with a season in its own name:
// that is what may be marked watched without asking.
func TestOnlyFilesNamingTheirSeasonGiveExactEpisodes(t *testing.T) {
	cases := []struct {
		path             string
		season, from, to int
		exact            bool
	}{
		{"Show.S01/Show.S01E02.1080p.mkv", 1, 2, 2, true},
		{"Show.S02E05-E06.mkv", 2, 5, 6, true},
		{"Show 3x07 HDTV.avi", 3, 7, 7, true},
		{"Show/Season 2/Episode 5.mkv", 0, 0, 0, false}, // the season comes from a folder
		{"Сериал/7 серия.mkv", 0, 0, 0, false},
		{"Movie.2021.1080p.mkv", 0, 0, 0, false},
	}
	for _, c := range cases {
		season, from, to, exact := torrserver.ExactEpisodes(c.path)
		if season != c.season || from != c.from || to != c.to || exact != c.exact {
			t.Errorf("ExactEpisodes(%q) = %d, %d, %d, %v; want %d, %d, %d, %v", c.path, season, from, to, exact, c.season, c.from, c.to, c.exact)
		}
	}
}
