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
