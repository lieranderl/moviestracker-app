package jacred

import (
	"strings"
	"testing"
	"time"
)

func TestAReleaseStreamsAtItsSizeOverItsRuntime(t *testing.T) {
	const gb = 1_000_000_000
	for name, c := range map[string]struct {
		r    Result
		want float64
	}{
		"26 GB 4K over 2 h 14 min":   {Result{Size: 26 * gb, Runtime: 134 * time.Minute}, 25.87},
		"2 GB 1080p over 2 h 14 min": {Result{Size: 2 * gb, Runtime: 134 * time.Minute}, 1.99},
		"runtime unknown":            {Result{Size: 2 * gb}, 0},
		"size unknown":               {Result{Runtime: time.Hour}, 0},
		"a 60 GB pack over 40 min":   {Result{Size: 60 * gb, Runtime: 40 * time.Minute}, 0}, // implausible: the runtime is not this release's
		"a 100 MB release over 2 h":  {Result{Size: gb / 10, Runtime: 2 * time.Hour}, 0},
	} {
		if got := c.r.Mbps(); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("%s: Mbps() = %.2f, want %.2f", name, got, c.want)
		}
	}
}

func TestSortByBitrateRanksTheRichestReleasesFirstAndUnknownLast(t *testing.T) {
	const gb = 1_000_000_000
	in := []Result{
		{Title: "unknown", Seeders: 900, Size: 8 * gb},
		{Title: "web-dl", Seeders: 50, Size: 9 * gb, Runtime: 2 * time.Hour},
		{Title: "remux", Seeders: 10, Size: 58 * gb, Runtime: 2 * time.Hour},
		{Title: "rip", Seeders: 400, Size: 2 * gb, Runtime: 2 * time.Hour},
	}
	var titles []string
	for _, r := range Sort(in, "bitrate") {
		titles = append(titles, r.Title)
	}
	if got, want := strings.Join(titles, "|"), "remux|web-dl|rip|unknown"; got != want {
		t.Errorf(`Sort("bitrate") = %s, want %s`, got, want)
	}
}

func TestAReleaseOfASeasonHoldsItsEpisodes(t *testing.T) {
	const seasonEpisodes = 10
	for title, want := range map[string]int{
		"Разделение / Severance [S02] (2025) WEB-DL 1080p":             10,
		"Разделение / Severance (2025) WEB-DL 1080p | серии 1-8 из 10": 8,
		"Разделение / Severance [01-04 из 10] (2025) WEBRip 720p":      4,
		"Severance.S02E05.1080p.WEB-DL.H264":                           1,
		"Severance.S02E01-E03.2160p.WEB-DL.DV":                         3,
		"Severance.S02E07-08.1080p.WEBRip":                             2,
	} {
		if got := (Result{Title: title, Seasons: []int{2}}).Episodes(seasonEpisodes); got != want {
			t.Errorf("Episodes(%q) = %d, want %d", title, got, want)
		}
	}
	if got := (Result{Title: "Severance S01-S02 1080p", Seasons: []int{1, 2}}).Episodes(seasonEpisodes); got != 0 {
		t.Errorf("a release of two seasons holds %d episodes of one, want 0: unknown", got)
	}
	// JacRed may leave the seasons out: the title still says it spans several.
	for _, title := range []string{
		"Severance S01-S02 1080p",
		"Severance [S01-02] 2160p",
		"Разделение / Severance (Сезон 1-2) WEB-DL 1080p",
		"Разделение / Severance [Сезоны: 1-2] WEBRip",
		"Severance Seasons 1-2 Complete 720p",
	} {
		if got := (Result{Title: title}).Episodes(seasonEpisodes); got != 0 {
			t.Errorf("Episodes(%q) without seasons = %d, want 0: it spans several seasons", title, got)
		}
	}
	if got := (Result{Title: "Severance S02 1080p"}).Episodes(seasonEpisodes); got != seasonEpisodes {
		t.Errorf("Episodes of a one-season title without seasons = %d, want the season's %d", got, seasonEpisodes)
	}
}
