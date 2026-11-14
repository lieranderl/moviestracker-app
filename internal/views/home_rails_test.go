package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

var homeMedia = []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie"}}

// railOf is the home rail with id: from its opening tag to the next rail's
// or the end of the rails.
func railOf(t *testing.T, out, id string) string {
	t.Helper()
	start := strings.Index(out, `<section id="`+id+`"`)
	if start < 0 {
		t.Fatalf("home lacks the %s rail", id)
	}
	rail, _, _ := strings.Cut(out[start+1:], `<section id="home-`)
	return rail
}

func TestHomeGroupsItsListsIntoATabbedRailOfMoviesAndOneOfSeries(t *testing.T) {
	out := html.UnescapeString(render(t, views.Movies(testUser, homeMedia, homeMedia, homeMedia, true, nil)))

	for rail, panels := range map[string][]string{
		"home-movies": {"trending-movies", "discover-now-playing", "discover-top-rated-movies"},
		"home-series": {"trending-series", "discover-popular-series", "discover-top-rated-series"},
	} {
		body := railOf(t, out, rail)
		if n := strings.Count(body, `role="tablist"`); n != 1 {
			t.Errorf("%s has %d tab lists, want 1", rail, n)
		}
		if n := strings.Count(body, `role="tab"`); n != len(panels) {
			t.Errorf("%s has %d tabs, want %d", rail, n, len(panels))
		}
		for i, panel := range panels {
			tab := regexp.MustCompile(`<button[^>]*id="tab-` + panel + `"[^>]*>`).FindString(body)
			if tab == "" || !strings.Contains(tab, `aria-controls="panel-`+panel+`"`) {
				t.Errorf("%s lacks a tab for %s: %q", rail, panel, tab)
			}
			if first := i == 0; strings.Contains(tab, `aria-selected="true"`) != first {
				t.Errorf("%s: tab %s selected at first = %t, want %t", rail, panel, !first, first)
			}
			pane := regexp.MustCompile(`<div[^>]*id="panel-` + panel + `"[^>]*>`).FindString(body)
			if !strings.Contains(pane, `role="tabpanel"`) || !strings.Contains(pane, `aria-labelledby="tab-`+panel+`"`) {
				t.Errorf("%s lacks a panel for %s: %q", rail, panel, pane)
			}
			if hidden := strings.Contains(pane, `class="hidden"`); hidden == (i == 0) {
				t.Errorf("%s: panel %s hidden at first = %t", rail, panel, hidden)
			}
			if !strings.Contains(body, `id="`+panel+`"`) {
				t.Errorf("%s lacks the %s list", rail, panel)
			}
		}
	}
	if strings.Contains(out, "popular-movies") {
		t.Error("home shows Popular movies, which repeats Trending and Now playing")
	}
}

func TestAHomeRailsTabShowsItsListAndItsLinkToTheWholeList(t *testing.T) {
	out := html.UnescapeString(render(t, views.Movies(testUser, homeMedia, homeMedia, homeMedia, true, nil)))
	body := railOf(t, out, "home-movies")

	for _, want := range []string{
		`data-signals:home-tab-movies="'trending-movies'"`,
		`data-on:click="$homeTabMovies = 'discover-now-playing'"`,
		`data-class:tab-active="$homeTabMovies == 'discover-now-playing'"`,
		`data-attr:aria-selected="$homeTabMovies == 'discover-now-playing' ? 'true' : 'false'"`,
		`data-class:hidden="!($homeTabMovies == 'discover-now-playing')"`,
		`href="/browse/trending-movies"`, `href="/browse/now-playing"`, `href="/browse/top-rated-movies"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the movies rail lacks %q", want)
		}
	}
}

func TestTheWebAppsHomeHasARailOfReleases(t *testing.T) {
	cloud := views.WithSite(context.Background(), views.Site{Cloud: true})
	out := html.UnescapeString(renderIn(t, cloud, views.Movies(testUser, homeMedia, homeMedia, homeMedia, true, nil)))

	body := railOf(t, out, "home-releases")
	for _, panel := range []string{"discover-latest-releases", "discover-hdr10-releases", "discover-dolby-vision-releases"} {
		if !strings.Contains(body, `id="tab-`+panel+`"`) || !strings.Contains(body, `id="`+panel+`"`) {
			t.Errorf("the releases rail lacks %s", panel)
		}
	}
	local := html.UnescapeString(render(t, views.Movies(testUser, homeMedia, homeMedia, homeMedia, true, nil)))
	if strings.Contains(local, `id="home-releases"`) {
		t.Error("the local app has no release feeds, yet shows a releases rail")
	}
}

func TestHomeWithoutTMDBDetailsShowsOnlyTheTrendingLists(t *testing.T) {
	out := html.UnescapeString(render(t, views.Movies(testUser, homeMedia, homeMedia, homeMedia, false, nil)))

	if strings.Contains(out, `role="tablist"`) || strings.Contains(out, "/api/discover") {
		t.Error("a rail with one list needs no tabs, and nothing is loaded lazily")
	}
	if strings.Contains(out, `href="/browse/`) {
		t.Error("home links to whole lists, which need TMDB details")
	}
	for _, want := range []string{`id="home-movies"`, `id="trending-movies"`, `id="home-series"`, `id="trending-series"`} {
		if !strings.Contains(out, want) {
			t.Errorf("home lacks %q", want)
		}
	}
}

func TestAnEmptyLazyListHidesItsTab(t *testing.T) {
	out := render(t, views.DiscoverRailLoaded(views.DiscoverRails[0], nil))

	if !strings.Contains(out, `id="tab-discover-now-playing" class="hidden"`) {
		t.Errorf("an empty list keeps its tab: %s", out)
	}
	if !strings.Contains(out, `id="discover-now-playing"`) || strings.Contains(out, "skeleton") {
		t.Errorf("an empty list keeps loading: %s", out)
	}
}
