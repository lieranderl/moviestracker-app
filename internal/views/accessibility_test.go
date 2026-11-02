package views_test

import (
	"context"
	"html"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// controlTag is an opening button or link tag and its content up to its close.
var controlTag = regexp.MustCompile(`(?s)<(button|a)\b([^>]*)>(.*?)</(?:button|a)>`)

func TestSeasonNavigationExposesTheCurrentPageAsALink(t *testing.T) {
	series := &tmdb.TVDetails{
		MediaItem: tmdb.MediaItem{ID: 95396, Title: "Severance", MediaType: "tv"},
		Seasons:   []tmdb.SeasonSummary{{Number: 1}, {Number: 2}},
	}
	out := html.UnescapeString(render(t, views.TVPage(testUser, series, &tmdb.Season{ShowID: 95396, Number: 2}, 2)))
	nav := regexp.MustCompile(`(?s)<nav[^>]*aria-label="Seasons".*?</nav>`).FindString(out)
	if nav == "" {
		t.Fatal("the series has no named season navigation")
	}
	if strings.Contains(nav, `role="tab"`) || strings.Count(nav, `aria-current="page"`) != 1 {
		t.Errorf("season navigation must expose one current page using native links: %s", nav)
	}
	current := regexp.MustCompile(`<a[^>]*href="/tv/95396\?season=2#seasons"[^>]*aria-current="page"`).FindString(nav)
	if current == "" {
		t.Error("Season 2 is not identified as the current season")
	}
}

// accessibleName says whether a control has a name assistive technology
// can read: a label attribute, a title, or visible text.
func accessibleName(attrs, content string) bool {
	if strings.Contains(attrs, "aria-label=") || strings.Contains(attrs, "aria-labelledby=") || strings.Contains(attrs, ` title="`) || strings.Contains(attrs, "data-text=") || strings.Contains(attrs, "data-attr:aria-label=") {
		return true
	}
	text := regexp.MustCompile(`(?s)<svg.*?</svg>|<[^>]+>`).ReplaceAllString(content, "")
	return strings.TrimSpace(html.UnescapeString(text)) != "" || strings.Contains(content, "data-text=") || regexp.MustCompile(`<img[^>]+alt="[^"]+"`).MatchString(content)
}

// Every button and link on the main pages has an accessible name.
func TestEveryControlHasAnAccessibleName(t *testing.T) {
	ctx := views.WithEpisodeMarks(context.Background(), views.EpisodeMarks{Track: true})
	series := &tmdb.TVDetails{MediaItem: tmdb.MediaItem{ID: 2, Title: "Severance", MediaType: "tv", TrailerKey: "abc"}, Seasons: []tmdb.SeasonSummary{{Number: 1, EpisodeCount: 2, AirDate: "2022-02-18"}}}
	season := &tmdb.Season{ShowID: 2, Number: 1, Episodes: []tmdb.Episode{{Number: 1, SeasonNumber: 1, Name: "Good News", AirDate: "2022-02-18"}}}
	torrents := []torrserver.Torrent{{Hash: "hash1", Title: "Show", FileStats: []torrserver.FileStat{{ID: 1, Path: "Show/S01E01.mkv"}, {ID: 2, Path: "Show/S01E02.mkv"}}}}
	pages := map[string]string{
		"home":       renderIn(t, ctx, views.Movies(testUser, []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie"}}, []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie"}}, nil, true, nil)),
		"movie":      renderIn(t, ctx, views.MoviePage(testUser, &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: 1, Title: "Dune", MediaType: "movie", TrailerKey: "abc"}})),
		"series":     renderIn(t, ctx, views.TVPage(testUser, series, season, 1)),
		"torrserver": renderIn(t, ctx, views.TorrServer(testUser, "http://nas:8090", "http://192.168.1.20:8095", testLinks, torrserver.EchoInfo{Version: "1.0", GSTAvailable: true, GSTKnown: true}, torrents, views.GStreamerSetup{})),
	}
	for page, out := range pages {
		for _, m := range controlTag.FindAllStringSubmatch(out, -1) {
			if !accessibleName(m[2], m[3]) {
				t.Errorf("%s: a %s has no accessible name: %.200s", page, m[1], m[0])
			}
		}
	}
}

// A viewer who asks for reduced motion gets a still billboard (no slide
// changes or trailers on their own) and no looping animations.
func TestReducedMotionStopsTheBillboardAndAnimations(t *testing.T) {
	out := html.UnescapeString(render(t, views.HeroBillboard([]tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie", TrailerKey: "abc"}, {ID: 2, Title: "Arrival", MediaType: "movie"}})))
	if !strings.Contains(out, "heroReduced: matchMedia('(prefers-reduced-motion: reduce)').matches") {
		t.Error("the billboard does not read the reduced-motion preference")
	}
	if !strings.Contains(out, "if ($heroReduced) return") {
		t.Error("the billboard still advances and plays trailers by itself for reduced motion")
	}
	css, err := os.ReadFile("../../frontend/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "@media (prefers-reduced-motion: reduce)") {
		t.Error("the stylesheet keeps animations for viewers who asked for reduced motion")
	}
}
