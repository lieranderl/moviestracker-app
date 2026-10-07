package handlers

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

const gigabyte = 1_000_000_000

func TestMovieSourcesShowTheirBitrateOverTheMoviesRuntime(t *testing.T) {
	// Inception runs 148 minutes: 26 GB over it is about 23.4 Mbps.
	jr := &fakeJacRed{results: []jacred.Result{
		{Tracker: "a", Title: "Inception 2160p Remux", SourceURL: "https://a.example/1", Magnet: "magnet:?xt=urn:btih:aaaa", Seeders: 5, Size: 26 * gigabyte},
		{Tracker: "b", Title: "Inception 1080p WEBRip", SourceURL: "https://b.example/2", Magnet: "magnet:?xt=urn:btih:bbbb", Seeders: 90, Size: 2 * gigabyte},
	}}
	server := newMediaServer(t, &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{27205: inception}}, jr, "")

	body := html.UnescapeString(get(t, server, "/api/torrents?type=movie&id=27205&sort=bitrate", true).Body.String())

	for _, want := range []string{"23.4 Mbps", "1.8 Mbps", "$sort = 'bitrate'", `aria-pressed="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("results lack %q", want)
		}
	}
	if strings.Index(body, "Inception 2160p Remux") > strings.Index(body, "Inception 1080p WEBRip") {
		t.Error("sort=bitrate should list the highest bitrate first")
	}
}

func TestSeriesSourcesShowTheirBitrateOverTheEpisodesTheyHold(t *testing.T) {
	// Season 2 has 13 episodes of 47 minutes: a 20 GB pack of all of them
	// is about 4.4 Mbps, a 1.5 GB single episode about 4.3 Mbps.
	show := *breakingBad
	show.EpisodeRuntime = 47
	details := tvDetails()
	details.series[1396] = &show
	jr := &fakeJacRed{results: []jacred.Result{
		{Tracker: "a", Title: "Breaking Bad S02 1080p", SourceURL: "https://a.example/1", Magnet: "magnet:?xt=urn:btih:aaaa", Seeders: 50, Size: 20 * gigabyte, Seasons: []int{2}},
		{Tracker: "b", Title: "Breaking.Bad.S02E05.1080p", SourceURL: "https://b.example/2", Magnet: "magnet:?xt=urn:btih:bbbb", Seeders: 9, Size: 1_500_000_000, Seasons: []int{2}},
	}}
	server := newMediaServer(t, details, jr, "")

	body := get(t, server, "/api/torrents?type=tv&id=1396&season=2", true).Body.String()

	for _, want := range []string{"4.4 Mbps", "4.3 Mbps"} {
		if !strings.Contains(body, want) {
			t.Errorf("results lack %q", want)
		}
	}
}
